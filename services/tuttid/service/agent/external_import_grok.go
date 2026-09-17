package agent

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
)

// grokImportProvider is the import identity for local Grok CLI sessions.
// Live Tutti Grok sessions already use this ACP identity; the import path
// stays import-only and does not add a runnable providerregistry descriptor.
const grokImportProvider = "acp:grok"

const (
	grokImportTargetID         = "extension:grok"
	grokImportRootEnvVar       = "GROK_HOME"
	grokImportDefaultRoot      = "~/.grok"
	grokImportExtraRootsEnvVar = "TUTTI_GROK_EXTRA_IMPORT_ROOTS"
	grokSessionsDirName        = "sessions"
	grokSummaryFileName        = "summary.json"
	grokUpdatesFileName        = "updates.jsonl"
	grokChatHistoryFileName    = "chat_history.jsonl"
	grokUsageFileName          = "usage.json"
	grokGroupCwdFileName       = ".cwd"
)

func grokExternalImportDescriptor() providerregistry.ExternalImportDescriptor {
	return providerregistry.ExternalImportDescriptor{
		RootEnvVar:       grokImportRootEnvVar,
		DefaultRoot:      grokImportDefaultRoot,
		ExtraRootsEnvVar: grokImportExtraRootsEnvVar,
	}
}

func scanGrokProviderSessions(cutoffUnixMS int64) ([]externalImportedSession, ExternalImportProvider, []ExternalImportError) {
	roots := grokImportRoots()
	summary := ExternalImportProvider{Provider: grokImportProvider}
	if len(roots) > 0 {
		summary.Root = roots[0]
	}
	sessions := make([]externalImportedSession, 0, 8)
	errors := make([]ExternalImportError, 0)
	indexBySessionID := make(map[string]int)
	for _, root := range roots {
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		summary.Available = true
		dirs, err := grokSessionDirs(root)
		if err != nil {
			summary.Error = err.Error()
			errors = append(errors, ExternalImportError{Provider: grokImportProvider, Message: err.Error()})
			continue
		}
		for _, dir := range dirs {
			session, ok, err := parseGrokSessionDir(dir)
			if err != nil {
				errors = append(errors, ExternalImportError{Provider: grokImportProvider, SourcePath: dir, Message: err.Error()})
				continue
			}
			if !ok {
				continue
			}
			if cutoffUnixMS > 0 && session.UpdatedAtUnixMS < cutoffUnixMS {
				continue
			}
			if index, exists := indexBySessionID[session.ProviderSessionID]; exists {
				if session.UpdatedAtUnixMS > sessions[index].UpdatedAtUnixMS {
					sessions[index] = session
				}
				continue
			}
			indexBySessionID[session.ProviderSessionID] = len(sessions)
			sessions = append(sessions, session)
		}
	}
	for _, session := range sessions {
		summary.SessionCount++
		summary.MessageCount += len(session.Messages)
	}
	return sessions, summary, errors
}

func grokImportRoots() []string {
	return externalProviderRoots(grokExternalImportDescriptor())
}

