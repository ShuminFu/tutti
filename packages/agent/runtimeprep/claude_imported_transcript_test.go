package runtimeprep

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 这一组覆盖的是「导入进来的 claude 会话到底续不续得上」。
//
// 关键前提（2026-09-17 用真机 claude 2.1.274 与 pinned SDK 逐条实测）：`claude --resume
// <uuid>` 与 sidecar 走的 Agent SDK **只认「进程 cwd 推出来的那一个 project 目录」**——
// `projects/<claudeProjectId(cwd)>/<session id>.jsonl`。把正文放在别的 project 目录名下
// 会说 "No conversation found with session ID"，把目录名换成该正文自己记录的 cwd、进程 cwd
// 换一个，同样找不到。所以「把正文接进恢复用的配置根」必须按**恢复时的 cwd**算目录名，
// 不能照抄正文原来所在的目录名（正文的 cwd 通常是 linked worktree，而导入会话的 cwd
// 被折到了主检出，两者不同 —— 这正是本机那 3 条导入会话的形态）。

func TestClaudeProjectDirMatchesPinnedSDKLocalProjectKey(t *testing.T) {
	// 期望值由 pinned SDK 的同一算法算出（packages/agent/claude-sdk-sidecar/src/
	// goalTranscript.ts 的 claudeProjectId），路径都不存在，避免 EvalSymlinks
	// 让期望值随机器而变。
	cases := []struct {
		cwd  string
		want string
	}{
		{"/opt/rnd/Projects/demo", "-opt-rnd-Projects-demo"},
		{"/opt/rnd/work trees/demo-dev/cliagent-backend", "-opt-rnd-work-trees-demo-dev-cliagent-backend"},
		{"/opt/rnd/Projects/edge__state.json", "-opt-rnd-Projects-edge--state-json"},
		// 非 ASCII 按 UTF-16 码元逐个替换（每个汉字一个 '-'）。
		{"/opt/rnd/项目/dev", "-opt-rnd----dev"},
		// 超过 200 个字符：截断 + base36 哈希后缀。
		{
			"/opt/" + strings.Repeat("a", 210),
			"-opt-" + strings.Repeat("a", 195) + "-nwg5pn",
		},
	}
	for _, testCase := range cases {
		if got := claudeProjectDir(testCase.cwd); got != testCase.want {
			t.Errorf("claudeProjectDir(%q) = %q, want %q", testCase.cwd, got, testCase.want)
		}
	}
}

// TestExposeClaudeImportedTranscriptUsesResumeCwdProjectDir 是本修复的核心用例：
// 正文在**用户自己的根**里、目录名是它**原始 cwd**（一个 linked worktree）的 slug，
// 而恢复时进程 cwd 是**折叠后的主检出** —— 链接必须落在主检出 slug 那一层，否则
// CLI 永远读不到它（本机那 3 条导入会话就是被折叠过的）。
func TestExposeClaudeImportedTranscriptUsesResumeCwdProjectDir(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	setTestHome(t, home)
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)

	worktreeCwd := filepath.Join(root, "worktrees", "rndmaster-dev")
	mainCheckoutCwd := filepath.Join(root, "rndmaster")
	sessionID := "a2550f49-1a2a-4f76-9d3f-b35309bad00e"
	source := filepath.Join(home, ".claude", "projects",
		claudeProjectDir(worktreeCwd), sessionID+".jsonl")
	const body = `{"sessionId":"a2550f49","cwd":"worktree"}` + "\n"
	writeSidecarTestFile(t, source, body)

	input := ProviderPrepareInput{
		PrepareInput: PrepareInput{
			AgentSessionID:            "imported-claude-code-abc",
			Provider:                  "claude-code",
			Cwd:                       mainCheckoutCwd,
			ExternalRolloutSourcePath: source,
		},
	}
	exposeClaudeImportedTranscriptFile(input)

	// 落点按恢复 cwd 算（主检出），不是正文原来那一层（worktree）。
	target := filepath.Join(managedConfigDir, "projects",
		claudeProjectDir(mainCheckoutCwd), sessionID+".jsonl")
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("transcript not exposed under the resume cwd project dir: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("exposed transcript mode = %v, want symlink", info.Mode())
	}
	if runtime.GOOS != "windows" {
		linked, err := os.Readlink(target)
		if err != nil {
			t.Fatal(err)
		}
		if linked != source {
			t.Fatalf("link target = %q, want %q", linked, source)
		}
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read through the exposed transcript: %v", err)
	}
	if string(content) != body {
		t.Fatalf("content through the link = %q, want %q", content, body)
	}
	// 反证：**不能**在正文原来那一层留下第二条（CLI 不会读那里）。
	if _, err := os.Lstat(filepath.Join(managedConfigDir, "projects",
		claudeProjectDir(worktreeCwd), sessionID+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("transcript must not be exposed under the source's own project dir, err = %v", err)
	}
	// 幂等。
	exposeClaudeImportedTranscriptFile(input)
}

