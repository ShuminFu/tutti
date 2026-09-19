package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStandardSkillDiscoveryAllProvidersAndRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd := t.TempDir()
	path := filepath.Join(cwd, ".agents", "skills", "review", "SKILL.md")
	writeSkill(t, path, "---\nname: review\ndescription: project review\n---\n")
	writeSkill(t, filepath.Join(cwd, ".claude", "skills", "review", "SKILL.md"), "---\nname: review\ndescription: native review\n---\n")
	for _, provider := range []string{"claude-code", "codex", "acp:deepseek-harness", "custom-agent"} {
		options := discoverComposerSkillOptions(provider, cwd, nil)
		var found bool
		for _, option := range options {
			if option.Name == "review" {
				found = true
				if option.Path != path || option.Invocation != "promptItem" {
					t.Fatalf("%s: %#v", provider, option)
				}
			}
		}
		if !found {
			t.Fatalf("%s: standard missing", provider)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, option := range discoverComposerSkillOptions("codex", cwd, nil) {
		if option.Name == "review" {
			t.Fatal("deleted skill retained")
		}
	}
}

func TestNativeSkillReferencesRespectDisabledAndNamespace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "review", "SKILL.md")
	writeSkill(t, path, "---\nname: review\ndescription: review plugin\n---\n")
	got := nativeSkillReferencesFromOptions([]ComposerCapabilityOption{
		{Kind: "skill", Name: "disabled:review", Path: path, Status: "disabled"},
		{Kind: "skill", Name: "plugin:review", Path: path, Status: "available"},
		{Kind: "skill", Name: "mismatch", Path: path, Status: "available"},
	})
	if len(got) != 1 || got[0].PluginName != "plugin" || got[0].Path != path {
		t.Fatalf("references=%#v", got)
	}
}

func TestLocalSkillMenuSelectionUsesRuntimeMatching(t *testing.T) {
	option := ComposerSkillOption{Name: "review", Path: "/tmp/.agents/review/SKILL.md", Trigger: "/review"}
	for _, selected := range []string{"review", "/review", "$review", option.Path} {
		if !workspaceAgentComposerSkillSelected(option, []string{selected}) {
			t.Errorf("missing %q", selected)
		}
	}
	for _, selected := range []string{"Review", "/Review", "/tmp/.agents/Review/SKILL.md"} {
		if workspaceAgentComposerSkillSelected(option, []string{selected}) {
			t.Errorf("case widened %q", selected)
		}
	}
	option.PluginName = "plugin"
	if workspaceAgentComposerSkillSelected(option, []string{"review"}) || !workspaceAgentComposerSkillSelected(option, []string{"$plugin:review"}) {
		t.Fatal("plugin namespace mismatch")
	}
}