func grokSessionDirs(root string) ([]string, error) {
	sessionsRoot := filepath.Join(root, grokSessionsDirName)
	info, err := os.Stat(sessionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	groups, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0)
	for _, group := range groups {
		if !group.IsDir() {
			continue
		}
		groupPath := filepath.Join(sessionsRoot, group.Name())
		entries, err := os.ReadDir(groupPath)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(groupPath, entry.Name())
			if grokSessionDirHasTranscript(dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs, nil
}

func grokSessionDirHasTranscript(dir string) bool {
	for _, name := range []string{grokUpdatesFileName, grokChatHistoryFileName, grokSummaryFileName} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func parseGrokSessionDir(dir string) (externalImportedSession, bool, error) {
	summary, err := readGrokSummary(dir)
	if err != nil {
		return externalImportedSession{}, false, err
	}
	session := externalImportedSession{
		Provider:          grokImportProvider,
		SourcePath:        dir,
		ProviderSessionID: firstNonEmptyString(summary.id, filepath.Base(dir)),
		Cwd:               firstNonEmptyString(summary.cwd, grokGroupCwd(dir)),
		SummaryTitle:      firstNonEmptyString(summary.generatedTitle, summary.sessionSummary),
		Model:             summary.model,
	}
	if updates, err := parseGrokUpdatesFile(filepath.Join(dir, grokUpdatesFileName)); err != nil {
		return externalImportedSession{}, false, err
	} else {
		session.Messages = updates.messages
		session.Model = firstNonEmptyString(session.Model, updates.model)
	}
	if len(session.Messages) == 0 {
		messages, model, err := parseGrokChatHistoryFile(filepath.Join(dir, grokChatHistoryFileName))
		if err != nil {
			return externalImportedSession{}, false, err
		}
		session.Messages = messages
		session.Model = firstNonEmptyString(session.Model, model)
	}
	usage, err := readGrokSessionUsage(dir)
	if err != nil {
		return externalImportedSession{}, false, err
	}
	normalized, ok, err := normalizeExternalParsedSession(session)
	if err != nil || !ok {
		return normalized, ok, err
	}
	attachGrokSessionUsage(normalized.Messages, usage)
	if normalized.StartedAtUnixMS <= 0 {
		normalized.StartedAtUnixMS = summary.createdAtUnixMS
	}
	if normalized.UpdatedAtUnixMS <= 0 {
		normalized.UpdatedAtUnixMS = firstNonZeroInt64(summary.updatedAtUnixMS, summary.createdAtUnixMS)
	}
	return normalized, true, nil
}

// readGrokSessionUsage loads the `usage.json` token snapshot the Grok CLI
// writes beside each session transcript and maps it onto the Claude
// `message.usage` key shape the import path already understands. A session dir
// without the file reports no usage: counts are copied verbatim from the
// snapshot and never derived from signals.json or from message text.
func readGrokSessionUsage(dir string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(dir, grokUsageFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	if usage := grokUsageTokens(mapField(decoded, "session")); len(usage) > 0 {
		return usage, nil
	}
	return grokUsageTokens(decoded), nil
}

// grokUsageTokenKeys renames Grok's camelCase totals to the Claude usage keys
// ParseProviderTokenUsage reads. `inputTokens`/`outputTokens` are already
// aliases there, but Grok's `cachedReadTokens`/`cacheCreationTokens` are not,
// so every key is mapped explicitly here rather than relying on the aliases.
var grokUsageTokenKeys = []struct {
	target  string
	sources []string
}{
	{target: "input_tokens", sources: []string{"inputTokens", "input_tokens"}},
	{target: "output_tokens", sources: []string{"outputTokens", "output_tokens"}},
	{target: "cache_read_input_tokens", sources: []string{"cachedReadTokens", "cacheReadInputTokens", "cache_read_input_tokens"}},
	{target: "cache_creation_input_tokens", sources: []string{"cacheCreationTokens", "cacheCreationInputTokens", "cache_creation_input_tokens"}},
}

// grokUsageTokens copies the reported counts out of a Grok usage snapshot.
// Keys the snapshot omits stay absent so a partial report never turns into a
// fabricated zero. Grok also records `reasoningTokens`, `totalTokens`,
// `modelCalls` and `costUsdTicks`; those have no Claude usage counterpart and
// are deliberately dropped instead of being folded into another count.
func grokUsageTokens(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	usage := make(map[string]any, len(grokUsageTokenKeys))
	for _, key := range grokUsageTokenKeys {
		for _, source := range key.sources {
			count, ok := grokUsageCount(raw[source])
			if !ok {
				continue
			}
			usage[key.target] = count
			break
		}
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}

func grokUsageCount(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed, true
		}
		if parsed, err := typed.Float64(); err == nil {
			return int64(parsed), true
		}
	}
	return 0, false
}

// attachGrokSessionUsage reports the session-level snapshot on the newest
// assistant message. Grok's usage.json only totals the whole session, and this
// is the same carrier Claude import uses: the message payload picks the counts
// up through externalImportedUsage, and session metadata picks the same object
// up through lastExternalImportedUsage. A session with no assistant message has
// nothing to attribute the tokens to, so the snapshot is left off.
func attachGrokSessionUsage(messages []externalImportedMessage, usage map[string]any) {
	if len(usage) == 0 {
		return
	}
	fallback := -1
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "assistant" {
			continue
		}
		if messages[index].Kind == "text" {
			messages[index].Usage = usage
			return
		}
		if fallback < 0 {
			fallback = index
		}
	}
	if fallback >= 0 {
		messages[fallback].Usage = usage
	}
}

type grokSummary struct {
	id              string
	cwd             string
	generatedTitle  string
	sessionSummary  string
	model           string
	createdAtUnixMS int64
	updatedAtUnixMS int64
}

func readGrokSummary(dir string) (grokSummary, error) {
	raw, err := os.ReadFile(filepath.Join(dir, grokSummaryFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return grokSummary{}, nil
		}
		return grokSummary{}, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return grokSummary{}, err
	}
	info := mapField(decoded, "info")
	return grokSummary{
		id:              firstNonEmptyString(stringField(info, "id"), stringField(decoded, "id")),
		cwd:             firstNonEmptyString(stringField(info, "cwd"), stringField(decoded, "cwd")),
		generatedTitle:  stringField(decoded, "generated_title"),
		sessionSummary:  stringField(decoded, "session_summary"),
		model:           firstNonEmptyString(stringField(decoded, "current_model_id"), stringField(decoded, "model")),
		createdAtUnixMS: unixMSFromAny(decoded["created_at"]),
		updatedAtUnixMS: unixMSFromAny(decoded["updated_at"]),
	}, nil
}

func grokGroupCwd(sessionDir string) string {
	groupDir := filepath.Dir(sessionDir)
	if raw, err := os.ReadFile(filepath.Join(groupDir, grokGroupCwdFileName)); err == nil {
		if cwd := strings.TrimSpace(string(raw)); cwd != "" {
			return cwd
		}
	}
	return grokDecodeCwd(filepath.Base(groupDir))
}

func grokDecodeCwd(encoded string) string {
	decoded, err := url.PathUnescape(encoded)
	if err != nil {
		return ""
	}
	return decoded
}

type grokParsedUpdates struct {
	messages []externalImportedMessage
	model    string
}

func parseGrokUpdatesFile(path string) (grokParsedUpdates, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return grokParsedUpdates{}, nil
		}
		return grokParsedUpdates{}, err
	}
	defer file.Close()
	acc := grokUpdateAccumulator{}
	err = readJSONLLines(file, func(index int, raw map[string]any) {
		acc.consume(index, raw)
	})
	if err != nil {
		return grokParsedUpdates{}, err
	}
	acc.flushText()
	return grokParsedUpdates{messages: acc.messages, model: acc.model}, nil
}

