package localskills

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestIdentityNamespacesPluginSkills(t *testing.T) {
	cases := []struct {
		name  string
		skill Skill
		want  string
	}{
		{name: "unqualified", skill: Skill{Name: "review"}, want: "review"},
		{name: "plugin scoped", skill: Skill{Name: "review", PluginName: "tutti"}, want: "tutti:review"},
		{name: "trimmed", skill: Skill{Name: " review ", PluginName: " tutti "}, want: "tutti:review"},
		{name: "empty plugin", skill: Skill{Name: "review", PluginName: "   "}, want: "review"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Identity(testCase.skill); got != testCase.want {
				t.Fatalf("Identity(%+v) = %q, want %q", testCase.skill, got, testCase.want)
			}
		})
	}
}

func TestStandardRootsStopsAtNearestGitRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	nested := filepath.Join(root, "pkg", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()

	want := []Root{
		projectRoot(filepath.Join(nested, ".agents", "skills")),
		projectRoot(filepath.Join(nested, ".agents")),
		projectRoot(filepath.Join(root, "pkg", ".agents", "skills")),
		projectRoot(filepath.Join(root, "pkg", ".agents")),
		projectRoot(filepath.Join(root, ".agents", "skills")),
		projectRoot(filepath.Join(root, ".agents")),
		personalRoot(filepath.Join(home, ".agents", "skills")),
		personalRoot(filepath.Join(home, ".agents")),
	}
	if got := StandardRoots(nested, home); !reflect.DeepEqual(got, want) {
		t.Fatalf("StandardRoots = %#v, want %#v", got, want)
	}
}

func TestStandardRootsTreatsGitWorktreeFileAsGitRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git"), "gitdir: "+filepath.Join(t.TempDir(), ".git", "worktrees", "wt")+"\n")
	nested := filepath.Join(root, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	roots := StandardRoots(nested, "")
	if len(roots) != 4 {
		t.Fatalf("StandardRoots = %#v, want the worktree root to end the walk", roots)
	}
	if roots[2].Path != filepath.Join(root, ".agents", "skills") || roots[3].Path != filepath.Join(root, ".agents") {
		t.Fatalf("StandardRoots = %#v, want the worktree root last", roots)
	}
}

func TestStandardRootsWalksToFilesystemRootWithoutGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filesystem root layout differs on windows")
	}
	nested := filepath.Join(t.TempDir(), "one", "two")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	roots := StandardRoots(nested, "")
	if len(roots) == 0 {
		t.Fatal("StandardRoots returned nothing")
	}
	if roots[0].Path != filepath.Join(nested, ".agents", "skills") {
		t.Fatalf("first root = %q, want the skills layout under cwd", roots[0].Path)
	}
	last := roots[len(roots)-2:]
	want := []Root{projectRoot(filepath.Join("/", ".agents", "skills")), projectRoot(filepath.Join("/", ".agents"))}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("last roots = %#v, want %#v", last, want)
	}
	for index := 1; index < len(roots); index++ {
		if roots[index].SourceKind != SourceKindProject {
			t.Fatalf("root %d = %#v, want project source kind", index, roots[index])
		}
	}
}

func TestStandardRootsOrdersSkillsLayoutBeforeFlatLayout(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	cwd := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	roots := StandardRoots(cwd, "")
	for index := 0; index+1 < len(roots); index += 2 {
		if filepath.Base(roots[index].Path) != "skills" {
			t.Fatalf("roots = %#v, want the skills layout before the flat layout", roots)
		}
		if roots[index+1].Path != filepath.Dir(roots[index].Path) {
			t.Fatalf("roots = %#v, want each skills layout followed by its own .agents", roots)
		}
	}
}

func TestStandardRootsKeepsPersonalRootsOutsideProjectWalk(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	cwd := filepath.Join(root, "app")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()

	roots := StandardRoots(cwd, home)
	last := roots[len(roots)-2:]
	want := []Root{
		personalRoot(filepath.Join(home, ".agents", "skills")),
		personalRoot(filepath.Join(home, ".agents")),
	}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("last roots = %#v, want %#v", last, want)
	}
}

func TestStandardRootsDropsPersonalRootsAlreadyWalked(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	cwd := filepath.Join(root, "app")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	// The home directory is the git root, so its two layouts were already
	// returned as project roots and must not come back a second time.
	roots := StandardRoots(cwd, root)
	if len(roots) != 4 {
		t.Fatalf("roots = %#v, want only the two walked ancestors", roots)
	}
	for _, root := range roots {
		if root.SourceKind != SourceKindProject {
			t.Fatalf("root %#v, want no duplicate personal roots", root)
		}
	}
}

func TestStandardRootsHandlesEmptyInputs(t *testing.T) {
	home := t.TempDir()
	personal := StandardRoots("", home)
	want := []Root{
		personalRoot(filepath.Join(home, ".agents", "skills")),
		personalRoot(filepath.Join(home, ".agents")),
	}
	if !reflect.DeepEqual(personal, want) {
		t.Fatalf("StandardRoots(\"\", home) = %#v, want %#v", personal, want)
	}
	for _, root := range StandardRoots(home, "") {
		if root.SourceKind != SourceKindProject {
			t.Fatalf("root %#v, want project roots when home is empty", root)
		}
	}
	if roots := StandardRoots("", ""); len(roots) != 0 {
		t.Fatalf("StandardRoots(\"\", \"\") = %#v, want no roots", roots)
	}
}

func TestStandardRootsUsesDirectoryOfFileCwd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	file := writeFile(t, filepath.Join(root, "app", "main.go"), "package main\n")

	roots := StandardRoots(file, "")
	if roots[0].Path != filepath.Join(root, "app", ".agents", "skills") {
		t.Fatalf("first root = %q, want the file's directory", roots[0].Path)
	}
	if roots[len(roots)-1].Path != filepath.Join(root, ".agents") {
		t.Fatalf("roots = %#v, want the walk to stop at the git root", roots)
	}
}
