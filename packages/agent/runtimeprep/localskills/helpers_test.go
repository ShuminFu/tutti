package localskills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent of %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func writeSkill(t *testing.T, dir string, frontmatter ...string) string {
	t.Helper()
	lines := []string{"---"}
	lines = append(lines, frontmatter...)
	lines = append(lines, "---", "", "Body text.", "")
	return writeFile(t, filepath.Join(dir, "SKILL.md"), strings.Join(lines, "\n"))
}

// writeValidSkill writes a SKILL.md that only differs in name and description.
func writeValidSkill(t *testing.T, dir string, name string, description string) string {
	t.Helper()
	return writeSkill(t, dir, "name: "+name, "description: "+description)
}

func projectRoot(path string) Root {
	return Root{Path: path, SourceKind: SourceKindProject}
}

func personalRoot(path string) Root {
	return Root{Path: path, SourceKind: SourceKindPersonal}
}

func skillNames(catalog Catalog) []string {
	names := make([]string, 0, len(catalog.Skills))
	for _, skill := range catalog.Skills {
		names = append(names, Identity(skill))
	}
	return names
}

func requireNames(t *testing.T, catalog Catalog, want ...string) {
	t.Helper()
	got := skillNames(catalog)
	if len(got) != len(want) {
		t.Fatalf("skills = %v, want %v (diagnostics: %v)", got, want, catalog.Diagnostics)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("skills = %v, want %v (diagnostics: %v)", got, want, catalog.Diagnostics)
		}
	}
}

func requireDiagnosticContaining(t *testing.T, catalog Catalog, want string) {
	t.Helper()
	for _, diagnostic := range catalog.Diagnostics {
		if strings.Contains(diagnostic, want) {
			return
		}
	}
	t.Fatalf("diagnostics %v do not contain %q", catalog.Diagnostics, want)
}

func requireNoDiagnostics(t *testing.T, catalog Catalog) {
	t.Helper()
	if len(catalog.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", catalog.Diagnostics)
	}
}

func requireDiagnosticCount(t *testing.T, catalog Catalog, want int) {
	t.Helper()
	if len(catalog.Diagnostics) != want {
		t.Fatalf("diagnostics = %v, want %d", catalog.Diagnostics, want)
	}
}

func findSkill(t *testing.T, catalog Catalog, identity string) Skill {
	t.Helper()
	for _, skill := range catalog.Skills {
		if Identity(skill) == identity {
			return skill
		}
	}
	t.Fatalf("skill %q not found in %v", identity, skillNames(catalog))
	return Skill{}
}
