package runtimeprep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep/localskills"
)

func TestLocalSkillsClaudeReferenceRefreshAndNativeResources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cwd := t.TempDir()
	source := filepath.Join(cwd, ".agents", "review", "SKILL.md")
	writeSidecarTestFile(t, source, "---\nname: review\ndescription: project review\n---\nRead references/rules.md.\n")
	root := filepath.Join(t.TempDir(), "plugin", "skills")
	input := PrepareInput{Cwd: cwd}
	if err := installLocalClaudeSkillReferences(root, input); err != nil {
		t.Fatal(err)
	}
	proxy := filepath.Join(root, "review", "SKILL.md")
	body, err := os.ReadFile(proxy)
	if err != nil || !strings.Contains(string(body), source) || strings.Contains(string(body), "Read references/rules.md.") {
		t.Fatalf("reference=%s %v", body, err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := installLocalClaudeSkillReferences(root, input); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(proxy); !os.IsNotExist(err) {
		t.Fatalf("stale reference retained: %v", err)
	}
}

func TestLocalSkillsNativeSourceNamespaceAndRemoval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	source := filepath.Join(t.TempDir(), "plugin", "SKILL.md")
	writeSidecarTestFile(t, source, "---\nname: review\ndescription: plugin review\n---\n")
	native := localskills.Skill{Name: "review", Path: source, PluginName: "plugin", SourceKind: "plugin"}
	selection := LocalSkillSelection{Native: []localskills.Skill{native}, Selected: []string{"plugin:review"}, Explicit: true}
	catalog := LocalSkillCatalog(t.TempDir(), selection)
	if len(catalog.Skills) != 2 || localskills.Identity(catalog.Skills[0]) != "plugin:review" {
		t.Fatalf("catalog=%#v", catalog)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	catalog = LocalSkillCatalog(t.TempDir(), selection)
	if len(catalog.Skills) != 1 || len(catalog.Diagnostics) != 1 {
		t.Fatalf("removed=%#v", catalog)
	}
}
