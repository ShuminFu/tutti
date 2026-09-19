package localskills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// layoutFixture builds a small project tree below a git root so the discovery
// tests can exercise both container layouts and root ordering at once.
type layoutFixture struct {
	gitRoot string
	home    string
}

func newLayoutFixture(t *testing.T) layoutFixture {
	t.Helper()
	gitRoot := t.TempDir()
	writeFile(t, filepath.Join(gitRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	return layoutFixture{gitRoot: gitRoot, home: t.TempDir()}
}

func (fixture layoutFixture) skillsRoot(ancestor string) string {
	return filepath.Join(ancestor, ".agents", "skills")
}

func (fixture layoutFixture) flatRoot(ancestor string) string {
	return filepath.Join(ancestor, ".agents")
}

func TestDiscoverReadsBothLayoutsNearestFirst(t *testing.T) {
	fixture := newLayoutFixture(t)
	cwd := filepath.Join(fixture.gitRoot, "pkg", "app")
	writeValidSkill(t, filepath.Join(fixture.skillsRoot(cwd), "app-skill"), "app-skill", "Nearest ancestor.")
	writeValidSkill(t, filepath.Join(fixture.skillsRoot(filepath.Join(fixture.gitRoot, "pkg")), "pkg-skill"), "pkg-skill", "Middle ancestor.")
	writeValidSkill(t, filepath.Join(fixture.skillsRoot(fixture.gitRoot), "root-skill"), "root-skill", "Skills layout.")
	writeValidSkill(t, filepath.Join(fixture.flatRoot(fixture.gitRoot), "flat-skill"), "flat-skill", "Flat layout.")

	catalog := Discover(StandardRoots(cwd, fixture.home))

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "app-skill", "pkg-skill", "root-skill", "flat-skill")
	for _, skill := range catalog.Skills {
		if skill.SourceKind != SourceKindProject {
			t.Fatalf("skill %+v, want the project source kind", skill)
		}
	}
}

func TestDiscoverPrefersNearestAncestor(t *testing.T) {
	fixture := newLayoutFixture(t)
	cwd := filepath.Join(fixture.gitRoot, "pkg", "app")
	near := writeValidSkill(t, filepath.Join(fixture.skillsRoot(cwd), "shared"), "shared", "Near.")
	far := writeValidSkill(t, filepath.Join(fixture.skillsRoot(fixture.gitRoot), "shared"), "shared", "Far.")

	catalog := Discover(StandardRoots(cwd, fixture.home))

	requireNames(t, catalog, "shared")
	if skill := findSkill(t, catalog, "shared"); skill.Description != "Near." {
		t.Fatalf("description = %q, want the nearest ancestor to win", skill.Description)
	}
	requireDiagnosticContaining(t, catalog, near)
	requireDiagnosticContaining(t, catalog, far)
	requireDiagnosticContaining(t, catalog, "shadowed")
	requireDiagnosticCount(t, catalog, 1)
}

func TestDiscoverPrefersSkillsLayoutWithinOneAncestor(t *testing.T) {
	fixture := newLayoutFixture(t)
	cwd := filepath.Join(fixture.gitRoot, "pkg")
	winner := writeValidSkill(t, filepath.Join(fixture.skillsRoot(cwd), "shared"), "shared", "Skills layout.")
	loser := writeValidSkill(t, filepath.Join(fixture.flatRoot(cwd), "shared"), "shared", "Flat layout.")

	catalog := Discover(StandardRoots(cwd, fixture.home))

	if skill := findSkill(t, catalog, "shared"); skill.Description != "Skills layout." {
		t.Fatalf("description = %q, want the skills layout to win", skill.Description)
	}
	requireDiagnosticCount(t, catalog, 1)
	requireDiagnosticContaining(t, catalog, winner)
	requireDiagnosticContaining(t, catalog, loser)
}

func TestDiscoverReadsPersonalRoots(t *testing.T) {
	fixture := newLayoutFixture(t)
	cwd := filepath.Join(fixture.gitRoot, "pkg")
	writeValidSkill(t, filepath.Join(fixture.skillsRoot(fixture.home), "personal-skill"), "personal-skill", "Personal skills layout.")
	writeValidSkill(t, filepath.Join(fixture.flatRoot(fixture.home), "personal-flat"), "personal-flat", "Personal flat layout.")

	catalog := Discover(StandardRoots(cwd, fixture.home))

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "personal-skill", "personal-flat")
	for _, skill := range catalog.Skills {
		if skill.SourceKind != SourceKindPersonal {
			t.Fatalf("skill %+v, want the personal source kind", skill)
		}
	}
}

func TestDiscoverSortsDirectoryEntries(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"zebra", "alpha", "middle"} {
		writeValidSkill(t, filepath.Join(dir, name), name, "Sorted.")
	}

	catalog := Discover([]Root{projectRoot(dir)})

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "alpha", "middle", "zebra")
}