type grokUpdateAccumulator struct {
	messages    []externalImportedMessage
	pending     *externalImportedMessage
	pendingKind string
	promptIndex int
	model       string
}

func (acc *grokUpdateAccumulator) consume(index int, raw map[string]any) {
	update := grokSessionUpdate(raw)
	if len(update) == 0 {
		return
	}
	kind := stringField(update, "sessionUpdate")
	timestamp := grokUpdateTimestamp(raw, update)
	if model := grokUpdateModel(update); model != "" {
		acc.model = model
	}
	switch kind {
	case "user_message_chunk":
		acc.appendText("user", grokChunkText(update["content"]), timestamp, grokUpdateRawID(update, index), grokPromptIndex(update, acc.promptIndex))
	case "agent_message_chunk":
		acc.appendText("assistant", grokChunkText(update["content"]), timestamp, grokUpdateRawID(update, index), acc.promptIndex)
	case "agent_thought_chunk":
		acc.appendText("reasoning", grokChunkText(update["content"]), timestamp, grokUpdateRawID(update, index), acc.promptIndex)
	case "tool_call":
		acc.flushText()
		if message := grokToolCallMessage(update, timestamp); externalImportedMessageHasContent(message) {
			acc.messages = append(acc.messages, message)
		}
	case "tool_call_update":
		acc.flushText()
		if message := grokToolCallUpdateMessage(update, timestamp); externalImportedMessageHasContent(message) {
			acc.messages = append(acc.messages, message)
		}
	case "turn_completed":
		acc.flushText()
		acc.promptIndex++
	}
}

