package localskills

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Filter narrows catalog to an explicit selection.
//
// With no explicit selection (explicit false and nothing but blank entries in
// selected, or an empty selected) the catalog comes back unchanged: discovery
// is the default and filtering only happens when a caller asks for it.
//
// Otherwise a skill is kept when a selected entry names it as its bare name,
// its identity (pluginName:name for a plugin-scoped skill), "/name", "$name",
// or its absolute SKILL.md path. Matching is case-sensitive. A bare name never
// reaches into a plugin namespace: "review" selects the unqualified review,
// and the plugin-scoped one needs "plugin:review". Because a skill name cannot
// contain a colon, a namespaced entry is never ambiguous with a bare name.
//
// A skill whose UserInvocable is false is hidden from an explicit selection:
// it cannot be selected, and a selection that would have matched it produces a
// diagnostic instead of a silent miss. Automatic is not consulted, so a skill
// the model may not invoke implicitly is still usable when explicitly
// selected. Input diagnostics are always preserved.
func Filter(catalog Catalog, selected []string, explicit bool) Catalog {
	tokens := make([]string, 0, len(selected))
	for _, entry := range selected {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		tokens = append(tokens, entry)
	}
	if !explicit && len(tokens) == 0 {
		return catalog
	}

	diagnostics := make([]string, 0, len(catalog.Diagnostics))
	diagnostics = append(diagnostics, catalog.Diagnostics...)
	skills := make([]Skill, 0, len(catalog.Skills))
	hidden := make(map[string]struct{})
	for _, skill := range catalog.Skills {
		if !matchesAnyToken(tokens, skill) {
			continue
		}
		if !skill.UserInvocable {
			key := physicalPath(skill.Path)
			if _, ok := hidden[key]; !ok {
				hidden[key] = struct{}{}
				diagnostics = append(diagnostics, fmt.Sprintf(
					"skill %q at %s is not user-invocable and cannot be selected explicitly",
					Identity(skill),
					skill.Path,
				))
			}
			continue
		}
		skills = append(skills, skill)
	}
	return Catalog{Skills: skills, Diagnostics: diagnostics}
}

func matchesAnyToken(tokens []string, skill Skill) bool {
	for _, token := range tokens {
		if matchesToken(token, skill) {
			return true
		}
	}
	return false
}

func matchesToken(token string, skill Skill) bool {
	if filepath.IsAbs(token) && filepath.Clean(token) == filepath.Clean(skill.Path) {
		return true
	}
	// A leading "/" also marks the provider trigger form, which makes such an
	// entry look like an absolute path, so the path comparison above cannot be
	// the only reading of it. A stripped path never matches, because an
	// identity contains no separator.
	name := token
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "$") {
		name = name[1:]
	}
	if name == "" {
		return false
	}
	if name == Identity(skill) {
		return true
	}
	return strings.TrimSpace(skill.PluginName) == "" && name == strings.TrimSpace(skill.Name)
}
