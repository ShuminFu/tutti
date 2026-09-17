package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestServiceScanSkipsDanglingTranscriptLinks 钉住扫描对**悬空链接**的处理。
//
// 恢复用的转录是把导入会话的原生正文链进受管配置根得来的（provider preparer 那一侧）：
// 用户根的正文被保留期剪掉或用户自己搬走之后，受管根里就留下一条悬空链接。它按扩展名
// 依旧像一份 `.jsonl`，直接去读会变成一条读不懂的导入错误（"no such file"），而那条
// 会话其实早就不在了 —— 跳过它，别的会话照常。
func TestServiceScanSkipsDanglingTranscriptLinks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	project := filepath.Join(root, "project-a")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("create project error = %v", err)
	}
	if canonical, ok := canonicalExistingDir(project); ok {
		project = canonical
	}
	claudeHome := filepath.Join(root, "claude-home")
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv(testClaudeExtraRootsEnv, filepath.Join(root, "user-claude"))

	now := time.Now().UTC().Format(time.RFC3339Nano)
	writeAgentServiceJSONL(t, filepath.Join(claudeHome, "projects", "project-a", "live.jsonl"),
		map[string]any{
			"timestamp": now, "sessionId": "claude-live", "cwd": project, "uuid": "live-1",
			"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Still here"}}},
		},
	)
	// 一条悬空链接：目标不存在，但文件名依旧像一份转录。
	dangling := filepath.Join(claudeHome, "projects", "project-a", "pruned.jsonl")
	if err := os.Symlink(filepath.Join(root, "gone", "pruned.jsonl"), dangling); err != nil {
		t.Fatalf("create dangling transcript link error = %v", err)
	}

	service := newIsolatedAgentService(newFakeRuntime())
	scan, err := service.ScanExternalImports(ctx, ExternalImportScanInput{Days: -1})
	if err != nil {
		t.Fatalf("ScanExternalImports error = %v", err)
	}
	if len(scan.Errors) != 0 {
		t.Fatalf("scan errors = %#v, want none for a dangling transcript link", scan.Errors)
	}
	if scan.ScannedSessions != 1 {
		t.Fatalf("scan = %#v, want exactly the live session", scan)
	}
}