func (acc *grokUpdateAccumulator) appendText(role string, text string, timestamp int64, rawID string, promptIndex int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	kind := "text"
	if role == "reasoning" {
		kind = "reasoning"
		role = "assistant"
	}
	sameTurn := acc.pending != nil && acc.pendingKind == kind && acc.pending.Role == role && acc.promptIndex == promptIndex
	if sameTurn {
		acc.pending.Text = strings.TrimSpace(acc.pending.Text + text)
		if timestamp > 0 {
			acc.pending.OccurredAtUnixMS = timestamp
			acc.pending.CompletedAtUnixMS = timestamp
		}
		return
	}
	acc.flushText()
	acc.promptIndex = promptIndex
	acc.pendingKind = kind
	message := externalImportedMessage{
		RawID:             rawID,
		Role:              role,
		Kind:              kind,
		Status:            "completed",
		Text:              text,
		OccurredAtUnixMS:  timestamp,
		StartedAtUnixMS:   timestamp,
		CompletedAtUnixMS: timestamp,
	}
	acc.pending = &message
}

func (acc *grokUpdateAccumulator) flushText() {
	if acc.pending == nil {
		return
	}
	acc.messages = append(acc.messages, *acc.pending)
	acc.pending = nil
	acc.pendingKind = ""
}

func parseGrokChatHistoryFile(path string) ([]externalImportedMessage, string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	defer file.Close()
	messages := make([]externalImportedMessage, 0)
	model := ""
	err = readJSONLLines(file, func(index int, raw map[string]any) {
		message, nextModel, ok := grokChatHistoryMessage(raw, index)
		if nextModel != "" {
			model = nextModel
		}
		if ok {
			messages = append(messages, message)
		}
	})
	return messages, model, err
}

func grokChatHistoryMessage(raw map[string]any, index int) (externalImportedMessage, string, bool) {
	recordType := stringField(raw, "type")
	model := firstNonEmptyString(stringField(raw, "model_id"), stringField(raw, "model"))
	switch recordType {
	case "user":
		text := grokChunkText(raw["content"])
		if grokUserInfoPreamble(text) {
			return externalImportedMessage{}, model, false
		}
		return grokTextMessage("user", "text", text, stringField(raw, "id"), index), model, strings.TrimSpace(text) != ""
	case "assistant":
		text := grokChunkText(raw["content"])
		return grokTextMessage("assistant", "text", text, stringField(raw, "id"), index), model, strings.TrimSpace(text) != ""
	case "reasoning":
		text := firstNonEmptyString(grokChunkText(raw["summary"]), grokChunkText(raw["content"]))
		return grokTextMessage("assistant", "reasoning", text, firstNonEmptyString(stringField(raw, "id"), strconv.Itoa(index)), index), model, strings.TrimSpace(text) != ""
	case "tool_result":
		callID := firstNonEmptyString(stringField(raw, "tool_call_id"), stringField(raw, "id"))
		text := grokChunkText(raw["content"])
		return grokToolResultMessage(callID, text, 0), model, callID != "" || strings.TrimSpace(text) != ""
	default:
		return externalImportedMessage{}, model, false
	}
}

func grokTextMessage(role string, kind string, text string, rawID string, index int) externalImportedMessage {
	return externalImportedMessage{
		RawID:  firstNonEmptyString(rawID, strconv.Itoa(index)),
		Role:   role,
		Kind:   kind,
		Status: "completed",
		Text:   strings.TrimSpace(text),
	}
}

