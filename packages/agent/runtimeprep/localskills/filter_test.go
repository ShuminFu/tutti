package localskills

import (
	"path/filepath"
	"strings"
	"testing"
)

func testCatalog(t *testing.T) Catalog {
	t.Helper()
	dir := t.TempDir()
	return Catalog{
		Skills: []Skill{
			{
				Name:          "review",
				Description:   "Unqualified review.",
				Path:          filepath.Join(dir, "project", "SKILL.md"),
				SourceKind:    SourceKindProject,
				Automatic:     true,
				UserInvocable: true,
			},
			{
				Name:          "review",
				Description:   "Plugin review.",
				Path:          filepath.Join(dir, "plugin", "SKILL.md"),
				SourceKind:    SourceKindProject,
				PluginName:    "tutti",
				Automatic:     true,
				UserInvocable: true,
			},
			{
				Name:          "other",
				Description:   "Something else.",
				Path:          filepath.Join(dir, "personal", "SKILL.md"),
				SourceKind:    SourceKindPersonal,
				Automatic:     true,
				UserInvocable: true,
			},
		},
		Diagnostics: []string{"skill root /missing: permission denied"},
	}
}

func TestFilterWithoutExplicitSelectionReturnsCatalogUnchanged(t *testing.T) {
	catalog := testCatalog(t)
	cases := []struct {
		name     string
		selected []string
		explicit bool
	}{
		{name: "nothing selected", selected: nil, explicit: false},
		{name: "blank entries only", selected: []string{"", "   "}, explicit: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			filtered := Filter(catalog, testCase.selected, testCase.explicit)
			requireNames(t, filtered, "review", "tutti:review", "other")
			if len(filtered.Diagnostics) != len(catalog.Diagnostics) {
				t.Fatalf("diagnostics = %v, want %v preserved", filtered.Diagnostics, catalog.Diagnostics)
			}
		})
	}
}

func TestFilterAppliesSelectionWithoutTheExplicitFlag(t *testing.T) {
	catalog := testCatalog(t)

	// A caller that supplies entries has asked for a selection even when it
	// does not set the explicit flag; only an empty selection means "all".
	requireNames(t, Filter(catalog, []string{"other"}, false), "other")
}

func TestFilterMatchesSelectionTokens(t *testing.T) {
	catalog := testCatalog(t)
	cases := []struct {
		name  string
		token string
		want  []string
	}{
		{name: "unqualified name", token: "review", want: []string{"review"}},
		{name: "plugin identity", token: "tutti:review", want: []string{"tutti:review"}},
		{name: "slash name", token: "/review", want: []string{"review"}},
		{name: "dollar name", token: "$review", want: []string{"review"}},
		{name: "slash identity", token: "/tutti:review", want: []string{"tutti:review"}},
		{name: "dollar identity", token: "$tutti:review", want: []string{"tutti:review"}},
		{name: "trimmed", token: "  other  ", want: []string{"other"}},
		{name: "unknown name", token: "missing", want: nil},
		{name: "unsafe prefix only", token: "/", want: nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			filtered := Filter(catalog, []string{testCase.token}, true)
			requireNames(t, filtered, testCase.want...)
		})
	}
}

func TestFilterMatchesAbsolutePaths(t *testing.T) {
	catalog := testCatalog(t)
	target := catalog.Skills[2].Path

	requireNames(t, Filter(catalog, []string{target}, true), "other")
	requireNames(t, Filter(catalog, []string{target, catalog.Skills[0].Path}, true), "review", "other")
	requireNames(t, Filter(catalog, []string{filepath.Join(filepath.Dir(target), "absent.md")}, true))
}

func TestFilterIsCaseSensitive(t *testing.T) {
	catalog := testCatalog(t)

	requireNames(t, Filter(catalog, []string{"Review"}, true))
	requireNames(t, Filter(catalog, []string{"TUTTI:review"}, true))
	requireNames(t, Filter(catalog, []string{strings.ToUpper(catalog.Skills[0].Path)}, true))
}

func TestFilterKeepsPluginNamespacesSeparate(t *testing.T) {
	catalog := testCatalog(t)

	// A bare name must not reach into a plugin namespace, and an identity must
	// not reach the unqualified skill of the same name.
	requireNames(t, Filter(catalog, []string{"review"}, true), "review")
	requireNames(t, Filter(catalog, []string{"tutti:review"}, true), "tutti:review")
}

func TestFilterHidesNonUserInvocableSkills(t *testing.T) {
	catalog := testCatalog(t)
	catalog.Skills[0].UserInvocable = false
	hidden := catalog.Skills[0]

	filtered := Filter(catalog, []string{"review", "other"}, true)

	requireNames(t, filtered, "other")
	requireDiagnosticCount(t, filtered, len(catalog.Diagnostics)+1)
	requireDiagnosticContaining(t, filtered, hidden.Path)
	requireDiagnosticContaining(t, filtered, "not user-invocable")
	if len(filtered.Diagnostics) > 0 {
		for _, diagnostic := range catalog.Diagnostics {
			requireDiagnosticContaining(t, filtered, diagnostic)
		}
	}
}

func TestFilterHidesNonUserInvocableSkillOncePerSkill(t *testing.T) {
	catalog := testCatalog(t)
	catalog.Skills[0].UserInvocable = false

	filtered := Filter(catalog, []string{"review", "/review", "$review", catalog.Skills[0].Path}, true)

	requireNames(t, filtered)
	requireDiagnosticCount(t, filtered, len(catalog.Diagnostics)+1)
}

func TestFilterSelectsNonAutomaticSkillsExplicitly(t *testing.T) {
	catalog := testCatalog(t)
	catalog.Skills[1].Automatic = false

	requireNames(t, Filter(catalog, []string{"tutti:review"}, true), "tutti:review")
}

func TestFilterExplicitEmptySelectionReturnsNothing(t *testing.T) {
	catalog := testCatalog(t)

	filtered := Filter(catalog, nil, true)

	requireNames(t, filtered)
	if len(filtered.Diagnostics) != len(catalog.Diagnostics) {
		t.Fatalf("diagnostics = %v, want %v preserved", filtered.Diagnostics, catalog.Diagnostics)
	}
}

func TestFilterPreservesCatalogOrder(t *testing.T) {
	catalog := testCatalog(t)

	filtered := Filter(catalog, []string{"other", "review"}, true)

	requireNames(t, filtered, "review", "other")
}

func TestFilterKeepsPathAndSourceKind(t *testing.T) {
	catalog := testCatalog(t)

	filtered := Filter(catalog, []string{"other"}, true)

	skill := findSkill(t, filtered, "other")
	if skill.SourceKind != SourceKindPersonal {
		t.Fatalf("sourceKind = %q, want %q", skill.SourceKind, SourceKindPersonal)
	}
	if skill.Path != catalog.Skills[2].Path {
		t.Fatalf("path = %q, want %q", skill.Path, catalog.Skills[2].Path)
	}
}

func TestFilterWithEmptyCatalog(t *testing.T) {
	filtered := Filter(Catalog{}, []string{"review"}, true)

	requireNames(t, filtered)
	if len(filtered.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v, want none", filtered.Diagnostics)
	}
}
