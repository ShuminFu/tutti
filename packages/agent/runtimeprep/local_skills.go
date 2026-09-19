package runtimeprep

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep/localskills"
)

const LocalSkillsFileEnv = "TUTTI_LOCAL_SKILLS_FILE"
const LocalSkillsSelectionEnv = "TUTTI_LOCAL_SKILLS_SELECTION_FILE"
const SkillCreatorPath = "builtin:skill-creator"

//go:embed skill_templates/skill-creator.md
var localSkillTemplates embed.FS

type LocalSkillSelection struct {
	Selected []string            `json:"selected"`
	Explicit bool                `json:"explicit"`
	Native   []localskills.Skill `json:"native,omitempty"`
}

type LocalSkillCatalogEntry struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Path          string `json:"path"`
	Automatic     bool   `json:"automatic"`
	UserInvocable bool   `json:"userInvocable"`
	Content       string `json:"content,omitempty"`
}

func LocalSkillCatalog(cwd string, selection LocalSkillSelection) localskills.Catalog {
	home, _ := os.UserHomeDir()
	catalog := localskills.Discover(localskills.StandardRoots(cwd, home))
	seen := map[string]bool{}
	for _, skill := range catalog.Skills {
		seen[localskills.Identity(skill)] = true
	}
	for _, source := range selection.Native {
		skill, err := localskills.Read(source.Path)
		if err != nil {
			catalog.Diagnostics = append(catalog.Diagnostics, err.Error())
			continue
		}
		if skill.Name != source.Name {
			continue
		}
		skill.PluginName = source.PluginName
		skill.SourceKind = source.SourceKind
		key := localskills.Identity(skill)
		if seen[key] {
			continue
		}
		seen[key] = true
		catalog.Skills = append(catalog.Skills, skill)
	}
	filtered := localskills.Filter(catalog, selection.Selected, selection.Explicit)
	if selection.Explicit || len(selection.Selected) > 0 {
		kept := map[string]bool{}
		for _, skill := range filtered.Skills {
			kept[localskills.Identity(skill)] = true
		}
		for _, skill := range catalog.Skills {
			if skill.SourceKind == "system" && !kept[localskills.Identity(skill)] {
				filtered.Skills = append(filtered.Skills, skill)
			}
		}
	}
	catalog = filtered
	for _, skill := range catalog.Skills {
		if localskills.Identity(skill) == "skill-creator" {
			return catalog
		}
	}
	// Built-in authoring remains available like the other trusted core skills.
	catalog.Skills = append(catalog.Skills, localskills.Skill{
		Name: "skill-creator", Description: "Create or update reusable skills in the project's .agents/skills directory", Path: SkillCreatorPath,
		SourceKind: "system", Automatic: true, UserInvocable: true,
	})
	return catalog
}

func LocalSkillInstructions(skill localskills.Skill) string {
	if skill.Path == SkillCreatorPath {
		body, _ := localSkillTemplates.ReadFile("skill_templates/skill-creator.md")
		return string(body)
	}
	return fmt.Sprintf("Use the %q skill. Read its SKILL.md at %q before proceeding. Resolve all relative scripts, references and assets against %q. Follow its instructions within the user's authorized scope.", localskills.Identity(skill), skill.Path, filepath.Dir(skill.Path))
}

