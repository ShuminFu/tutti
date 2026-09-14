package modelgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func convertResponseTools(tools []json.RawMessage) ([]map[string]any, responseToolMap, []string, error) {
	if len(tools) == 0 {
		return nil, nil, nil, nil
	}
	result := make([]map[string]any, 0, len(tools))
	toolMap := make(responseToolMap)
	filteredTypes := make(map[string]struct{})
	seenNames := make(map[string]struct{})
	seenIdentities := make(map[string]struct{})
	for index, encoded := range tools {
		var header struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(encoded, &header); err != nil || (header.Type != "function" && header.Type != "custom") {
			continue
		}
		name := strings.TrimSpace(header.Name)
		if name == "" {
			continue
		}
		if _, exists := seenNames[name]; exists {
			return nil, nil, nil, invalidParam(
				fmt.Sprintf("tools[%d].name", index),
				fmt.Sprintf("tool name %q is duplicated", name),
			)
		}
		seenNames[name] = struct{}{}
	}
	for index, encoded := range tools {
		var tool struct {
			Type        string            `json:"type"`
			Name        string            `json:"name"`
			Description string            `json:"description"`
			Parameters  json.RawMessage   `json:"parameters"`
			Format      json.RawMessage   `json:"format"`
			Strict      *bool             `json:"strict"`
			Tools       []json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(encoded, &tool); err != nil {
			return nil, nil, nil, invalidParam(fmt.Sprintf("tools[%d]", index), "invalid tool")
		}
		switch tool.Type {
		case "function":
			function, err := convertFunctionTool(
				tool.Name,
				tool.Description,
				tool.Parameters,
				tool.Strict,
				fmt.Sprintf("tools[%d]", index),
			)
			if err != nil {
				return nil, nil, nil, err
			}
			name := function["name"].(string)
			seenIdentities["\x00"+name] = struct{}{}
			toolMap[name] = responseToolIdentity{Name: name, Type: "function"}
			result = append(result, map[string]any{"type": "function", "function": function})
		case "custom":
			identity, err := convertCustomTool(tool.Name, tool.Description, tool.Format, fmt.Sprintf("tools[%d]", index))
			if err != nil {
				return nil, nil, nil, err
			}
			chatName := identity.Name
			if sanitizeChatToolName(chatName) != chatName || len(chatName) > 64 {
				chatName = flattenedChatToolName("custom", identity.Name, seenNames)
			}
			seenIdentities["\x00"+identity.Name] = struct{}{}
			seenNames[chatName] = struct{}{}
			toolMap[chatName] = identity
			result = append(result, customToolWrapper(chatName, identity))
		case "namespace":
			namespace := strings.TrimSpace(tool.Name)
			if namespace == "" {
				return nil, nil, nil, invalidParam(fmt.Sprintf("tools[%d].name", index), "tool namespace name is required")
			}
			if len(tool.Tools) == 0 {
				return nil, nil, nil, invalidParam(fmt.Sprintf("tools[%d].tools", index), "tool namespace must contain at least one function")
			}
			for nestedIndex, nestedEncoded := range tool.Tools {
				var nested struct {
					Type        string          `json:"type"`
					Name        string          `json:"name"`
					Description string          `json:"description"`
					Parameters  json.RawMessage `json:"parameters"`
					Strict      *bool           `json:"strict"`
				}
				param := fmt.Sprintf("tools[%d].tools[%d]", index, nestedIndex)
				if err := json.Unmarshal(nestedEncoded, &nested); err != nil {
					return nil, nil, nil, invalidParam(param, "invalid namespaced tool")
				}
				if nested.Type != "function" {
					filteredTypes[normalizedFilteredToolType(nested.Type)] = struct{}{}
					continue
				}
				function, err := convertFunctionTool(
					nested.Name,
					namespacedToolDescription(tool.Description, nested.Description),
					nested.Parameters,
					nested.Strict,
					param,
				)
				if err != nil {
					return nil, nil, nil, err
				}
				responseName := function["name"].(string)
				identityKey := namespace + "\x00" + responseName
				if _, exists := seenIdentities[identityKey]; exists {
					return nil, nil, nil, invalidParam(
						param+".name",
						fmt.Sprintf("tool name %q is duplicated within namespace %q", responseName, namespace),
					)
				}
				seenIdentities[identityKey] = struct{}{}
				chatName := flattenedChatToolName(namespace, responseName, seenNames)
				function["name"] = chatName
				seenNames[chatName] = struct{}{}
				toolMap[chatName] = responseToolIdentity{Name: responseName, Namespace: namespace, Type: "function"}
				result = append(result, map[string]any{"type": "function", "function": function})
			}
		default:
			// A Responses tool entry is an availability declaration, not a
			// call. Intersect declarations with the set this Chat adapter can
			// represent so newly advertised hosted tools do not break ordinary
			// turns. Explicit choices and call/output history are validated
			// separately and remain fail-closed.
			filteredTypes[normalizedFilteredToolType(tool.Type)] = struct{}{}
		}
	}
	filtered := make([]string, 0, len(filteredTypes))
	for toolType := range filteredTypes {
		filtered = append(filtered, toolType)
	}
	sort.Strings(filtered)
	return result, toolMap, filtered, nil
}

func chatNameForResponseTool(namespace string, name string, toolMap responseToolMap) string {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	for chatName, identity := range toolMap {
		if identity.Name == name && identity.Namespace == namespace {
			return chatName
		}
	}
	return name
}

func convertFunctionTool(
	name string,
	description string,
	encodedParameters json.RawMessage,
	strict *bool,
	param string,
) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, invalidParam(param+".name", "function tool name is required")
	}
	function := map[string]any{"name": name}
	if description != "" {
		function["description"] = description
	}
	if len(bytes.TrimSpace(encodedParameters)) > 0 &&
		!bytes.Equal(bytes.TrimSpace(encodedParameters), []byte("null")) {
		var parameters any
		if err := json.Unmarshal(encodedParameters, &parameters); err != nil {
			return nil, invalidParam(param+".parameters", "function parameters must be valid JSON")
		}
		function["parameters"] = parameters
	}
	if strict != nil {
		function["strict"] = *strict
	}
	return function, nil
}

