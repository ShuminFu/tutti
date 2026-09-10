package runtimeprep

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func mergeJSONExtensionRuntimeConfig(
	config string,
	endpoint *ModelEndpointConfig,
	declaration *ExtensionModelEndpoint,
) (string, error) {
	if !ExtensionModelEndpointApplies(endpoint, declaration) {
		return config, nil
	}
	document := map[string]any{}
	if strings.TrimSpace(config) != "" {
		if err := json.Unmarshal([]byte(config), &document); err != nil {
			return "", fmt.Errorf("parse extension runtime JSON config: %w", err)
		}
	}
	values := []struct {
		path  []string
		value any
	}{
		{declaration.ConfigKeys.Provider, strings.TrimSpace(declaration.ProviderValue)},
		{declaration.ConfigKeys.Model, strings.TrimSpace(endpoint.Model)},
		{declaration.ConfigKeys.BaseURL, strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")},
	}
	if len(declaration.ConfigKeys.APIKeyEnv) > 0 {
		values = append(values, struct {
			path  []string
			value any
		}{declaration.ConfigKeys.APIKeyEnv, strings.TrimSpace(declaration.APIKeyEnv)})
	}
	if len(declaration.ConfigKeys.WireAPI) > 0 {
		value := strings.TrimSpace(declaration.WireAPIConfigValue)
		if value == "" {
			value = strings.TrimSpace(declaration.WireAPI)
		}
		values = append(values, struct {
			path  []string
			value any
		}{declaration.ConfigKeys.WireAPI, value})
	}
	if len(declaration.ConfigKeys.Models) > 0 {
		catalog := make([]map[string]any, 0, len(endpoint.Models))
		seen := map[string]struct{}{}
		for _, model := range endpoint.Models {
			id := strings.TrimSpace(model.ID)
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			name := strings.TrimSpace(model.Name)
			if name == "" {
				name = id
			}
			entry := map[string]any{"id": id, "name": name}
			if len(model.ReasoningEfforts) > 0 {
				entry["reasoningEfforts"] = model.ReasoningEfforts
			}
			catalog = append(catalog, entry)
		}
		values = append(values, struct {
			path  []string
			value any
		}{declaration.ConfigKeys.Models, catalog})
	}
	for _, value := range values {
		if err := jsonSetPath(document, value.path, value.value); err != nil {
			return "", err
		}
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", fmt.Errorf("write extension runtime JSON config: %w", err)
	}
	return string(encoded) + "\n", nil
}

func jsonSetPath(root map[string]any, keyPath []string, value any) error {
	if len(keyPath) == 0 {
		return errors.New("extension runtime JSON key path is empty")
	}
	mapping := root
	for _, key := range keyPath[:len(keyPath)-1] {
		next, exists := mapping[key]
		if !exists {
			child := map[string]any{}
			mapping[key] = child
			mapping = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("extension runtime JSON %s must be an object", strings.Join(keyPath[:len(keyPath)-1], "."))
		}
		mapping = child
	}
	mapping[keyPath[len(keyPath)-1]] = value
	return nil
}
