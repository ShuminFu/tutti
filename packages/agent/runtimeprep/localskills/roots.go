package localskills

import (
	"os"
	"path/filepath"
	"strings"
)

// Source kinds reported by StandardRoots for the roots it derives.
const (
	SourceKindProject  = "project"
	SourceKindPersonal = "personal"
)

const (
	agentsDirName = ".agents"
	skillsDirName = "skills"
	gitMarkerName = ".git"
	skillFileName = "SKILL.md"
)

// Root is one directory whose immediate subdirectories may each be a skill
// directory, that is a <root>/<name>/SKILL.md container. SourceKind and
// PluginName are copied onto every skill discovered under the root, so a host
// that adds its own plugin roots can namespace them.
type Root struct {
	Path       string
	SourceKind string
	PluginName string
}

// StandardRoots returns the candidate skill roots for a session started in cwd
// for a user whose home directory is home.
//
// Project roots walk from cwd up to the nearest git root and stop there: a
// directory holding a .git directory (a normal checkout) or a .git file (a
// linked worktree) both end the walk, and the git root itself contributes
// roots. Outside a git repository the walk continues to the filesystem root.
// Each ancestor contributes its ".agents/skills" layout before its bare
// ".agents" layout, so a nearer ancestor wins over a farther one and, within
// one ancestor, the skills layout wins. Personal roots follow the project
// roots, in the same two-layout order, and are always SourceKindPersonal.
//
// Roots are returned whether or not they exist on disk; Discover ignores a
// missing root silently. A root path that is already present keeps its first,
// project-scoped occurrence.
func StandardRoots(cwd string, home string) []Root {
	roots := make([]Root, 0, 8)
	for _, ancestor := range ancestorDirs(cwd) {
		roots = append(roots, Root{
			Path:       filepath.Join(ancestor, agentsDirName, skillsDirName),
			SourceKind: SourceKindProject,
		})
		roots = append(roots, Root{
			Path:       filepath.Join(ancestor, agentsDirName),
			SourceKind: SourceKindProject,
		})
	}
	for _, personal := range personalDirs(home) {
		roots = append(roots, Root{
			Path:       filepath.Join(personal, agentsDirName, skillsDirName),
			SourceKind: SourceKindPersonal,
		})
		roots = append(roots, Root{
			Path:       filepath.Join(personal, agentsDirName),
			SourceKind: SourceKindPersonal,
		})
	}
	return dedupeRoots(roots)
}

// ancestorDirs lists cwd and its parents, nearest first, stopping at the
// nearest git root or at the filesystem root when there is none.
func ancestorDirs(cwd string) []string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil
	}
	current, err := filepath.Abs(cwd)
	if err != nil {
		current = filepath.Clean(cwd)
	}
	current = filepath.Clean(current)
	if info, err := os.Stat(current); err == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	dirs := make([]string, 0, 8)
	for {
		dirs = append(dirs, current)
		if isGitRoot(current) {
			return dirs
		}
		parent := filepath.Dir(current)
		if parent == current {
			return dirs
		}
		current = parent
	}
}

// isGitRoot reports whether dir carries a git marker. Both a .git directory
// and a .git file (what git writes at the root of a linked worktree) count.
func isGitRoot(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, gitMarkerName))
	return err == nil
}

// personalDirs lists the home directory that contributes personal roots.
func personalDirs(home string) []string {
	home = strings.TrimSpace(home)
	if home == "" {
		return nil
	}
	if absolute, err := filepath.Abs(home); err == nil {
		home = absolute
	}
	return []string{filepath.Clean(home)}
}

func dedupeRoots(roots []Root) []Root {
	seen := make(map[string]struct{}, len(roots))
	unique := make([]Root, 0, len(roots))
	for _, root := range roots {
		path := filepath.Clean(strings.TrimSpace(root.Path))
		if path == "" || path == "." {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		root.Path = path
		unique = append(unique, root)
	}
	return unique
}
