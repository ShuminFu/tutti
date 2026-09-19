package agent

import (
	"context"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep/localskills"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type extensionComposerProfileResolverStub struct {
	profile ExtensionComposerProfile
}

func (s extensionComposerProfileResolverStub) ResolveExtensionComposerProfile(context.Context, string) (ExtensionComposerProfile, error) {
	return s.profile, nil
}

func TestDiscoverComposerSkillOptionsUsesExtensionDeclaredRoots(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	repoDir := filepath.Join(tempDir, "repo")
	cwd := filepath.Join(repoDir, "packages", "app")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	writeSkill(t, filepath.Join(repoDir, ".gemini", "skills", "project-review", "SKILL.md"), `---
name: project-review
description: Review this project.
---
`)
	writeSkill(t, filepath.Join(homeDir, ".agents", "skills", "personal-review", "SKILL.md"), `---
name: personal-review
description: Review any project.
---
`)
	service := newIsolatedAgentService(newFakeRuntime())
	service.ExtensionComposerProfiles = extensionComposerProfileResolverStub{
		profile: ExtensionComposerProfile{Skills: &ExtensionComposerSkillProfile{
			Invocation:    "textTrigger",
			TriggerPrefix: "/skill:",
			Roots: []ExtensionComposerSkillRoot{
				{Scope: "workspace", Path: ".gemini/skills"},
				{Scope: "user", Path: ".agents/skills"},
			},
		}},
	}
	options := service.discoverComposerSkillOptionsForLaunch(
		context.Background(),
		"acp:gemini",
		cwd,
		nil,
		map[string]any{"kind": "agent_extension", "extensionInstallationId": "gemini@1.0.1"},
	)
	if got := composerSkillOptionTriggers(options); !slices.Equal(got, []string{"/skill:project-review", "/personal-review", "/skill-creator"}) {
		t.Fatalf("extension skill triggers = %#v", got)
	}
	for _, option := range options {
		if option.Name == "project-review" && option.Invocation != "textTrigger" {
			t.Fatalf("extension skill invocation = %q", option.Invocation)
		}
	}
}

func TestDiscoverComposerSkillOptionsForExtensionHidesTuttiInjectedSkills(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	cwd := filepath.Join(repoDir, "packages", "app")
	writeSkill(t, filepath.Join(repoDir, ".agent_context", "skills", "browser-use", "SKILL.md"), `---
name: browser-use
description: Use Tutti browser automation.
---
`)
	writeSkill(t, filepath.Join(repoDir, ".agent_context", "skills", "hermes-native", "SKILL.md"), `---
name: hermes-native
description: Native Hermes skill.
---
`)
	service := newIsolatedAgentService(newFakeRuntime())
	service.ExtensionComposerProfiles = extensionComposerProfileResolverStub{
		profile: ExtensionComposerProfile{Skills: &ExtensionComposerSkillProfile{
			Invocation:    "textTrigger",
			TriggerPrefix: "/",
			Roots: []ExtensionComposerSkillRoot{
				{Scope: "workspace", Path: ".agent_context/skills"},
			},
		}},
	}
	options := service.discoverComposerSkillOptionsForLaunch(
		context.Background(),
		"acp:hermes",
		cwd,
		nil,
		map[string]any{"kind": "agent_extension", "extensionInstallationId": "hermes@0.1.0"},
	)
	if got := composerSkillOptionTriggers(options); !slices.Equal(got, []string{"/hermes-native", "/skill-creator"}) {
		t.Fatalf("extension skill triggers = %#v, want native Hermes skills without Tutti-injected browser-use", got)
	}
}

