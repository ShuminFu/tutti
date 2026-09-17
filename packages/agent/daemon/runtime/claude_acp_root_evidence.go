package agentruntime

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

const claudeACPProjectIDMaxLength = 200

type claudeACPRootTurnEvidence struct {
	StopReason    string
	Outcome       string
	AssistantText string
}

type claudeACPTranscriptRecord struct {
	Type         string          `json:"type"`
	IsSidechain  bool            `json:"isSidechain"`
	ParentToolID string          `json:"parent_tool_use_id"`
	Message      json.RawMessage `json:"message"`
}

type claudeACPTranscriptMessage struct {
	StopReason string          `json:"stop_reason"`
	Content    json.RawMessage `json:"content"`
}

func claudeACPRootTurnEvidenceFromTranscript(
	path string,
	streamedAssistantText string,
) (claudeACPRootTurnEvidence, string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return claudeACPRootTurnEvidence{}, "transcript_path_empty", false
	}
	file, err := os.Open(path)
	if err != nil {
		return claudeACPRootTurnEvidence{}, "transcript_unavailable", false
	}
	defer file.Close()

	var lastRoot *claudeACPTranscriptRecord
	var lastRootMessage claudeACPTranscriptMessage
	var lastRootText string
	sidechainAfterRoot := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record claudeACPTranscriptRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		recordType := strings.TrimSpace(record.Type)
		if record.IsSidechain || strings.TrimSpace(record.ParentToolID) != "" {
			if lastRoot != nil && recordType == "assistant" {
				sidechainAfterRoot = true
			}
			continue
		}
		switch recordType {
		case "assistant":
			var message claudeACPTranscriptMessage
			if err := json.Unmarshal(record.Message, &message); err != nil {
				continue
			}
			lastRoot = &record
			lastRootMessage = message
			lastRootText = acpTextFromValue(decodeJSONValue(message.Content))
			sidechainAfterRoot = false
		case "user":
			// A later root user prompt is a new turn. Drop the previous root
			// assistant so we cannot settle the current Tutti turn from it.
			lastRoot = nil
			lastRootMessage = claudeACPTranscriptMessage{}
			lastRootText = ""
			sidechainAfterRoot = false
		}
	}
	if err := scanner.Err(); err != nil {
		return claudeACPRootTurnEvidence{}, "transcript_read_error", false
	}
	if lastRoot == nil {
		return claudeACPRootTurnEvidence{}, "root_assistant_missing", false
	}
	if sidechainAfterRoot {
		return claudeACPRootTurnEvidence{}, "background_continuation", false
	}
	stopReason := canonicalACPStopReason(lastRootMessage.StopReason)
	outcome, ok := claudeACPTranscriptStopReasonOutcome(stopReason)
	if !ok {
		return claudeACPRootTurnEvidence{}, "stop_reason_not_terminal", false
	}
	if claudeACPTranscriptContentHasToolUse(lastRootMessage.Content) {
		return claudeACPRootTurnEvidence{}, "root_tool_use_open", false
	}
	if !claudeACPAssistantTextMatches(streamedAssistantText, lastRootText) {
		return claudeACPRootTurnEvidence{}, "assistant_text_mismatch", false
	}
	return claudeACPRootTurnEvidence{
		StopReason:    firstNonEmpty(stopReason, "end_turn"),
		Outcome:       outcome,
		AssistantText: lastRootText,
	}, "root_assistant_terminal", true
}

func claudeACPTranscriptStopReasonOutcome(stopReason string) (string, bool) {
	switch strings.TrimSpace(stopReason) {
	case "", "tool_use", "pause_turn":
		return "", false
	case "canceled":
		return "canceled", true
	case "refusal", "max_tokens", "max_turn_requests":
		return "failed", true
	default:
		return "completed", true
	}
}

func claudeACPTranscriptContentHasToolUse(raw json.RawMessage) bool {
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return false
	}
	for _, block := range blocks {
		if strings.EqualFold(strings.TrimSpace(asString(block["type"])), "tool_use") {
			return true
		}
	}
	return false
}

func claudeACPAssistantTextMatches(streamed string, transcript string) bool {
	streamed = strings.TrimSpace(streamed)
	transcript = strings.TrimSpace(transcript)
	if streamed == "" {
		return transcript == ""
	}
	if transcript == "" {
		return false
	}
	return streamed == transcript || strings.HasPrefix(transcript, streamed)
}

func claudeACPTranscriptPath(configDir string, cwd string, providerSessionID string) string {
	configDir = strings.TrimSpace(configDir)
	providerSessionID = strings.TrimSpace(providerSessionID)
	cwd = strings.TrimSpace(cwd)
	if configDir == "" || providerSessionID == "" || cwd == "" {
		return ""
	}
	return filepath.Join(configDir, "projects", claudeACPProjectID(cwd), providerSessionID+".jsonl")
}

func claudeACPConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		if filepath.IsAbs(dir) {
			return filepath.Clean(dir)
		}
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func claudeACPProjectID(cwd string) string {
	resolved := strings.TrimSpace(cwd)
	if absolute, err := filepath.Abs(resolved); err == nil {
		resolved = absolute
	}
	if evaluated, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = evaluated
	}
	sanitized := sanitizeClaudeACPProjectName(resolved)
	units := utf16.Encode([]rune(sanitized))
	if len(units) <= claudeACPProjectIDMaxLength {
		return sanitized
	}
	return string(utf16.Decode(units[:claudeACPProjectIDMaxLength])) + "-" + claudeACPProjectHash(resolved)
}

func sanitizeClaudeACPProjectName(path string) string {
	var builder strings.Builder
	for _, unit := range utf16.Encode([]rune(path)) {
		switch {
		case unit >= 'a' && unit <= 'z', unit >= 'A' && unit <= 'Z', unit >= '0' && unit <= '9':
			builder.WriteRune(rune(unit))
		default:
			builder.WriteByte('-')
		}
	}
	return builder.String()
}

func claudeACPProjectHash(value string) string {
	var hash int32
	for _, unit := range utf16.Encode([]rune(value)) {
		hash = hash<<5 - hash + int32(unit)
	}
	magnitude := int64(hash)
	if magnitude < 0 {
		magnitude = -magnitude
	}
	if magnitude == 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var reversed []byte
	for magnitude > 0 {
		reversed = append(reversed, digits[magnitude%36])
		magnitude /= 36
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return string(reversed)
}

func decodeJSONValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}
