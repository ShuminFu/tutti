package agent

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep/localskills"
)

const (
	cursorSkillMaxFiles = 2048
	cursorSkillMaxBytes = 50 << 20
)

// CursorSkillImportEntry describes one immediate child of .cursor/skills.
// Status is ready, exists, invalid, or unsafe. Existing skills are never replaced.
type CursorSkillImportEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Folder string `json:"-"`
}

type CursorSkillImportPreview struct {
	Destination string                   `json:"destination"`
	Skills      []CursorSkillImportEntry `json:"skills"`
}

type CursorSkillImportResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// PreviewCursorSkills accepts a selected .cursor directory or its skills child.
// The destination is the sibling .agents/skills directory in the same project
// (or home directory). Neither preview nor import alters the Cursor source.
func PreviewCursorSkills(selected string) (CursorSkillImportPreview, error) {
	source, destination, err := cursorSkillRoots(selected)
	if err != nil {
		return CursorSkillImportPreview{}, err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return CursorSkillImportPreview{}, err
	}
	preview := CursorSkillImportPreview{Destination: destination, Skills: []CursorSkillImportEntry{}}
	seen := map[string]bool{}
	seenSource := map[string]bool{}
	home, _ := os.UserHomeDir()
	existing := localskills.Discover(localskills.StandardRoots(filepath.Dir(filepath.Dir(destination)), home))
	for _, skill := range existing.Skills {
		seen[skill.Name] = true
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(source, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			preview.Skills = append(preview.Skills, CursorSkillImportEntry{Name: entry.Name(), Status: "unsafe", Reason: err.Error()})
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			preview.Skills = append(preview.Skills, CursorSkillImportEntry{Name: entry.Name(), Status: "unsafe", Reason: "skill directory must not be a symlink"})
			continue
		}
		if !info.IsDir() {
			continue
		}
		metadata := filepath.Join(path, "SKILL.md")
		metadataInfo, err := os.Lstat(metadata)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !metadataInfo.Mode().IsRegular() {
			preview.Skills = append(preview.Skills, CursorSkillImportEntry{Name: entry.Name(), Status: "invalid", Reason: "SKILL.md must be a regular file"})
			continue
		}
		if err := inspectCursorSkillTree(path); err != nil {
			preview.Skills = append(preview.Skills, CursorSkillImportEntry{Name: entry.Name(), Status: "unsafe", Reason: err.Error()})
			continue
		}
		skill, err := localskills.Read(metadata)
		if err != nil {
			preview.Skills = append(preview.Skills, CursorSkillImportEntry{Name: entry.Name(), Status: "invalid", Reason: err.Error()})
			continue
		}
		item := CursorSkillImportEntry{Name: skill.Name, Status: "ready", Folder: entry.Name()}
		if seenSource[skill.Name] {
			item.Status, item.Reason = "invalid", "duplicate Cursor skill name"
		} else if seen[skill.Name] || pathExists(filepath.Join(destination, skill.Name)) || pathExists(filepath.Join(filepath.Dir(destination), skill.Name)) {
			item.Status, item.Reason = "exists", "a skill with this name already exists"
		}
		seenSource[skill.Name] = true
		seen[skill.Name] = true
		preview.Skills = append(preview.Skills, item)
	}
	sort.Slice(preview.Skills, func(i, j int) bool { return preview.Skills[i].Name < preview.Skills[j].Name })
	return preview, nil
}