func TestExposeClaudeImportedTranscriptIsNoopWithoutImportedSource(t *testing.T) {
	root := t.TempDir()
	setTestHome(t, root)
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)

	exposeClaudeImportedTranscriptFile(ProviderPrepareInput{
		PrepareInput: PrepareInput{Provider: "claude-code", Cwd: root},
	})
	if _, err := os.Stat(managedConfigDir); !os.IsNotExist(err) {
		t.Fatalf("non-imported session must not create anything in the config root, err = %v", err)
	}
}

func TestExposeClaudeImportedTranscriptKeepsPanelOwnedSessionInPlace(t *testing.T) {
	root := t.TempDir()
	if canonical, ok := canonicalDirForTest(root); ok {
		root = canonical
	}
	setTestHome(t, root)
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)

	cwd := filepath.Join(root, "project-a")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionID := "panel-owned-session"
	source := filepath.Join(managedConfigDir, "projects", claudeProjectDir(cwd), sessionID+".jsonl")
	writeSidecarTestFile(t, source, "{}\n")

	// 正文本来就在恢复要读的那一层：这是面板自己跑的会话，不该再动任何东西。
	exposeClaudeImportedTranscriptFile(ProviderPrepareInput{
		PrepareInput: PrepareInput{Provider: "claude-code", Cwd: cwd, ExternalRolloutSourcePath: source},
	})
	entries, err := os.ReadDir(filepath.Dir(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("project dir entries = %#v, want only the original transcript", entries)
	}
	info, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("panel-owned transcript must not be replaced by a link")
	}
}

func TestExposeClaudeImportedTranscriptLeavesGoneSourceToTheRecreateFallback(t *testing.T) {
	root := t.TempDir()
	setTestHome(t, root)
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)

	// 正文过了保留期 / 被用户删了：什么都不做，交回既有的 recreate 降级。
	exposeClaudeImportedTranscriptFile(ProviderPrepareInput{
		PrepareInput: PrepareInput{
			Provider:                  "claude-code",
			Cwd:                       filepath.Join(root, "project-a"),
			ExternalRolloutSourcePath: filepath.Join(root, "gone", "sid.jsonl"),
		},
	})
	if _, err := os.Stat(managedConfigDir); !os.IsNotExist(err) {
		t.Fatalf("nothing should be created for a missing source, err = %v", err)
	}
}

func TestExposeClaudeImportedTranscriptDoesNotClobberAnOccupiedTarget(t *testing.T) {
	root := t.TempDir()
	if canonical, ok := canonicalDirForTest(root); ok {
		root = canonical
	}
	setTestHome(t, root)
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)

	cwd := filepath.Join(root, "project-a")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionID := "occupied-session"
	source := filepath.Join(root, "elsewhere", sessionID+".jsonl")
	writeSidecarTestFile(t, source, "imported copy\n")
	occupied := filepath.Join(managedConfigDir, "projects", claudeProjectDir(cwd), sessionID+".jsonl")
	writeSidecarTestFile(t, occupied, "panel copy\n")

	exposeClaudeImportedTranscriptFile(ProviderPrepareInput{
		PrepareInput: PrepareInput{Provider: "claude-code", Cwd: cwd, ExternalRolloutSourcePath: source},
	})
	content, err := os.ReadFile(occupied)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "panel copy\n" {
		t.Fatalf("occupied target = %q, want it untouched", content)
	}
}

func TestExposeClaudeImportedTranscriptSkipsRelativeSource(t *testing.T) {
	root := t.TempDir()
	setTestHome(t, root)
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)

	// 相对路径建出来的链接会以 daemon 自己的 cwd 为基准 —— 宁可不接。
	exposeClaudeImportedTranscriptFile(ProviderPrepareInput{
		PrepareInput: PrepareInput{
			Provider:                  "claude-code",
			Cwd:                       filepath.Join(root, "project-a"),
			ExternalRolloutSourcePath: filepath.Join("relative", "sid.jsonl"),
		},
	})
	if _, err := os.Stat(managedConfigDir); !os.IsNotExist(err) {
		t.Fatalf("relative source must not produce a link, err = %v", err)
	}
}

