package agentruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep/localskills"
)

// Local skills are a provider input projection. Canonical submission facts and
// display text remain untouched, including on retries, guidance and history replay.
func projectLocalSkillPrompt(session Session, content []PromptContentBlock, refresh bool) ([]PromptContentBlock, error) {
	selection := runtimeprep.LocalSkillSelection{}
	if path, found := lastEnvironmentValue(session.Env, runtimeprep.LocalSkillsSelectionEnv); found {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("read local skill selection: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() > 4<<20 {
			return nil, fmt.Errorf("invalid local skill selection file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &selection); err != nil {
			return nil, fmt.Errorf("invalid local skill selection: %w", err)
		}
	}
	catalog := runtimeprep.LocalSkillCatalog(session.CWD, selection)
	if refresh {
		if path, found := lastEnvironmentValue(session.Env, runtimeprep.LocalSkillsFileEnv); found && path != "" {
			if err := runtimeprep.WriteLocalSkillCatalog(path, session.CWD, selection); err != nil {
				return nil, fmt.Errorf("refresh local skills: %w", err)
			}
		}
	}
	byName := make(map[string]localskills.Skill, len(catalog.Skills))
	for _, skill := range catalog.Skills {
		byName[localskills.Identity(skill)] = skill
	}
	result := make([]PromptContentBlock, 0, len(content))
	used := map[string]bool{}
	// A structured selection takes precedence over the textual label. Validate
	// against the current catalog before consuming either representation.
	for _, block := range content {
		if block.Type != "skill" {
			continue
		}
		skill, found := byName[block.Name]
		if found && skill.Path == block.Path {
			if !skill.UserInvocable {
				return nil, fmt.Errorf("skill %q is not user invocable", block.Name)
			}
			used[block.Name] = true
		} else if found || block.Path == runtimeprep.SkillCreatorPath || standardSkillPath(block.Path) {
			return nil, fmt.Errorf("local skill %q is no longer available at %q; refresh the skill selection", block.Name, block.Path)
		}
	}
	for _, block := range content {
		if block.Type == "skill" && used[block.Name] && byName[block.Name].Path == block.Path {
			continue
		}
		if block.Type == "text" {
			trimmed := strings.TrimLeftFunc(block.Text, unicode.IsSpace)
			token, rest, _ := strings.Cut(trimmed, " ")
			// Fields handles tabs/newlines without interpreting inline prose, code,
			// paths, or provider-native commands as local skill invocations.
			if fields := strings.Fields(trimmed); len(fields) > 0 {
				token = fields[0]
				rest = strings.TrimSpace(strings.TrimPrefix(trimmed, token))
			}
			if strings.HasPrefix(token, "/") || strings.HasPrefix(token, "$") {
				name := token[1:]
				if skill, ok := byName[name]; ok {
					if !skill.UserInvocable {
						return nil, fmt.Errorf("skill %q is not user invocable", name)
					}
					used[name] = true
					block.Text = rest
				}
			}
		}
		if block.Type != "text" || strings.TrimSpace(block.Text) != "" {
			result = append(result, block)
		}
	}
	// Catalog order is stable; map iteration must not change provider prompts.
	for _, skill := range catalog.Skills {
		if used[localskills.Identity(skill)] {
			result = append(result, PromptContentBlock{Type: "text", Text: runtimeprep.LocalSkillInstructions(skill)})
		}
	}
	return result, nil
}

func standardSkillPath(path string) bool {
	for _, segment := range strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".agents" {
			return true
		}
	}
	return false
}