// ImportCursorSkills rechecks the selected source and conflicts at commit time.
// A hidden staging directory keeps incomplete copies out of skill discovery.
// The completed directory is installed with a no-replace rooted rename.
func ImportCursorSkills(selected string, names []string) ([]CursorSkillImportResult, error) {
	if len(names) == 0 {
		return nil, errors.New("select at least one skill")
	}
	_, destination, err := cursorSkillRoots(selected)
	if err != nil {
		return nil, err
	}
	preview, err := PreviewCursorSkills(selected)
	if err != nil {
		return nil, err
	}
	byName := map[string]CursorSkillImportEntry{}
	for _, item := range preview.Skills {
		if _, exists := byName[item.Name]; !exists {
			byName[item.Name] = item
		}
	}
	results := make([]CursorSkillImportResult, 0, len(names))
	projectRoot, err := os.OpenRoot(filepath.Dir(filepath.Dir(destination)))
	if err != nil {
		return nil, err
	}
	defer projectRoot.Close()
	cursorRoot, err := openRealChildRoot(projectRoot, ".cursor")
	if err != nil {
		return nil, err
	}
	defer cursorRoot.Close()
	sourceRoot, err := openRealChildRoot(cursorRoot, "skills")
	if err != nil {
		return nil, err
	}
	defer sourceRoot.Close()
	selectedNames := map[string]bool{}
	for _, name := range names {
		if selectedNames[name] {
			continue
		}
		selectedNames[name] = true
		item, ok := byName[name]
		if !ok {
			results = append(results, CursorSkillImportResult{Name: name, Status: "invalid", Reason: "skill not found"})
			continue
		}
		if item.Status != "ready" {
			results = append(results, CursorSkillImportResult{Name: name, Status: item.Status, Reason: item.Reason})
			continue
		}
		if err := ensureRootDirectory(projectRoot, ".agents"); err != nil {
			return results, err
		}
		agentsRoot, err := openRealChildRoot(projectRoot, ".agents")
		if err != nil {
			return results, err
		}
		if err := ensureRootDirectory(agentsRoot, "skills"); err != nil {
			agentsRoot.Close()
			return results, err
		}
		destinationRoot, err := openRealChildRoot(agentsRoot, "skills")
		agentsRoot.Close()
		if err != nil {
			return results, err
		}
		// Frontmatter names need not match source folder names.
		sourceSkill, err := openRealChildRoot(sourceRoot, item.Folder)
		if err == nil {
			err = copyCursorSkillAtomically(sourceSkill, destinationRoot, name)
			sourceSkill.Close()
		}
		destinationRoot.Close()
		if err != nil {
			results = append(results, CursorSkillImportResult{Name: name, Status: "failed", Reason: err.Error()})
			continue
		}
		results = append(results, CursorSkillImportResult{Name: name, Status: "imported"})
	}
	return results, nil
}

func cursorSkillRoots(selected string) (string, string, error) {
	selected = strings.TrimSpace(selected)
	if !filepath.IsAbs(selected) {
		return "", "", errors.New("select an absolute .cursor/skills directory")
	}
	selected = filepath.Clean(selected)
	if filepath.Base(selected) == ".cursor" {
		selected = filepath.Join(selected, "skills")
	}
	if filepath.Base(selected) != "skills" || filepath.Base(filepath.Dir(selected)) != ".cursor" {
		return "", "", errors.New("select a .cursor directory or its skills child")
	}
	projectRoot, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(selected)))
	if err != nil {
		return "", "", err
	}
	source := filepath.Join(projectRoot, ".cursor", "skills")
	for _, path := range []string{filepath.Dir(source), source} {
		info, err := os.Lstat(path)
		if err != nil {
			return "", "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("%s must be a real directory", path)
		}
	}
	destination := filepath.Join(projectRoot, ".agents", "skills")
	for _, path := range []string{filepath.Dir(destination), destination} {
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("%s must be a real directory", path)
		}
	}
	return source, destination, nil
}

func inspectCursorSkillTree(root string) error {
	files, bytes := 0, int64(0)
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", path)
		}
		files++
		if files > cursorSkillMaxFiles {
			return errors.New("skill exceeds import file limit")
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
			if bytes > cursorSkillMaxBytes {
				return errors.New("skill exceeds import size limit")
			}
		}
		return nil
	})
}

func copyCursorSkillAtomically(source, destination *os.Root, name string) error {
	if err := inspectRootedCursorSkillTree(source); err != nil {
		return err
	}
	stageName := ".cursor-import-" + uuid.NewString()
	if err := destination.Mkdir(stageName, 0o700); err != nil {
		return err
	}
	// Keep the stage hidden until its files have been copied and validated.
	target, err := openRealChildRoot(destination, stageName)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			// Remove contents through the held root. If the stage pathname was
			// swapped, never recursively delete its replacement.
			cleanupCursorSkillStage(target, destination, stageName)
		}
		target.Close()
	}()
	if err := copyCursorSkillTree(source, target, true); err != nil {
		return err
	}
	// Validate the exact copied metadata before making this directory visible.
	if err := copyCursorSkillFile(source, target, "SKILL.md", "SKILL.md"); err != nil {
		return err
	}
	staged := filepath.Join(target.Name(), "SKILL.md")
	imported, err := localskills.Read(staged)
	if err != nil || imported.Name != name {
		return errors.New("SKILL.md changed during import")
	}
	if err := renameRootedCursorEntry(destination, stageName, name); err != nil {
		return fmt.Errorf("import could not finish: %w", err)
	}
	published = true
	return nil
}

