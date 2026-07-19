package claudesidecar

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ToolState mirrors the sidecar's per-tool bookkeeping.
type ToolState struct {
	ID               string
	Name             string
	Input            map[string]any
	PartialInputJSON string
	Started          bool
	ParentToolUseID  string
	Steps            []map[string]any
}

type structuredPatchHunk struct {
	oldStart float64
	oldLines float64
	newStart float64
	newLines float64
	lines    []string
}

func contentBlocksFromMessage(message map[string]any) []map[string]any {
	inner := recordValue(message["message"])
	if inner == nil {
		return nil
	}
	content, ok := inner["content"].([]any)
	if !ok {
		return nil
	}
	blocks := make([]map[string]any, 0, len(content))
	for _, block := range content {
		if record := recordValue(block); record != nil {
			blocks = append(blocks, record)
		}
	}
	return blocks
}

func sdkContentFromPromptBlocks(value any, fallbackPrompt string) []map[string]any {
	var content []map[string]any
	if items, ok := value.([]any); ok {
		for _, item := range items {
			record := recordValue(item)
			if record == nil {
				continue
			}
			switch record["type"] {
			case "text":
				if text := stringValue(record["text"]); text != "" {
					content = append(content, map[string]any{"type": "text", "text": text})
				}
			case "image":
				mimeType := stringValue(record["mimeType"])
				data := stringValue(record["data"])
				imageURL := stringValue(record["url"])
				if data == "" && isSupportedClaudeImageMimeType(mimeType) && isSafeImageURL(imageURL) {
					content = append(content, map[string]any{
						"type":   "image",
						"source": map[string]any{"type": "url", "url": imageURL},
					})
					continue
				}
				if isSupportedClaudeImageMimeType(mimeType) && data != "" && imageURL == "" {
					content = append(content, map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": mimeType,
							"data":       data,
						},
					})
				}
			}
		}
	}
	if len(content) == 0 {
		if prompt := strings.TrimSpace(fallbackPrompt); prompt != "" {
			content = append(content, map[string]any{"type": "text", "text": prompt})
		}
	}
	return content
}

func isSafeImageURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil
}

func isSupportedClaudeImageMimeType(value string) bool {
	return value == "image/png" || value == "image/jpeg" || value == "image/webp"
}

func parseJSONObject(value string) map[string]any {
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return nil
	}
	return recordValue(parsed)
}

func isToolUseBlock(block map[string]any) bool {
	switch block["type"] {
	case "tool_use", "server_tool_use", "mcp_tool_use":
		return true
	default:
		return false
	}
}

func isThinkingBlock(block map[string]any) bool {
	if block["type"] != "thinking" {
		return false
	}
	_, ok := block["thinking"].(string)
	return ok
}

func toolPayload(turnID string, tool *ToolState, status string, result map[string]any) map[string]any {
	output := toolResultOutput(result)
	metadata := toolMetadata(tool, result)
	payload := map[string]any{
		"turnId":     turnID,
		"toolCallId": tool.ID,
		"callId":     tool.ID,
		"toolName":   tool.Name,
		"callType":   toolCallType(tool.Name),
		"name":       toolDisplayName(tool.Name),
		"status":     status,
		"input":      normalizeToolInput(tool),
		"locations":  toolLocations(tool.Input),
		"metadata":   metadata,
	}
	content := toolContent(tool, result)
	if len(content) > 0 {
		payload["content"] = content
	}
	if output != nil {
		payload["output"] = output
	}
	if status == "failed" {
		if output != nil {
			payload["error"] = output
		} else {
			payload["error"] = map[string]any{"message": "Claude Code tool call failed"}
		}
	}
	return payload
}

var unsupportedCommandNames = map[string]struct{}{
	"clear":            {},
	"cost":             {},
	"keybindings-help": {},
	"login":            {},
	"logout":           {},
	"output-style:new": {},
	"release-notes":    {},
	"todos":            {},
}

func commandEntries(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	commands := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if text, isString := item.(string); isString {
			name := strings.TrimSpace(text)
			if name == "" {
				continue
			}
			if _, unsupported := unsupportedCommandNames[name]; unsupported {
				continue
			}
			commands = append(commands, map[string]any{"name": name})
			continue
		}
		command := recordValue(item)
		if command == nil {
			continue
		}
		rawName := stringValue(command["name"])
		if rawName == "" {
			continue
		}
		if _, unsupported := unsupportedCommandNames[rawName]; unsupported {
			continue
		}
		name := rawName
		if strings.HasSuffix(rawName, " (MCP)") {
			name = "mcp:" + strings.ReplaceAll(rawName, " (MCP)", "")
		}
		entry := map[string]any{
			"name":        name,
			"description": stringValue(command["description"]),
		}
		if hint := commandArgumentHint(command["argumentHint"]); hint != "" {
			entry["input"] = map[string]any{"hint": hint}
		}
		commands = append(commands, entry)
	}
	return commands
}