func TestDiscoverUsesFrontmatterNameOverDirectoryName(t *testing.T) {
	dir := t.TempDir()
	writeValidSkill(t, filepath.Join(dir, "directory-name"), "frontmatter-name", "Renamed.")

	catalog := Discover([]Root{projectRoot(dir)})

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "frontmatter-name")
}

func TestDiscoverFollowsSymlinkedSkillDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	writeValidSkill(t, target, "linked", "Reached through a link.")
	link := filepath.Join(dir, "abc-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	catalog := Discover([]Root{projectRoot(dir)})

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "linked")
	if skill := findSkill(t, catalog, "linked"); skill.Path != filepath.Join(link, "SKILL.md") {
		t.Fatalf("path = %q, want the first sorted entry", skill.Path)
	}
}

func TestDiscoverTreatsRepeatedRootsAsOneSkill(t *testing.T) {
	dir := t.TempDir()
	writeValidSkill(t, filepath.Join(dir, "once"), "once", "Seen twice.")

	catalog := Discover([]Root{projectRoot(dir), personalRoot(dir)})

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "once")
}

func TestDiscoverTreatsSymlinkedRootAsTheSameRoot(t *testing.T) {
	dir := t.TempDir()
	writeValidSkill(t, filepath.Join(dir, "once"), "once", "Seen twice.")
	link := filepath.Join(t.TempDir(), "linked-root")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	catalog := Discover([]Root{projectRoot(dir), personalRoot(link)})

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "once")
}

func TestDiscoverSkipsHiddenEntriesAndNeverRecurses(t *testing.T) {
	dir := t.TempDir()
	writeValidSkill(t, filepath.Join(dir, ".hidden"), "hidden", "Hidden directory.")
	writeValidSkill(t, filepath.Join(dir, "visible"), "visible", "Visible directory.")
	writeValidSkill(t, filepath.Join(dir, "visible", ".agents", "skills", "deep"), "deep", "Nested root.")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a directory\n")
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	catalog := Discover([]Root{projectRoot(dir)})

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "visible")
}

func TestDiscoverSkipsMissingRootSilently(t *testing.T) {
	catalog := Discover([]Root{
		projectRoot(filepath.Join(t.TempDir(), "missing", ".agents", "skills")),
		personalRoot(filepath.Join(t.TempDir(), "missing", ".agents")),
	})

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog)
}

func TestDiscoverDiagnosesRootReadFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions behave differently on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read a mode 0 directory")
	}
	dir := t.TempDir()
	writeValidSkill(t, filepath.Join(dir, "skill"), "skill", "Unreadable root.")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	catalog := Discover([]Root{projectRoot(dir)})

	requireNames(t, catalog)
	requireDiagnosticCount(t, catalog, 1)
	requireDiagnosticContaining(t, catalog, dir)
}

func TestDiscoverDiagnosesInvalidMetadataWithFileAndReason(t *testing.T) {
	dir := t.TempDir()
	broken := writeFile(t, filepath.Join(dir, "broken", "SKILL.md"), "# No frontmatter\n")
	writeValidSkill(t, filepath.Join(dir, "healthy"), "healthy", "Fine.")

	catalog := Discover([]Root{projectRoot(dir)})

	requireNames(t, catalog, "healthy")
	requireDiagnosticCount(t, catalog, 1)
	requireDiagnosticContaining(t, catalog, broken)
	requireDiagnosticContaining(t, catalog, "missing --- frontmatter delimiter")
}

func TestDiscoverDiagnosesInvalidOpenAIPolicy(t *testing.T) {
	dir := t.TempDir()
	writeValidSkill(t, filepath.Join(dir, "skill"), "skill", "Broken sidecar.")
	sidecar := writeFile(t, filepath.Join(dir, "skill", "agents", "openai.yaml"), "policy: [unclosed\n")

	catalog := Discover([]Root{projectRoot(dir)})

	requireNames(t, catalog)
	requireDiagnosticCount(t, catalog, 1)
	requireDiagnosticContaining(t, catalog, sidecar)
}

func TestDiscoverNamespacesPluginSkillsSeparately(t *testing.T) {
	dir := t.TempDir()
	writeValidSkill(t, filepath.Join(dir, "plugin", "review"), "review", "Plugin scoped.")
	writeValidSkill(t, filepath.Join(dir, "local", "review"), "review", "Unqualified.")

	catalog := Discover([]Root{
		{Path: filepath.Join(dir, "plugin"), SourceKind: SourceKindProject, PluginName: "tutti"},
		projectRoot(filepath.Join(dir, "local")),
	})

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "tutti:review", "review")
	if skill := findSkill(t, catalog, "tutti:review"); skill.PluginName != "tutti" {
		t.Fatalf("pluginName = %q, want tutti", skill.PluginName)
	}
	if skill := findSkill(t, catalog, "review"); skill.PluginName != "" {
		t.Fatalf("pluginName = %q, want an unqualified skill", skill.PluginName)
	}
}