// TestExposeClaudeImportedTranscriptRepointsADanglingLink 覆盖「用户把自己的配置根搬走」
// 之后的下一轮恢复：目标位置上是我们上次建的链接、但它的目标已经不存在。留着它就是一条
// CLI 读不到的链接（等价于没接），所以就地重指。
func TestExposeClaudeImportedTranscriptRepointsADanglingLink(t *testing.T) {
	root := t.TempDir()
	if canonical, ok := canonicalDirForTest(root); ok {
		root = canonical
	}
	setTestHome(t, root)
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)

	cwd := filepath.Join(root, "project-a")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionID := "moved-session"
	source := filepath.Join(root, "new-user-claude", sessionID+".jsonl")
	writeSidecarTestFile(t, source, "moved copy\n")
	target := filepath.Join(managedConfigDir, "projects", claudeProjectDir(cwd), sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "old-user-claude", sessionID+".jsonl"), target); err != nil {
		t.Fatalf("create dangling link error = %v", err)
	}

	exposeClaudeImportedTranscriptFile(ProviderPrepareInput{
		PrepareInput: PrepareInput{Provider: "claude-code", Cwd: cwd, ExternalRolloutSourcePath: source},
	})
	linked, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("Readlink error = %v", err)
	}
	if linked != source {
		t.Fatalf("link target = %q, want it repointed at %q", linked, source)
	}
}

// TestExposeClaudeImportedTranscriptKeepsAForeignLink 反向：目标上的链接指向**另一个
// 存在**的文件（不是我们建的那条），就不能动它 —— 万一那是别人会话的正文。
func TestExposeClaudeImportedTranscriptKeepsAForeignLink(t *testing.T) {
	root := t.TempDir()
	if canonical, ok := canonicalDirForTest(root); ok {
		root = canonical
	}
	setTestHome(t, root)
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)

	cwd := filepath.Join(root, "project-a")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionID := "shared-id"
	source := filepath.Join(root, "user-claude", sessionID+".jsonl")
	writeSidecarTestFile(t, source, "imported copy\n")
	other := filepath.Join(root, "other-claude", sessionID+".jsonl")
	writeSidecarTestFile(t, other, "someone else\n")
	target := filepath.Join(managedConfigDir, "projects", claudeProjectDir(cwd), sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, target); err != nil {
		t.Fatalf("create foreign link error = %v", err)
	}

	exposeClaudeImportedTranscriptFile(ProviderPrepareInput{
		PrepareInput: PrepareInput{Provider: "claude-code", Cwd: cwd, ExternalRolloutSourcePath: source},
	})
	linked, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("Readlink error = %v", err)
	}
	if linked != other {
		t.Fatalf("link target = %q, want the foreign link left untouched", linked)
	}
}

// TestExposeClaudeImportedTranscriptAtTreatsAConcurrentWinnerAsSuccess 覆盖并发：
// 两条恢复同时进来，都 Lstat 到 target 不存在，其中一个 Symlink 成功后另一个拿到
// EEXIST。EEXIST 不是失败 —— 此刻目标已经指向这份正文，恢复照样能读到。
func TestExposeClaudeImportedTranscriptAtTreatsAConcurrentWinnerAsSuccess(t *testing.T) {
	root := t.TempDir()
	if canonical, ok := canonicalDirForTest(root); ok {
		root = canonical
	}
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)
	sessionID := "raced-session"
	source := filepath.Join(root, "user-claude", sessionID+".jsonl")
	writeSidecarTestFile(t, source, "raced copy\n")
	target := filepath.Join(managedConfigDir, "projects", claudeProjectDir(root), sessionID+".jsonl")

	// 「另一个自己」先把链接建好，然后我们装作不知道地再建一次。
	exposeClaudeImportedTranscriptFile(ProviderPrepareInput{
		PrepareInput: PrepareInput{Provider: "claude-code", Cwd: root, ExternalRolloutSourcePath: source},
	})
	if err := exposeClaudeImportedTranscriptAt(target, source, ProviderPrepareInput{}); err != nil {
		t.Fatalf("exposeClaudeImportedTranscriptAt() error = %v, want nil when the target already points at the source", err)
	}
}