func localSkillPolicy(input PrepareInput) string {
	catalog := LocalSkillCatalog(input.Cwd, LocalSkillSelection{Selected: input.AgentSkills, Explicit: input.AgentCapabilitiesExplicit, Native: input.NativeSkillReferences})
	var out strings.Builder
	out.WriteString("\n\n## Local skills\nSkills use .agents/skills/<name>/SKILL.md (also accepts .agents/<name>/SKILL.md). The nearest project wins, then ancestors, then the user's global .agents. Standard skills take precedence over native and built-in names. Keep native provider skills available.\nRead the indicated source before using a skill; resolve resources relative to its source directory. Forward the exact skill source and these rules when delegating to subagents; child agents use the same catalog, with their own explicit capability restrictions preserved. Do not infer permission to perform side effects from a skill.\n")
	for _, skill := range catalog.Skills {
		if !skill.Automatic {
			continue
		}
		if skill.Path == SkillCreatorPath {
			out.WriteString("\n" + LocalSkillInstructions(skill) + "\n")
		} else {
			fmt.Fprintf(&out, "- %s: %s (source: %q)\n", localskills.Identity(skill), strings.Join(strings.Fields(skill.Description), " "), skill.Path)
		}
	}
	for _, diagnostic := range catalog.Diagnostics {
		fmt.Fprintf(&out, "Skill discovery diagnostic: %s\n", diagnostic)
	}
	return out.String()
}

func WriteLocalSkillCatalog(path, cwd string, selection LocalSkillSelection) error {
	catalog := LocalSkillCatalog(cwd, selection)
	entries := make([]LocalSkillCatalogEntry, 0, len(catalog.Skills))
	for _, skill := range catalog.Skills {
		entry := LocalSkillCatalogEntry{Name: localskills.Identity(skill), Description: skill.Description, Path: skill.Path, Automatic: skill.Automatic, UserInvocable: skill.UserInvocable}
		if skill.Path == SkillCreatorPath {
			entry.Content = LocalSkillInstructions(skill)
		}
		entries = append(entries, entry)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	// This file is session-owned. A rename prevents a native child from observing
	// a partially refreshed catalog while a turn is being dispatched.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".local-skills-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func prepareLocalSkillCatalog(input PrepareInput, root string) ([]string, error) {
	selection := LocalSkillSelection{Selected: input.AgentSkills, Explicit: input.AgentCapabilitiesExplicit, Native: append([]localskills.Skill(nil), input.NativeSkillReferences...)}
	// Built-ins are the documented exception to user-owned standard roots. Keep
	// their provider-neutral source available to registries and native children.
	builtinRoot := filepath.Join(root, "builtin-skills")
	if err := os.RemoveAll(builtinRoot); err != nil {
		return nil, err
	}
	if _, err := installProviderNativeSkills(builtinRoot, input); err != nil {
		return nil, err
	}
	builtins := localskills.Discover([]localskills.Root{{Path: builtinRoot, SourceKind: "system"}})
	selection.Native = append(selection.Native, builtins.Skills...)
	path := filepath.Join(root, "local-skills.json")
	if err := WriteLocalSkillCatalog(path, input.Cwd, selection); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(selection)
	if err != nil {
		return nil, err
	}
	selectionPath := filepath.Join(root, "local-skills-selection.json")
	if err := os.WriteFile(selectionPath, encoded, 0o600); err != nil {
		return nil, err
	}
	return []string{LocalSkillsFileEnv + "=" + path, LocalSkillsSelectionEnv + "=" + selectionPath}, nil
}

func installLocalClaudeSkillReferences(root string, input PrepareInput) error {
	catalog := LocalSkillCatalog(input.Cwd, LocalSkillSelection{Selected: input.AgentSkills, Explicit: input.AgentCapabilitiesExplicit})
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		target := filepath.Join(root, entry.Name(), "SKILL.md")
		body, err := os.ReadFile(target)
		if err == nil && strings.Contains(string(body), "<!-- tutti-local-skill-reference -->") {
			if err := os.Remove(target); err != nil {
				return err
			}
			_ = os.Remove(filepath.Dir(target))
		}
	}
	for _, skill := range catalog.Skills {
		target := filepath.Join(root, skill.Name, "SKILL.md")
		// Core transport skills retain their internal native names. Explicit
		// unqualified invocation still resolves the standard catalog first.
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		content := fmt.Sprintf("---\nname: %q\ndescription: %q\ndisable-model-invocation: %t\nuser-invocable: %t\n---\n\n<!-- tutti-local-skill-reference -->\n%s\n", skill.Name, skill.Description, !skill.Automatic, skill.UserInvocable, LocalSkillInstructions(skill))
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}
