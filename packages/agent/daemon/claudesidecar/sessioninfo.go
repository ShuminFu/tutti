package claudesidecar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Session titles come from the Claude CLI's own session files, the way the
// SDK's getSessionInfo reads them: head and tail windows of
// <projects>/<munged-cwd>/<sessionId>.jsonl scanned for customTitle/aiTitle,
// lastPrompt, and summary markers. Best effort by design.

const sessionInfoWindowBytes = 65536
const projectDirNameLimit = 200

type claudeSessionInfo struct {
	customTitle string
	summary     string
}

func claudeProjectsRoot() string {
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(home, ".claude")
	}
	return filepath.Join(configDir, "projects")
}

var nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9]`)

// mungeProjectPath mirrors the CLI's project-directory naming: replace every
// non-alphanumeric with '-', truncating long paths with a base36 hash suffix.
func mungeProjectPath(path string) string {
	munged := nonAlphanumeric.ReplaceAllString(path, "-")
	if len(munged) <= projectDirNameLimit {
		return munged
	}
	return munged[:projectDirNameLimit] + "-" + pathHashBase36(path)
}

// pathHashBase36 mirrors the CLI's 32-bit string hash rendered in base 36.
func pathHashBase36(value string) string {
	var hash int32
	for _, char := range []byte(value) {
		hash = hash*31 + int32(char)
	}
	magnitude := int64(hash)
	if magnitude < 0 {
		magnitude = -magnitude
	}
	return strconv.FormatInt(magnitude, 36)
}

func getClaudeSessionInfo(sessionID string, dir string) *claudeSessionInfo {
	root := claudeProjectsRoot()
	if root == "" || sessionID == "" {
		return nil
	}
	fileName := sessionID + ".jsonl"
	var candidates []string
	if dir != "" {
		resolved := dir
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			resolved = real
		}
		candidates = append(candidates, filepath.Join(root, mungeProjectPath(resolved), fileName))
		if resolved != dir {
			candidates = append(candidates, filepath.Join(root, mungeProjectPath(dir), fileName))
		}
	}
	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			candidates = append(candidates, filepath.Join(root, entry.Name(), fileName))
		}
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		if info := sessionInfoFromFile(candidate); info != nil {
			return info
		}
	}
	return nil
}

func sessionInfoFromFile(path string) *claudeSessionInfo {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil || stat.Size() == 0 {
		return nil
	}
	head := make([]byte, sessionInfoWindowBytes)
	n, err := file.ReadAt(head, 0)
	if n == 0 && err != nil {
		return nil
	}
	headText := string(head[:n])
	tailText := headText
	if tailStart := stat.Size() - sessionInfoWindowBytes; tailStart > 0 {
		tail := make([]byte, sessionInfoWindowBytes)
		tailN, _ := file.ReadAt(tail, tailStart)
		tailText = string(tail[:tailN])
	}
	firstLine := headText
	if index := strings.IndexByte(headText, '\n'); index >= 0 {
		firstLine = headText[:index]
	}
	if strings.Contains(firstLine, `"isSidechain":true`) || strings.Contains(firstLine, `"isSidechain": true`) {
		return nil
	}
	customTitle := firstNonEmptyText(
		jsonFieldLastValue(tailText, "customTitle"),
		jsonFieldLastValue(headText, "customTitle"),
		jsonFieldLastValue(tailText, "aiTitle"),
		jsonFieldLastValue(headText, "aiTitle"),
	)
	summary := firstNonEmptyText(
		customTitle,
		jsonFieldLastValue(tailText, "lastPrompt"),
		jsonFieldLastValue(tailText, "summary"),
		firstPromptFromHead(headText),
	)
	if summary == "" {
		return nil
	}
	return &claudeSessionInfo{customTitle: customTitle, summary: summary}
}

// jsonFieldLastValue scans raw text for the last `"key":"value"` occurrence
// and JSON-decodes the value, mirroring the SDK's window scanning.
func jsonFieldLastValue(text string, key string) string {
	var result string
	for _, marker := range []string{`"` + key + `":"`, `"` + key + `": "`} {
		searchFrom := 0
		for {
			index := strings.Index(text[searchFrom:], marker)
			if index < 0 {
				break
			}
			start := searchFrom + index + len(marker)
			end := start
			for end < len(text) {
				if text[end] == '\\' {
					end += 2
					continue
				}
				if text[end] == '"' {
					if decoded := decodeJSONStringBody(text[start:end]); decoded != "" {
						result = decoded
					}
					break
				}
				end++
			}
			searchFrom = end + 1
			if searchFrom >= len(text) {
				break
			}
		}
	}
	return result
}

func decodeJSONStringBody(body string) string {
	if !strings.Contains(body, "\\") {
		return body
	}
	var decoded string
	if err := json.Unmarshal([]byte(`"`+body+`"`), &decoded); err != nil {
		return body
	}
	return decoded
}

func firstPromptFromHead(head string) string {
	for _, line := range strings.Split(head, "\n") {
		if !strings.Contains(line, `"type":"user"`) && !strings.Contains(line, `"type": "user"`) {
			continue
		}
		parsed := parseJSONObject(line)
		if parsed == nil {
			continue
		}
		if text := readUserMessageNotificationText(parsed); text != "" {
			return text
		}
	}
	return ""
}