// TestExposeClaudeImportedTranscriptDegradesWhenTheConfigRootIsUnusable 钉住
// 「宁可降级也不要让整条恢复准备失败」：配置根不可用时，Prepare 必须照常返回（用户还能
// 发消息，界面另有 system_notice 说明历史没接上），而不是整个会话起不来。
// （这个 fixture 里先失败的是 Lstat —— ENOTDIR 不算 fs.ErrNotExist，所以走的是
// "stat link target" 那条降级；契约本身与 MkdirAll 失败时一致。）
func TestExposeClaudeImportedTranscriptDegradesWhenTheConfigRootIsUnusable(t *testing.T) {
	root := t.TempDir()
	setTestHome(t, root)
	// 把配置根做成一个**普通文件**：对它下面的路径做任何操作都不可能成功。
	brokenConfigDir := filepath.Join(root, "managed-claude")
	if err := os.WriteFile(brokenConfigDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", brokenConfigDir)

	source := filepath.Join(root, "user-claude", "sid.jsonl")
	writeSidecarTestFile(t, source, "{}\n")
	cwd := filepath.Join(root, "project-a")
	exposeClaudeImportedTranscriptFile(ProviderPrepareInput{
		PrepareInput: PrepareInput{Provider: "claude-code", Cwd: cwd, ExternalRolloutSourcePath: source},
	})
	content, err := os.ReadFile(brokenConfigDir)
	if err != nil {
		t.Fatalf("config root file must be left alone: %v", err)
	}
	if string(content) != "not a directory" {
		t.Fatalf("config root file = %q, want it untouched", content)
	}
	// 真正的契约：整条 Prepare 照样成功，用户还能发消息（历史没接上另有 system_notice）。
	preparer := ClaudeCodePreparer{StateDir: t.TempDir()}
	if _, err := preparer.Prepare(context.Background(), ProviderPrepareInput{
		PrepareInput: testResolvedInput(t, PrepareInput{
			AgentSessionID: "imported-broken-root", AgentTargetID: "local:claude-code",
			Provider: "claude-code", Cwd: cwd, ExternalRolloutSourcePath: source,
		}),
		RuntimeRoot: t.TempDir(),
		Store:       LocalStore{StateDir: t.TempDir()},
	}); err != nil {
		t.Fatalf("Prepare() error = %v, want the session to stay usable when the config root is unusable", err)
	}
}

// TestClaudeCodePreparerExposesImportedTranscript 把钩子接在真入口上：Prepare 返回前
// 必须已经铺好，否则恢复时 CLI 照样找不到正文（这正是第一版修复踩空的地方）。
func TestClaudeCodePreparerExposesImportedTranscript(t *testing.T) {
	root := t.TempDir()
	if canonical, ok := canonicalDirForTest(root); ok {
		root = canonical
	}
	setTestHome(t, root)
	managedConfigDir := filepath.Join(root, "managed-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", managedConfigDir)

	cwd := filepath.Join(root, "project-a")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionID := "preparer-session"
	source := filepath.Join(root, "user-claude", "projects", "old-slug", sessionID+".jsonl")
	writeSidecarTestFile(t, source, "{}\n")

	preparer := ClaudeCodePreparer{StateDir: t.TempDir()}
	prepared, err := preparer.Prepare(context.Background(), ProviderPrepareInput{
		PrepareInput: testResolvedInput(t, PrepareInput{
			AgentSessionID:            "imported-" + sessionID,
			AgentTargetID:             "local:claude-code",
			Provider:                  "claude-code",
			Cwd:                       cwd,
			ExternalRolloutSourcePath: source,
		}),
		RuntimeRoot: t.TempDir(),
		Store:       LocalStore{StateDir: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if want := cwd; prepared.Cwd != want {
		t.Fatalf("prepared cwd = %q, want %q", prepared.Cwd, want)
	}
	target := filepath.Join(managedConfigDir, "projects", claudeProjectDir(cwd), sessionID+".jsonl")
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("Prepare() did not expose the imported transcript: %v", err)
	}
}

func canonicalDirForTest(path string) (string, bool) {
	if evaluated, err := filepath.EvalSymlinks(path); err == nil {
		return evaluated, true
	}
	return path, false
}