func TestDiscoverReportsPluginShadowingInsideOneNamespace(t *testing.T) {
	dir := t.TempDir()
	winner := writeValidSkill(t, filepath.Join(dir, "first", "review"), "review", "First plugin.")
	loser := writeValidSkill(t, filepath.Join(dir, "second", "review"), "review", "Second plugin.")

	catalog := Discover([]Root{
		{Path: filepath.Join(dir, "first"), PluginName: "tutti"},
		{Path: filepath.Join(dir, "second"), PluginName: "tutti"},
	})

	requireNames(t, catalog, "tutti:review")
	requireDiagnosticCount(t, catalog, 1)
	requireDiagnosticContaining(t, catalog, winner)
	requireDiagnosticContaining(t, catalog, loser)
}

func TestDiscoverReflectsHotChanges(t *testing.T) {
	fixture := newLayoutFixture(t)
	cwd := filepath.Join(fixture.gitRoot, "pkg")
	roots := StandardRoots(cwd, fixture.home)
	skillDir := filepath.Join(fixture.skillsRoot(cwd), "hot")
	writeValidSkill(t, skillDir, "hot", "First.")

	first := Discover(roots)
	if skill := findSkill(t, first, "hot"); skill.Description != "First." {
		t.Fatalf("description = %q, want First.", skill.Description)
	}

	writeValidSkill(t, skillDir, "hot", "Second.")
	second := Discover(roots)
	if skill := findSkill(t, second, "hot"); skill.Description != "Second." {
		t.Fatalf("description = %q, want the re-read value", skill.Description)
	}

	if err := os.RemoveAll(skillDir); err != nil {
		t.Fatal(err)
	}
	requireNames(t, Discover(roots))
}

func TestDiscoverCarriesInvocationFlagsFromMetadata(t *testing.T) {
	fixture := newLayoutFixture(t)
	cwd := filepath.Join(fixture.gitRoot, "pkg")
	root := fixture.skillsRoot(cwd)
	writeSkill(t, filepath.Join(root, "implicit"), "name: implicit", "description: Model invocable.", "disable-model-invocation: false")
	writeSkill(t, filepath.Join(root, "manual"), "name: manual", "description: Explicit only.", "disable-model-invocation: true")
	writeSkill(t, filepath.Join(root, "internal"), "name: internal", "description: Model only.", "user-invocable: false")
	writeValidSkill(t, filepath.Join(root, "sidecar"), "sidecar", "Policy sidecar.")
	writeFile(t, filepath.Join(root, "sidecar", "agents", "openai.yaml"), "policy:\n  allow_implicit_invocation: false\n")

	catalog := Discover(StandardRoots(cwd, fixture.home))

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "implicit", "internal", "manual", "sidecar")
	if skill := findSkill(t, catalog, "implicit"); !skill.Automatic || !skill.UserInvocable {
		t.Fatalf("skill = %+v, want both flags on", skill)
	}
	if skill := findSkill(t, catalog, "manual"); skill.Automatic || !skill.UserInvocable {
		t.Fatalf("skill = %+v, want explicit-only", skill)
	}
	if skill := findSkill(t, catalog, "internal"); !skill.Automatic || skill.UserInvocable {
		t.Fatalf("skill = %+v, want model-only", skill)
	}
	if skill := findSkill(t, catalog, "sidecar"); skill.Automatic {
		t.Fatalf("skill = %+v, want the openai policy to disable implicit invocation", skill)
	}
}

func TestDiscoverHandlesPathsWithSpaces(t *testing.T) {
	fixture := newLayoutFixture(t)
	cwd := filepath.Join(fixture.gitRoot, "my project", "the app")
	writeValidSkill(t, filepath.Join(fixture.skillsRoot(cwd), "my skill"), "my-skill", "Spaced path.")

	catalog := Discover(StandardRoots(cwd, fixture.home))

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog, "my-skill")
	if skill := findSkill(t, catalog, "my-skill"); skill.Path != filepath.Join(fixture.skillsRoot(cwd), "my skill", "SKILL.md") {
		t.Fatalf("path = %q, want the spaced path", skill.Path)
	}
}

func TestDiscoverIgnoresRootThatIsAFile(t *testing.T) {
	dir := t.TempDir()
	root := writeFile(t, filepath.Join(dir, "agents-file"), "not a directory\n")

	catalog := Discover([]Root{projectRoot(root)})

	requireNames(t, catalog)
	requireDiagnosticCount(t, catalog, 1)
	requireDiagnosticContaining(t, catalog, root)
}

func TestDiscoverSkipsSkillDirectoryWithoutSKILLMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "not-a-skill", "SKILL.md.bak"), "backup\n")

	catalog := Discover([]Root{projectRoot(dir)})

	requireNoDiagnostics(t, catalog)
	requireNames(t, catalog)
}