func TestDiscoverComposerSkillOptionsCodexUsesProviderNativeTriggers(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	repoDir := filepath.Join(tempDir, "repo")
	cwd := filepath.Join(repoDir, "packages", "app")
	codexHome := filepath.Join(tempDir, "runtime", "codex-home")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	writeSkill(t, filepath.Join(repoDir, ".codex", "skills", "architecture-review", "SKILL.md"), `---
name: architecture-review
description: Review architecture changes.
---

Review repository changes.
`)
	writeSkill(t, filepath.Join(homeDir, ".agents", "skills", "lark-doc", "SKILL.md"), `---
name: lark-doc
description: >
  Work with Lark documents.
  Search and edit cloud docs.
---
`)
	writeSkill(t, filepath.Join(homeDir, ".agents", "skills", "broken-agents", "SKILL.md"), `description: Missing frontmatter delimiter.
---
`)
	writeSkill(t, filepath.Join(homeDir, ".codex", "skills", "caveman", "SKILL.md"), `---
name: caveman
description: >
  Ultra-compressed communication mode.
  Use when the user asks to be brief.
---
`)
	writeSkill(t, filepath.Join(homeDir, ".codex", "skills", "broken-codex", "SKILL.md"), `---
name: broken-codex
description: Missing closing delimiter.
`)
	writeSkill(t, filepath.Join(homeDir, ".codex", "skills", ".system", "hidden", "SKILL.md"), `---
name: hidden
description: Hidden system skill.
---
`)
	writeSkill(t, filepath.Join(codexHome, "skills", ".system", "imagegen", "SKILL.md"), `---
name: imagegen
description: Generate images.
---
`)
	writeSkill(t, filepath.Join(codexHome, "skills", "tutti-cli", "SKILL.md"), `---
name: tutti-cli
description: Internal Tutti CLI.
---
`)

	options := discoverComposerSkillOptions("codex", cwd, []string{
		"CODEX_HOME=" + codexHome,
	})

	triggers := composerSkillOptionTriggers(options)
	want := []string{"$architecture-review", "$caveman", "/lark-doc", "$imagegen", "/skill-creator"}
	if !equalStringSlices(triggers, want) {
		t.Fatalf("triggers = %#v, want %#v", triggers, want)
	}
	if options[0].SourceKind != "project" || options[1].SourceKind != "personal" || options[2].SourceKind != "personal" || options[3].SourceKind != "system" {
		t.Fatalf("source kinds = %#v", options)
	}
	if options[1].Description != "Ultra-compressed communication mode. Use when the user asks to be brief." {
		t.Fatalf("codex personal description = %q", options[1].Description)
	}
	if options[2].Description != "Work with Lark documents. Search and edit cloud docs." {
		t.Fatalf("folded description = %q", options[2].Description)
	}
}

func TestDiscoverComposerSkillOptionsClaudeUsesSlashAndPluginNamespace(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	repoDir := filepath.Join(tempDir, "repo")
	cwd := filepath.Join(repoDir, "apps", "desktop")
	pluginDir := filepath.Join(tempDir, "plugins", "product-design")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	writeSkill(t, filepath.Join(repoDir, ".claude", "skills", "summarize", "SKILL.md"), `---
name: summarize
description: Summarize changes.
---
`)
	writeSkill(t, filepath.Join(homeDir, ".claude", "skills", "personal-review", "SKILL.md"), `---
name: personal-review
description: Review personal workflow.
---
`)
	writeSkill(t, filepath.Join(pluginDir, "skills", "frontend-design", "SKILL.md"), `---
name: frontend-design
description: Design frontend UI.
---
`)
	writeSkill(t, filepath.Join(pluginDir, "skills", "tutti-cli", "SKILL.md"), `---
name: tutti-cli
description: Internal Tutti CLI.
---
`)

	options := discoverComposerSkillOptions("claude-code", cwd, []string{
		"TUTTI_CLAUDE_PLUGIN_DIR=" + pluginDir,
	})

	triggers := composerSkillOptionTriggers(options)
	want := []string{"/summarize", "/personal-review", "/product-design:frontend-design", "/skill-creator"}
	if !equalStringSlices(triggers, want) {
		t.Fatalf("triggers = %#v, want %#v", triggers, want)
	}
	if options[2].PluginName != "product-design" || options[2].SourceKind != "plugin" {
		t.Fatalf("plugin option = %#v", options[2])
	}
}

func TestDiscoverComposerSkillOptionsCursorUsesPluginDir(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	repoDir := filepath.Join(tempDir, "repo")
	cwd := filepath.Join(repoDir, "apps", "desktop")
	pluginDir := filepath.Join(tempDir, "plugins", "tutti-cli")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	writeSkill(t, filepath.Join(repoDir, ".cursor", "skills", "project-skill", "SKILL.md"), `---
name: project-skill
description: Project Cursor skill.
---
`)
	writeSkill(t, filepath.Join(homeDir, ".cursor", "skills", "personal-skill", "SKILL.md"), `---
name: personal-skill
description: Personal Cursor skill.
---
`)
	writeSkill(t, filepath.Join(pluginDir, "skills", "workflow-check", "SKILL.md"), `---
name: workflow-check
description: Runtime Cursor plugin skill.
---
`)
	writeSkill(t, filepath.Join(pluginDir, "skills", "tutti-cli", "SKILL.md"), `---
name: tutti-cli
description: Internal Tutti CLI.
---
`)

	options := discoverComposerSkillOptions("cursor", cwd, []string{
		"TUTTI_CURSOR_PLUGIN_DIR=" + pluginDir,
	})

	triggers := composerSkillOptionTriggers(options)
	want := []string{"$project-skill", "$personal-skill", "$workflow-check", "/skill-creator"}
	if !equalStringSlices(triggers, want) {
		t.Fatalf("triggers = %#v, want %#v", triggers, want)
	}
	if options[2].PluginName != "tutti-cli" || options[2].SourceKind != "plugin" {
		t.Fatalf("plugin option = %#v", options[2])
	}
}