func cleanupCursorSkillStage(stage, destination *os.Root, stageName string) {
	var removeContents func(*os.Root) error
	removeContents = func(current *os.Root) error {
		dir, err := current.Open(".")
		if err != nil {
			return err
		}
		entries, err := dir.ReadDir(-1)
		dir.Close()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			info, err := current.Lstat(entry.Name())
			if err != nil {
				return err
			}
			if info.IsDir() {
				child, err := openRealChildRoot(current, entry.Name())
				if err != nil {
					return err
				}
				err = removeContents(child)
				child.Close()
				if err != nil {
					return err
				}
			}
			if err := current.Remove(entry.Name()); err != nil {
				return err
			}
		}
		return nil
	}
	if err := removeContents(stage); err != nil {
		return
	}
	original, err := stage.Stat(".")
	if err != nil {
		return
	}
	current, err := destination.Lstat(stageName)
	if err == nil && os.SameFile(original, current) {
		// Remove only the now-empty stage. A swapped-in nonempty directory
		// cannot be removed by Root.Remove.
		_ = destination.Remove(stageName)
	}
}

func inspectRootedCursorSkillTree(root *os.Root) error {
	files, bytes := 0, int64(0)
	var walk func(*os.Root) error
	walk = func(current *os.Root) error {
		dir, err := current.Open(".")
		if err != nil {
			return err
		}
		entries, err := dir.ReadDir(-1)
		dir.Close()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			info, err := current.Lstat(entry.Name())
			if err != nil {
				return err
			}
			files++
			if files > cursorSkillMaxFiles {
				return errors.New("skill exceeds import file limit")
			}
			if info.IsDir() {
				child, err := openRealChildRoot(current, entry.Name())
				if err != nil {
					return err
				}
				err = walk(child)
				child.Close()
				if err != nil {
					return err
				}
				continue
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported file type: %s", entry.Name())
			}
			bytes += info.Size()
			if bytes > cursorSkillMaxBytes {
				return errors.New("skill exceeds import size limit")
			}
		}
		return nil
	}
	return walk(root)
}

func copyCursorSkillTree(source, destination *os.Root, topLevel bool) error {
	dir, err := source.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		info, err := source.Lstat(name)
		if err != nil {
			return err
		}
		if info.IsDir() {
			childSource, err := openRealChildRoot(source, name)
			if err != nil {
				return err
			}
			if err = destination.Mkdir(name, info.Mode().Perm()|0o700); err == nil {
				var childTarget *os.Root
				childTarget, err = openRealChildRoot(destination, name)
				if err == nil {
					err = copyCursorSkillTree(childSource, childTarget, false)
					childTarget.Close()
				}
			}
			childSource.Close()
			if err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", name)
		}
		if topLevel && name == "SKILL.md" {
			continue
		}
		if err := copyCursorSkillFile(source, destination, name, name); err != nil {
			return err
		}
	}
	return nil
}

func copyCursorSkillFile(source, destination *os.Root, sourceName, targetName string) error {
	before, err := source.Lstat(sourceName)
	if err != nil || !before.Mode().IsRegular() {
		return errors.New("source file changed during import")
	}
	input, err := source.Open(sourceName)
	if err != nil {
		return err
	}
	opened, err := input.Stat()
	if err != nil || !os.SameFile(before, opened) {
		input.Close()
		return errors.New("source file changed during import")
	}
	output, err := destination.OpenFile(targetName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, before.Mode().Perm())
	if err != nil {
		input.Close()
		return err
	}
	_, copyErr := io.CopyN(output, input, before.Size())
	inputErr := input.Close()
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if inputErr != nil {
		return inputErr
	}
	return closeErr
}

func ensureRootDirectory(parent *os.Root, name string) error {
	info, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return parent.Mkdir(name, 0o755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must be a real directory", name)
	}
	return nil
}

func openRealChildRoot(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s must be a real directory", name)
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err == nil && os.SameFile(before, after) {
		var current os.FileInfo
		current, err = parent.Lstat(name)
		if err == nil && current.IsDir() && os.SameFile(before, current) {
			return child, nil
		}
	}
	child.Close()
	return nil, fmt.Errorf("%s changed during import", name)
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}