func grokToolCallMessage(update map[string]any, timestamp int64) externalImportedMessage {
	callID := firstNonEmptyString(stringField(update, "toolCallId"), stringField(update, "tool_call_id"))
	name := firstNonEmptyString(stringField(update, "title"), stringField(update, "kind"), callID)
	input := mapField(update, "rawInput")
	if len(input) == 0 {
		input = mapField(update, "raw_input")
	}
	payload := map[string]any{
		"source":   "external_import",
		"provider": grokImportProvider,
		"status":   "running",
	}
	if callID != "" {
		payload["callId"] = callID
	}
	if name != "" {
		payload["name"] = name
		payload["toolName"] = name
	}
	if len(input) > 0 {
		payload["input"] = input
	}
	return externalImportedMessage{
		RawID:            firstNonEmptyString(callID, stringField(update, "title")),
		MessageIDSeed:    grokToolMessageIDSeed(callID, "call"),
		Role:             "assistant",
		Kind:             "tool_call",
		Status:           "running",
		Text:             name,
		Payload:          payload,
		OccurredAtUnixMS: timestamp,
		StartedAtUnixMS:  timestamp,
	}
}

func grokToolCallUpdateMessage(update map[string]any, timestamp int64) externalImportedMessage {
	callID := firstNonEmptyString(stringField(update, "toolCallId"), stringField(update, "tool_call_id"))
	status := normalizeExternalMessageStatus(stringField(update, "status"))
	text := grokChunkText(update["content"])
	return grokToolResultMessage(callID, text, timestamp, status)
}

func grokToolResultMessage(callID string, text string, timestamp int64, status ...string) externalImportedMessage {
	resolvedStatus := "completed"
	if len(status) > 0 && status[0] != "" {
		resolvedStatus = status[0]
	}
	payload := map[string]any{
		"source":   "external_import",
		"provider": grokImportProvider,
		"status":   resolvedStatus,
	}
	if callID != "" {
		payload["callId"] = callID
	}
	if text != "" {
		payload["output"] = text
	}
	return externalImportedMessage{
		RawID:             firstNonEmptyString(callID, text),
		MessageIDSeed:     grokToolMessageIDSeed(callID, "result"),
		Role:              "tool",
		Kind:              "tool_call",
		Status:            resolvedStatus,
		Text:              text,
		Payload:           payload,
		OccurredAtUnixMS:  timestamp,
		CompletedAtUnixMS: timestamp,
	}
}

func grokToolMessageIDSeed(callID string, kind string) string {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return ""
	}
	return callID + "\x00" + kind
}

func grokSessionUpdate(raw map[string]any) map[string]any {
	if params := mapField(raw, "params"); len(params) > 0 {
		if update := mapField(params, "update"); len(update) > 0 {
			return update
		}
	}
	if update := mapField(raw, "update"); len(update) > 0 {
		return update
	}
	if stringField(raw, "sessionUpdate") != "" {
		return raw
	}
	return nil
}

func grokUpdateTimestamp(raw map[string]any, update map[string]any) int64 {
	if params := mapField(raw, "params"); len(params) > 0 {
		if ts := unixMSFromAny(mapField(params, "_meta")["agentTimestampMs"]); ts > 0 {
			return ts
		}
	}
	if ts := unixMSFromAny(raw["timestamp"]); ts > 0 {
		return ts
	}
	if meta := mapField(update, "_meta"); len(meta) > 0 {
		return unixMSFromAny(meta["agentTimestampMs"])
	}
	return 0
}

func grokUpdateMeta(update map[string]any) map[string]any {
	return mapField(update, "_meta")
}

func grokUpdateModel(update map[string]any) string {
	meta := grokUpdateMeta(update)
	return firstNonEmptyString(stringField(meta, "modelId"), stringField(meta, "model"))
}

func grokUpdateRawID(update map[string]any, index int) string {
	return firstNonEmptyString(stringField(grokUpdateMeta(update), "eventId"), strconv.Itoa(index))
}

func grokPromptIndex(update map[string]any, fallback int) int {
	meta := grokUpdateMeta(update)
	if len(meta) == 0 {
		return fallback
	}
	value, ok := meta["promptIndex"]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func grokChunkText(value any) string {
	return strings.TrimSpace(externalContentText(value))
}

func grokUserInfoPreamble(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "<user_info>")
}

func grokCleanUserText(text string) (string, bool) {
	if grokUserInfoPreamble(text) {
		return "", false
	}
	return text, strings.TrimSpace(text) != ""
}