func TestDiscoverComposerSkillOptionsOpenCodeUsesNativeAndCompatibleRoots(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	repoDir := filepath.Join(tempDir, "repo")
	cwd := filepath.Join(repoDir, "apps", "desktop")
	customConfigDir := filepath.Join(tempDir, "opencode-config")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	writeSkill(t, filepath.Join(repoDir, ".opencode", "skills", "project-open", "SKILL.md"), `---
name: project-open
description: Project OpenCode skill.
---
`)
	writeSkill(t, filepath.Join(repoDir, ".claude", "skills", "project-claude", "SKILL.md"), `---
name: project-claude
description: Claude-compatible OpenCode skill.
---
`)
	writeSkill(t, filepath.Join(repoDir, ".agents", "skills", "project-agent", "SKILL.md"), `---
name: project-agent
description: Agent-compatible OpenCode skill.
---
`)
	writeSkill(t, filepath.Join(homeDir, ".config", "opencode", "skills", "personal-open", "SKILL.md"), `---
name: personal-open
description: Personal OpenCode skill.
---
`)
	writeSkill(t, filepath.Join(homeDir, ".claude", "skills", "personal-claude", "SKILL.md"), `---
name: personal-claude
description: Personal Claude-compatible skill.
---
`)
	writeSkill(t, filepath.Join(homeDir, ".agents", "skills", "personal-agent", "SKILL.md"), `---
name: personal-agent
description: Personal agent-compatible skill.
---
`)
	writeSkill(t, filepath.Join(customConfigDir, "opencode", "skills", "custom-config", "SKILL.md"), `---
name: custom-config
description: Custom OpenCode config skill.
---
`)

	options := discoverComposerSkillOptions("opencode", cwd, []string{
		"OPENCODE_CONFIG_DIR=" + customConfigDir,
	})

	triggers := composerSkillOptionTriggers(options)
	want := []string{
		"/project-agent",
		"/project-claude",
		"/project-open",
		"/custom-config",
		"/personal-agent",
		"/personal-claude",
		"/personal-open",
		"/skill-creator",
	}
	if !equalStringSlices(triggers, want) {
		t.Fatalf("triggers = %#v, want %#v", triggers, want)
	}
	if options[0].SourceKind != "project" || options[3].SourceKind != "personal" {
		t.Fatalf("source kinds = %#v", options)
	}
}

func TestSkillDiagnosticsRefreshAfterFix(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "broken", "SKILL.md")
	writeSkill(t, path, "missing frontmatter")
	first := localskills.Discover([]localskills.Root{{Path: root}})
	if len(first.Diagnostics) != 1 || !strings.Contains(first.Diagnostics[0], path) {
		t.Fatalf("diagnostics=%#v", first.Diagnostics)
	}
	writeSkill(t, path, "---\nname: repaired\ndescription: fixed\n---\n")
	next := localskills.Discover([]localskills.Root{{Path: root}})
	if len(next.Diagnostics) != 0 || len(next.Skills) != 1 {
		t.Fatalf("repaired=%#v", next)
	}
}

func TestReadSkillMetadataSupportsFoldedDescription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SKILL.md")
	writeSkill(t, path, `---
name: lark-whiteboard
version: 1.0.0
description: >
  飞书画板：查询和编辑飞书云文档中的画板。
  支持导出画板为预览图片、导出原始节点结构。
metadata:
  requires:
    bins: ["lark-cli"]
---
`)

	metadata, ok := readSkillMetadata(path)
	if !ok {
		t.Fatalf("readSkillMetadata() ok = false, want true")
	}
	if metadata.name != "lark-whiteboard" {
		t.Fatalf("name = %q", metadata.name)
	}
	want := "飞书画板：查询和编辑飞书云文档中的画板。 支持导出画板为预览图片、导出原始节点结构。"
	if metadata.description != want {
		t.Fatalf("description = %q, want %q", metadata.description, want)
	}
}

func TestReadSkillMetadataRejectsMissingDelimitedFrontmatter(t *testing.T) {
	tempDir := t.TempDir()
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "missing start delimiter",
			content: `name: broken
---
`,
		},
		{
			name: "missing end delimiter",
			content: `---
name: broken
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(tempDir, test.name, "SKILL.md")
			writeSkill(t, path, test.content)

			metadata, ok := readSkillMetadata(path)

			if ok {
				t.Fatalf("readSkillMetadata() ok = true, want false")
			}
			if metadata.name != "" || metadata.description != "" {
				t.Fatalf("metadata = %#v, want empty", metadata)
			}
		})
	}
}

func writeSkill(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func composerSkillOptionTriggers(options []ComposerSkillOption) []string {
	triggers := make([]string, 0, len(options))
	for _, option := range options {
		triggers = append(triggers, option.Trigger)
	}
	return triggers
}

func equalStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
