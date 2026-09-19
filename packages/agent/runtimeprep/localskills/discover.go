package localskills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Catalog is the result of one discovery pass over a list of roots.
type Catalog struct {
	Skills      []Skill
	Diagnostics []string
}

// Discover reads every root in order and returns the skills it found.
//
// Only the immediate subdirectories of a root are considered, and only
// non-hidden ones, so a nested or unrelated .agents directory below a root is
// never scanned. A root that does not exist is silent; any other root-level
// read failure, such as a permission error, becomes a diagnostic. Inside a
// root, a subdirectory without a SKILL.md is silent, while a SKILL.md whose
// metadata is missing or invalid gets a diagnostic naming that file and the
// reason.
//
// A skill directory may be a symlink. Two entries resolving to the same
// physical SKILL.md are one skill: the first is kept and the duplicate is
// silent, which keeps a symlinked layout from looking like a conflict.
//
// Two different files claiming the same identity (the name, or pluginName:name
// under a plugin-scoped root) are a shadowing conflict: the first root in the
// list wins and the diagnostic names both the winner and the loser. An
// unqualified skill never collides with a plugin-scoped skill of the same name.
//
// Skills come back in root order, and in directory order within a root, so the
// catalog is deterministic and its first entry for an identity is the winner.
func Discover(roots []Root) Catalog {
	catalog := Catalog{
		Skills:      []Skill{},
		Diagnostics: []string{},
	}
	seenPhysical := make(map[string]struct{})
	winners := make(map[string]string)
	for _, root := range roots {
		rootPath := strings.TrimSpace(root.Path)
		if rootPath == "" {
			continue
		}
		rootPath = filepath.Clean(rootPath)
		entries, err := os.ReadDir(rootPath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				catalog.Diagnostics = append(
					catalog.Diagnostics,
					fmt.Sprintf("skill root %s: %v", rootPath, err),
				)
			}
			continue
		}
		sort.SliceStable(entries, func(left int, right int) bool {
			return entries[left].Name() < entries[right].Name()
		})
		for _, entry := range entries {
			name := entry.Name()
			if name == "" || strings.HasPrefix(name, ".") {
				continue
			}
			dirPath := filepath.Join(rootPath, name)
			if info, err := os.Stat(dirPath); err != nil || !info.IsDir() {
				continue
			}
			skillPath := filepath.Join(dirPath, skillFileName)
			if info, err := os.Stat(skillPath); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					catalog.Diagnostics = append(catalog.Diagnostics, fmt.Sprintf("%s: %v", skillPath, err))
				}
				continue
			} else if info.IsDir() {
				catalog.Diagnostics = append(catalog.Diagnostics, fmt.Sprintf("%s: SKILL.md must be a regular file", skillPath))
				continue
			}
			physical := physicalPath(skillPath)
			if _, ok := seenPhysical[physical]; ok {
				continue
			}
			seenPhysical[physical] = struct{}{}
			skill, err := Read(skillPath)
			if err != nil {
				catalog.Diagnostics = append(catalog.Diagnostics, err.Error())
				continue
			}
			skill.SourceKind = root.SourceKind
			skill.PluginName = strings.TrimSpace(root.PluginName)
			identity := Identity(skill)
			if winner, ok := winners[identity]; ok {
				catalog.Diagnostics = append(catalog.Diagnostics, fmt.Sprintf(
					"skill %q at %s is shadowed by %s; the first root wins",
					identity,
					skill.Path,
					winner,
				))
				continue
			}
			winners[identity] = skill.Path
			catalog.Skills = append(catalog.Skills, skill)
		}
	}
	return catalog
}

// physicalPath resolves a SKILL.md to the file it really is, so a symlinked
// directory and its target are recognized as the same skill. An unresolvable
// path falls back to its cleaned form.
func physicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}
