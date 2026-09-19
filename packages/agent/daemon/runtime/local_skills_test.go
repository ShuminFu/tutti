package agentruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func localSkillFixture(t *testing.T) (Session, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd := t.TempDir()
	path := filepath.Join(cwd, ".agents", "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: review\ndescription: Review changes\n---\nUse references/guide.md.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Session{CWD: cwd}, path
}

func TestLocalSkillProjectionPreservesOriginalAndNativeCommands(t *testing.T) {
	session, path := localSkillFixture(t)
	for _, prompt := range []string{"/review inspect it", "$review\ninspect it"} {
		input := []PromptContentBlock{{Type: "text", Text: prompt}}
		got, err := projectLocalSkillPrompt(session, input, false)
		if err != nil || len(got) != 2 || got[0].Text != "inspect it" || !strings.Contains(got[1].Text, path) {
			t.Fatalf("projection=%#v err=%v", got, err)
		}
		if input[0].Text != prompt {
			t.Fatal("canonical content mutated")
		}
	}
	for _, prompt := range []string{"/compact", "mention /review in documentation", "```\n/review\n```", "/review/file.md", "$review_other"} {
		got, err := projectLocalSkillPrompt(session, []PromptContentBlock{{Type: "text", Text: prompt}}, false)
		if err != nil || len(got) != 1 || got[0].Text != prompt {
			t.Fatalf("native/prose changed: %#v %v", got, err)
		}
	}
}

func TestLocalSkillProjectionRejectsStaleOrForgedSelection(t *testing.T) {
	session, path := localSkillFixture(t)
	content := []PromptContentBlock{{Type: "text", Text: "Use this"}, {Type: "skill", Name: "review", Path: path}}
	if got, err := projectLocalSkillPrompt(session, content, false); err != nil || len(got) != 2 {
		t.Fatalf("valid=%#v %v", got, err)
	}
	content[1].Path = filepath.Join(session.CWD, ".agents", "skills", "other", "SKILL.md")
	if _, err := projectLocalSkillPrompt(session, content, false); err == nil {
		t.Fatal("forged path accepted")
	}
	content[1].Path = path
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := projectLocalSkillPrompt(session, content, false); err == nil {
		t.Fatal("deleted selection accepted")
	}
}

func TestLocalSkillProjectionHonorsSelectionAndInvocationPolicy(t *testing.T) {
	session, path := localSkillFixture(t)
	selection, _ := json.Marshal(runtimeprep.LocalSkillSelection{Explicit: true})
	selectionPath := filepath.Join(t.TempDir(), "selection.json")
	if err := os.WriteFile(selectionPath, selection, 0o600); err != nil {
		t.Fatal(err)
	}
	session.Env = []string{runtimeprep.LocalSkillsSelectionEnv + "=" + selectionPath}
	if _, err := projectLocalSkillPrompt(session, []PromptContentBlock{{Type: "skill", Name: "review", Path: path}}, false); err == nil {
		t.Fatal("allowlist bypassed")
	}
	session.Env = nil
	if err := os.WriteFile(path, []byte("---\nname: review\ndescription: hidden\nuser-invocable: false\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := projectLocalSkillPrompt(session, []PromptContentBlock{{Type: "text", Text: "/review"}}, false); err == nil {
		t.Fatal("hidden skill accepted")
	}
}

func TestLocalSkillProjectionBuiltinAndCatalogRefresh(t *testing.T) {
	session, _ := localSkillFixture(t)
	catalogPath := filepath.Join(t.TempDir(), "catalog.json")
	session.Env = []string{runtimeprep.LocalSkillsFileEnv + "=" + catalogPath}
	got, err := projectLocalSkillPrompt(session, []PromptContentBlock{{Type: "text", Text: "/skill-creator"}}, true)
	if err != nil || len(got) != 1 || !strings.Contains(got[0].Text, ".agents/skills") {
		t.Fatalf("creator=%#v %v", got, err)
	}
	data, err := os.ReadFile(catalogPath)
	if err != nil || !strings.Contains(string(data), "review") {
		t.Fatalf("catalog=%s %v", data, err)
	}
}
