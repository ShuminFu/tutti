package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCursorSkill(t *testing.T, root, folder, name string) string {
	t.Helper()
	dir := filepath.Join(root, ".cursor", "skills", folder)
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: Imported skill\n---\nRead references/guide.md\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "guide.md"), []byte("guide content"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCursorSkillImportPreservesResourcesAndSkipsConflicts(t *testing.T) {
	project := t.TempDir()
	writeCursorSkill(t, project, "source-folder", "portable-skill")
	selected := filepath.Join(project, ".cursor")
	preview, err := PreviewCursorSkills(selected)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Skills) != 1 || preview.Skills[0].Name != "portable-skill" || preview.Skills[0].Status != "ready" {
		t.Fatalf("preview = %+v", preview)
	}
	results, err := ImportCursorSkills(selected, []string{"portable-skill"})
	if err != nil || len(results) != 1 || results[0].Status != "imported" {
		t.Fatalf("import = %+v, %v", results, err)
	}
	resource := filepath.Join(project, ".agents", "skills", "portable-skill", "references", "guide.md")
	content, err := os.ReadFile(resource)
	if err != nil || string(content) != "guide content" {
		t.Fatalf("resource = %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(project, ".cursor", "skills", "source-folder", "SKILL.md")); err != nil {
		t.Fatalf("Cursor source changed: %v", err)
	}
	preview, err = PreviewCursorSkills(selected)
	if err != nil || preview.Skills[0].Status != "exists" {
		t.Fatalf("second preview = %+v, %v", preview, err)
	}
	results, err = ImportCursorSkills(selected, []string{"portable-skill"})
	if err != nil || results[0].Status != "exists" {
		t.Fatalf("second import = %+v, %v", results, err)
	}
}

func TestCursorSkillImportRejectsSymlinksAndInvalidRoots(t *testing.T) {
	project := t.TempDir()
	dir := writeCursorSkill(t, project, "unsafe", "unsafe-skill")
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "references", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	preview, err := PreviewCursorSkills(filepath.Join(project, ".cursor", "skills"))
	if err != nil || len(preview.Skills) != 1 || preview.Skills[0].Status != "unsafe" {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if _, err := PreviewCursorSkills(filepath.Join(project, "other", "skills")); err == nil {
		t.Fatal("accepted non-Cursor root")
	}
	if _, err := os.Stat(filepath.Join(project, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("preview wrote destination: %v", err)
	}
}

func TestCursorSkillImportDetectsExistingIdentityInBareAgentsRoot(t *testing.T) {
	project := t.TempDir()
	writeCursorSkill(t, project, "from-cursor", "same-name")
	existing := filepath.Join(project, ".agents", "custom-folder")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "SKILL.md"), []byte("---\nname: same-name\ndescription: Existing\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewCursorSkills(filepath.Join(project, ".cursor", "skills"))
	if err != nil || len(preview.Skills) != 1 || preview.Skills[0].Status != "exists" {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	results, err := ImportCursorSkills(filepath.Join(project, ".cursor", "skills"), []string{"same-name"})
	if err != nil || len(results) != 1 || results[0].Status != "exists" {
		t.Fatalf("import = %+v, %v", results, err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "same-name")); !os.IsNotExist(err) {
		t.Fatal("overwrote existing identity")
	}
}

func TestCursorSkillImportRejectsSymlinkDestination(t *testing.T) {
	project := t.TempDir()
	writeCursorSkill(t, project, "valid", "valid-skill")
	if err := os.Symlink(t.TempDir(), filepath.Join(project, ".agents")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := ImportCursorSkills(filepath.Join(project, ".cursor", "skills"), []string{"valid-skill"})
	if err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("expected symlink destination rejection, got %v", err)
	}
}

func TestCursorSkillCopyRejectsLateSourceSymlink(t *testing.T) {
	project := t.TempDir()
	skillDir := writeCursorSkill(t, project, "late-link", "late-link")
	source, err := os.OpenRoot(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination := filepath.Join(project, ".agents", "skills")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	target, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.md"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(skillDir, "references")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skillDir, "references")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := copyCursorSkillAtomically(source, target, "late-link"); err == nil {
		t.Fatal("copied through a symlink added after source selection")
	}
	if _, err := os.Stat(filepath.Join(destination, "late-link")); !os.IsNotExist(err) {
		t.Fatalf("partial destination remains: %v", err)
	}
}

func TestCursorSkillCopyNeverReplacesConcurrentDestination(t *testing.T) {
	project := t.TempDir()
	skillDir := writeCursorSkill(t, project, "source", "same-name")
	source, err := os.OpenRoot(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination := filepath.Join(project, ".agents", "skills")
	existing := filepath.Join(destination, "same-name")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(existing, "marker")
	if err := os.WriteFile(marker, []byte("existing data"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if err := copyCursorSkillAtomically(source, target, "same-name"); err == nil {
		t.Fatal("replaced an existing destination")
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "existing data" {
		t.Fatalf("existing destination changed: %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(existing, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("published copied data over existing destination: %v", err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed import left staged copies: %v, %v", entries, err)
	}
}