func speedFromFastModeState(state any) string {
	switch state {
	case "on":
		return "fast"
	case "off":
		return "standard"
	default:
		return ""
	}
}

func commandArgumentHint(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range items {
		if text, isString := item.(string); isString {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
	}
	return strings.Join(parts, " ")
}

func toolCallType(toolName string) string {
	normalized := strings.ToLower(toolName)
	switch normalized {
	case "askuserquestion", "enterplanmode", "exitplanmode":
		return "interactive"
	}
	if normalized == "task" || strings.Contains(normalized, "agent") {
		return "subagent"
	}
	for _, marker := range []string{"bash", "command", "shell", "terminal"} {
		if strings.Contains(normalized, marker) {
			return "command"
		}
	}
	for _, marker := range []string{"edit", "write", "patch", "file"} {
		if strings.Contains(normalized, marker) {
			return "file_change"
		}
	}
	if strings.Contains(normalized, "websearch") || strings.Contains(normalized, "web_search") {
		return "web_search"
	}
	if strings.Contains(normalized, "mcp") {
		return "mcp_tool"
	}
	return "tool"
}

func answersFromInteractivePayload(payload map[string]any, toolInput map[string]any) map[string]any {
	if byQuestionID := recordValue(payload["answersByQuestionId"]); byQuestionID != nil {
		return answersByQuestionText(byQuestionID, toolInput)
	}
	if answers := recordValue(payload["answers"]); answers != nil {
		return answers
	}
	return map[string]any{}
}

func answersByQuestionText(answersByQuestionID map[string]any, toolInput map[string]any) map[string]any {
	var questions []any
	if toolInput != nil {
		questions, _ = toolInput["questions"].([]any)
	}
	if len(questions) == 0 {
		return answersByQuestionID
	}
	answers := map[string]any{}
	for index, value := range questions {
		question := recordValue(value)
		questionText := ""
		if question != nil {
			questionText = stringValue(question["question"])
		}
		if questionText == "" {
			continue
		}
		key := ""
		if question != nil {
			key = stringValue(question["id"])
		}
		if key == "" {
			key = fmt.Sprintf("question-%d", index+1)
		}
		answer, present := answersByQuestionID[key]
		if !present {
			continue
		}
		answers[questionText] = sdkAnswerValue(answer)
	}
	return answers
}

func sdkAnswerValue(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	var parts []string
	for _, item := range items {
		if text, isString := item.(string); isString {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
	}
	return strings.Join(parts, ", ")
}

func toolDisplayName(toolName string) string {
	if toolName != "" {
		return toolName
	}
	return "Claude Code tool"
}

func normalizeToolInput(tool *ToolState) map[string]any {
	input := cloneRecord(tool.Input)
	if input == nil {
		input = map[string]any{}
	}
	if tool.Name != "" {
		if _, present := input["toolName"]; !present {
			input["toolName"] = tool.Name
		}
	}
	return input
}

func toolResultOutput(result map[string]any) map[string]any {
	if result == nil {
		return nil
	}
	content := result["content"]
	output := map[string]any{}
	switch typed := content.(type) {
	case string:
		output["text"] = typed
	case []any:
		output["content"] = typed
		var parts []string
		for _, item := range typed {
			record := recordValue(item)
			if record == nil {
				continue
			}
			if text, isString := record["text"].(string); isString && text != "" {
				parts = append(parts, text)
			}
		}
		if text := strings.Join(parts, "\n"); text != "" {
			output["text"] = text
		}
	default:
		if content != nil {
			output["content"] = content
		}
	}
	if toolResponse := toolResponseFromResult(result); toolResponse != nil {
		output["toolResponse"] = toolResponse
		structuredPatch := structuredPatchOutput(toolResponse)
		if len(structuredPatch) > 0 {
			output["structuredPatch"] = structuredPatch
		}
		changes := fileChangesFromStructuredPatch(structuredPatch)
		if len(changes) > 0 {
			output["changes"] = changes
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func toolMetadata(tool *ToolState, result map[string]any) map[string]any {
	metadata := map[string]any{
		"adapter":  "claude-agent-sdk",
		"toolName": tool.Name,
	}
	parentToolUseID := strings.TrimSpace(tool.ParentToolUseID)
	if parentToolUseID == "" {
		parentToolUseID = stringValue(tool.Input["parent_tool_use_id"])
	}
	if parentToolUseID != "" {
		metadata["parentToolUseId"] = parentToolUseID
	}
	if len(tool.Steps) > 0 {
		metadata["steps"] = tool.Steps
	}
	if subagentLaunch := subagentLaunchMetadata(result); subagentLaunch != nil {
		for key, value := range subagentLaunch {
			metadata[key] = value
		}
	}
	if fileChange := fileChangeMetadata(tool); fileChange != nil {
		metadata["fileChange"] = fileChange
	}
	if toolResponse := toolResponseFromResult(result); toolResponse != nil {
		metadata["claudeToolResponse"] = toolResponse
	}
	return metadata
}

var asyncAgentLaunched = regexp.MustCompile(`(?i)Async agent launched successfully`)

func subagentLaunchMetadata(result map[string]any) map[string]any {
	// Nested agent launches stream through the parent query without a locally
	// observed tool_use block, so the tool name may be unknown here. The launch
	// result text is the authoritative signal, not the tool call type.
	text := toolResultText(result)
	if !asyncAgentLaunched.MatchString(text) {
		return nil
	}
	agentID := firstNonEmptyText(
		lineTokenValue(text, "agentId"),
		lineTokenValue(text, "agent_id"),
		jsonStringValue(text, "agentId"),
		jsonStringValue(text, "agent_id"),
	)
	outputFile := firstNonEmptyText(
		lineValue(text, "output_file"),
		lineValue(text, "outputFile"),
		jsonStringValue(text, "output_file"),
		jsonStringValue(text, "outputFile"),
	)
	metadata := map[string]any{
		"async":          true,
		"subagentAsync":  true,
		"taskStatus":     "running",
		"subagentStatus": "running",
	}
	if agentID != "" {
		metadata["subagentAgentId"] = agentID
		metadata["agentId"] = agentID
	}
	if outputFile != "" {
		metadata["outputFile"] = outputFile
		metadata["subagentOutputFile"] = outputFile
	}
	return metadata
}

func toolResultText(result map[string]any) string {
	if result == nil {
		return ""
	}
	output := toolResultOutput(result)
	if output != nil {
		if text := stringValue(output["text"]); text != "" {
			return text
		}
	}
	switch typed := result["content"].(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, item := range typed {
			record := recordValue(item)
			if record == nil {
				continue
			}
			if text, isString := record["text"].(string); isString && text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func lineValue(text string, key string) string {
	pattern := regexp.MustCompile(`(?i)(?:^|\n)\s*` + regexp.QuoteMeta(key) + `\s*:\s*(.+)`)
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

var whitespaceSplit = regexp.MustCompile(`\s+`)

func lineTokenValue(text string, key string) string {
	value := lineValue(text, key)
	if value == "" {
		return ""
	}
	tokens := whitespaceSplit.Split(value, 2)
	if len(tokens) == 0 {
		return ""
	}
	return strings.TrimSpace(tokens[0])
}

func jsonStringValue(text string, key string) string {
	pattern := regexp.MustCompile(`(?i)"` + regexp.QuoteMeta(key) + `"\s*:\s*"([^"]+)"`)
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func toolLocations(input map[string]any) []map[string]any {
	var paths []string
	seen := map[string]struct{}{}
	for _, key := range []string{"file_path", "path", "notebook_path", "filename"} {
		text, ok := input[key].(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		if _, duplicate := seen[trimmed]; duplicate {
			continue
		}
		seen[trimmed] = struct{}{}
		paths = append(paths, trimmed)
	}
	locations := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		locations = append(locations, map[string]any{"path": path})
	}
	return locations
}

func toolContent(tool *ToolState, result map[string]any) []map[string]any {
	var content []map[string]any
	if fileChange := fileChangeMetadata(tool); fileChange != nil {
		block := map[string]any{"type": "file_change"}
		for key, value := range fileChange {
			block[key] = value
		}
		content = append(content, block)
	}
	if output := toolResultOutput(result); output != nil {
		block := map[string]any{"type": "tool_result"}
		for key, value := range output {
			block[key] = value
		}
		content = append(content, block)
	}
	return content
}

func fileChangeMetadata(tool *ToolState) map[string]any {
	if toolCallType(tool.Name) != "file_change" {
		return nil
	}
	metadata := map[string]any{"toolName": tool.Name}
	locations := toolLocations(tool.Input)
	if len(locations) > 0 {
		paths := make([]any, 0, len(locations))
		for _, location := range locations {
			paths = append(paths, location["path"])
		}
		metadata["paths"] = paths
	}
	oldText := firstNonEmptyText(stringValue(tool.Input["old_string"]), stringValue(tool.Input["oldText"]))
	newText := firstNonEmptyText(stringValue(tool.Input["new_string"]), stringValue(tool.Input["newText"]))
	if newText == "" && tool.Name == "Write" {
		newText = stringValue(tool.Input["content"])
	}
	if oldText != "" {
		metadata["oldText"] = oldText
	}
	if newText != "" {
		metadata["newText"] = newText
	}
	if len(metadata) <= 1 {
		return nil
	}
	return metadata
}

func toolResponseFromResult(result map[string]any) map[string]any {
	if result == nil {
		return nil
	}
	meta := recordValue(result["_meta"])
	claudeCode := recordValue(meta["claudeCode"])
	if toolResponse := recordValue(claudeCode["toolResponse"]); toolResponse != nil {
		return toolResponse
	}
	return recordValue(result["tool_response"])
}

func structuredPatchOutput(toolResponse map[string]any) []map[string]any {
	filePath := stringValue(toolResponse["filePath"])
	rawHunks := structuredPatchHunks(toolResponse["structuredPatch"])
	if filePath == "" || len(rawHunks) == 0 {
		return nil
	}
	diff := unifiedDiffFromStructuredPatch(rawHunks)
	if diff == "" {
		return nil
	}
	kind := toolResponse["type"]
	if kind == nil {
		kind = "update"
	}
	return []map[string]any{
		{
			"path":     filePath,
			"filePath": filePath,
			"kind":     kind,
			"change":   kind,
			"diff":     diff,
			"patch":    diff,
		},
	}
}

func fileChangesFromStructuredPatch(structuredPatch []map[string]any) []map[string]any {
	if len(structuredPatch) == 0 {
		return nil
	}
	filePatch := structuredPatch[0]
	return []map[string]any{
		{
			"path": filePatch["path"],
			"type": filePatch["change"],
			"diff": filePatch["diff"],
		},
	}
}

func structuredPatchHunks(value any) []structuredPatchHunk {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var hunks []structuredPatchHunk
	for _, item := range items {
		hunk := recordValue(item)
		if hunk == nil {
			continue
		}
		rawLines, isList := hunk["lines"].([]any)
		if !isList {
			continue
		}
		var lines []string
		for _, line := range rawLines {
			if text, isString := line.(string); isString {
				lines = append(lines, text)
			}
		}
		if len(lines) == 0 {
			continue
		}
		hunks = append(hunks, structuredPatchHunk{
			oldStart: numberValue(hunk["oldStart"]),
			oldLines: numberValue(hunk["oldLines"]),
			newStart: numberValue(hunk["newStart"]),
			newLines: numberValue(hunk["newLines"]),
			lines:    lines,
		})
	}
	return hunks
}

func unifiedDiffFromStructuredPatch(hunks []structuredPatchHunk) string {
	var lines []string
	for _, hunk := range hunks {
		lines = append(lines, fmt.Sprintf(
			"@@ -%s,%s +%s,%s @@",
			formatPatchNumber(hunk.oldStart),
			formatPatchNumber(hunk.oldLines),
			formatPatchNumber(hunk.newStart),
			formatPatchNumber(hunk.newLines),
		))
		for _, line := range hunk.lines {
			lines = append(lines, normalizeDiffLine(line))
		}
	}
	return strings.Join(lines, "\n")
}

func formatPatchNumber(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d", int64(value))
	}
	return fmt.Sprintf("%v", value)
}

func normalizeDiffLine(line string) string {
	if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") {
		return line
	}
	return " " + line
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func goalStateFromContentBlocks(blocks []map[string]any) map[string]any {
	for _, block := range blocks {
		attachment := goalStatusAttachment(block, 6)
		if attachment == nil {
			continue
		}
		objective := stringValue(attachment["condition"])
		if objective == "" {
			continue
		}
		status := "active"
		if attachment["met"] == true {
			status = "complete"
		}
		goal := map[string]any{
			"objective": objective,
			"status":    status,
		}
		for _, key := range []string{"reason", "iterations", "durationMs", "tokens", "sentinel"} {
			if value, present := attachment[key]; present {
				goal[key] = value
			}
		}
		return goal
	}
	return nil
}

func goalStatusAttachment(value map[string]any, depth int) map[string]any {
	if depth <= 0 {
		return nil
	}
	if stringValue(value["type"]) == "goal_status" {
		return value
	}
	if attachment := recordValue(value["attachment"]); attachment != nil && stringValue(attachment["type"]) == "goal_status" {
		return attachment
	}
	for _, child := range value {
		if nested := goalStatusAttachmentFromUnknown(child, depth-1); nested != nil {
			return nested
		}
	}
	return nil
}

func goalStatusAttachmentFromUnknown(value any, depth int) map[string]any {
	if record := recordValue(value); record != nil {
		return goalStatusAttachment(record, depth)
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	for _, item := range items {
		if nested := goalStatusAttachmentFromUnknown(item, depth); nested != nil {
			return nested
		}
	}
	return nil
}