// convertCustomTool compiles one Responses custom tool into the Chat function
// that carries its free-form input. The identity keeps the original name,
// namespace, and custom type so Responses output can restore the custom tool
// call shape; the returned Chat tool is the strict string-input wrapper.
func convertCustomTool(name string, description string, encodedFormat json.RawMessage, param string) (responseToolIdentity, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return responseToolIdentity{}, invalidParam(param+".name", "custom tool name is required")
	}
	formatNote, err := customToolFormatNote(encodedFormat, param)
	if err != nil {
		return responseToolIdentity{}, err
	}
	return responseToolIdentity{
		Name:               name,
		Type:               "custom",
		WrappedCustomInput: true,
		Description:        customToolWrapperDescription(description, formatNote),
	}, nil
}

func customToolWrapper(chatName string, identity responseToolIdentity) map[string]any {
	function := map[string]any{
		"name":        chatName,
		"parameters":  customToolParameters(),
		"description": identity.Description,
	}
	return map[string]any{"type": "function", "function": function}
}

func convertResponseToolChoice(encoded json.RawMessage, toolMap responseToolMap, toolCount int) (any, error) {
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err == nil {
		switch value {
		case "auto", "none":
			return value, nil
		case "required":
			if toolCount == 0 {
				return nil, invalidParam("tool_choice", "required tool_choice has no translatable tools")
			}
			return value, nil
		default:
			return nil, invalidParam("tool_choice", fmt.Sprintf("unsupported tool_choice %q", value))
		}
	}
	var choice struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(trimmed, &choice); err != nil {
		return nil, invalidParam("tool_choice", "named tool_choice must be a valid object")
	}
	if choice.Type != "function" && choice.Type != "custom" {
		return nil, invalidParam(
			"tool_choice",
			fmt.Sprintf("tool_choice type %q cannot be translated to Chat Completions", choice.Type),
		)
	}
	if strings.TrimSpace(choice.Name) == "" {
		return nil, invalidParam("tool_choice", "named tool_choice requires a name")
	}
	chatName, found := responseToolChatName(choice.Namespace, choice.Name, toolMap)
	if !found {
		return nil, invalidParam("tool_choice", fmt.Sprintf("selected %s tool %q is not registered", choice.Type, choice.Name))
	}
	identity := toolMap[chatName]
	if identity.Type != choice.Type {
		return nil, invalidParam("tool_choice", fmt.Sprintf("selected tool %q is not a %s tool", choice.Name, choice.Type))
	}
	if identity.WrappedCustomInput {
		// A custom tool is declared upstream as its wrapper function, so the
		// Chat choice must select that function by name.
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": chatName,
			},
		}, nil
	}
	return map[string]any{
		"type": choice.Type,
		choice.Type: map[string]any{
			"name": chatName,
		},
	}, nil
}

func responseToolChatName(namespace string, name string, toolMap responseToolMap) (string, bool) {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	for chatName, identity := range toolMap {
		if identity.Name == name && identity.Namespace == namespace {
			return chatName, true
		}
	}
	return "", false
}
