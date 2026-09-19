package agent

import (
	"github.com/tutti-os/tutti/packages/agent/runtimeprep/localskills"
	"strings"
)

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func filterWorkspaceAgentComposerSkills(options []ComposerSkillOption, selected []string, capabilitiesExplicit bool) []ComposerSkillOption {
	selected = normalizedSnapshotStrings(selected)
	if !capabilitiesExplicit && len(selected) == 0 {
		return options
	}
	result := make([]ComposerSkillOption, 0, len(options))
	for _, option := range options {
		// Daemon/system injected entries are part of the trusted runtime profile
		// and cannot be removed by a user Agent selection.
		if option.SourceKind == composerSkillSourceSystem || option.SourceKind == composerSkillSourceTuttiInjected {
			result = append(result, option)
			continue
		}
		if workspaceAgentComposerSkillSelected(option, selected) {
			result = append(result, option)
		}
	}
	return result
}

func filterWorkspaceAgentComposerCapabilities(
	options []ComposerCapabilityOption,
	selected []string,
	capabilitiesExplicit bool,
) []ComposerCapabilityOption {
	selected = normalizedSnapshotStrings(selected)
	if !capabilitiesExplicit && len(selected) == 0 {
		return options
	}
	wanted := make(map[string]struct{}, len(selected))
	for _, value := range selected {
		wanted[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	result := make([]ComposerCapabilityOption, 0, len(options))
	for _, option := range options {
		// Skills have their own allowlist and have already been filtered before
		// they are projected into the unified capability catalog.
		if option.Kind == "skill" || workspaceAgentComposerCapabilitySelected(option, wanted) {
			result = append(result, option)
		}
	}
	return result
}

func workspaceAgentComposerCapabilitySelected(option ComposerCapabilityOption, wanted map[string]struct{}) bool {
	candidates := []string{
		option.ID,
		option.Name,
		option.PluginName,
		option.ServerName,
		option.ToolName,
		option.Trigger,
		option.Path,
	}
	if strings.TrimSpace(option.ServerName) != "" {
		candidates = append(candidates, "mcpServer:"+option.ServerName)
	}
	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if _, ok := wanted[candidate]; ok {
			return true
		}
	}
	return false
}

func workspaceAgentComposerSkillSelected(option ComposerSkillOption, selected []string) bool {
	name, plugin := option.Name, option.PluginName
	if prefix, suffix, ok := strings.Cut(name, ":"); ok {
		plugin, name = prefix, suffix
	}
	// Use the runtime selector verbatim, including namespace, alias and
	// case-sensitive path handling. The menu must never widen this selection.
	catalog := localskills.Catalog{Skills: []localskills.Skill{{Name: name, PluginName: plugin, Path: option.Path, UserInvocable: true}}}
	return len(localskills.Filter(catalog, selected, true).Skills) != 0
}

// Native plugin catalogs are additional skill sources and obey the same custom
// agent selection as filesystem skills, rather than bypassing it as tools.
func filterWorkspaceAgentCapabilitySkills(options []ComposerCapabilityOption, selected []string, explicit bool) []ComposerCapabilityOption {
	if !explicit && len(selected) == 0 {
		return options
	}
	result := make([]ComposerCapabilityOption, 0, len(options))
	for _, option := range options {
		if option.Kind != "skill" || option.Path == "builtin:skill-creator" || workspaceAgentComposerSkillSelected(ComposerSkillOption{Name: option.Name, PluginName: option.PluginName, Trigger: option.Trigger, Path: option.Path}, selected) {
			result = append(result, option)
		}
	}
	return result
}
