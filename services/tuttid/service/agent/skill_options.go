package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep/localskills"
)

const (
	composerSkillSourceProject       = "project"
	composerSkillSourcePersonal      = "personal"
	composerSkillSourceBundled       = "bundled"
	composerSkillSourcePlugin        = "plugin"
	composerSkillSourceSystem        = "system"
	composerSkillSourceTuttiInjected = "tutti-injected"
)

var hiddenTuttiProviderSkills = map[string]struct{}{
	"tutti-cli":              {},
	"tutti-handoff":          {},
	"issue-manager":          {},
	"workspace-app":          {},
	"reference":              {},
	"browser-use":            {},
	"computer-use":           {},
	"tutti-model-allocation": {},
}

func discoverComposerSkillOptions(provider string, cwd string, env []string) []ComposerSkillOption {
	roots, triggerFor := composerSkillDiscoveryPlan(provider, cwd, env)
	var native []ComposerSkillOption
	if triggerFor != nil {
		native = discoverComposerSkillOptionsFromRoots(roots, triggerFor)
	}
	return mergeLocalComposerSkills(cwd, native)
}

func (s *Service) discoverComposerSkillOptions(provider string, cwd string, env []string) []ComposerSkillOption {
	// Discovery is cheap and must include metadata/alias edits and deletions,
	// including agents/openai.yaml, without retaining unbounded signatures.
	return discoverComposerSkillOptions(provider, cwd, env)
}

func mergeLocalComposerSkills(cwd string, native []ComposerSkillOption) []ComposerSkillOption {
	catalog := runtimeprep.LocalSkillCatalog(cwd, runtimeprep.LocalSkillSelection{})
	result := make([]ComposerSkillOption, 0, len(catalog.Skills)+len(native))
	seen := map[string]bool{}
	for _, skill := range catalog.Skills {
		// Reserve names even when hidden, so a lower-priority native entry cannot
		// undo user-invocable:false on the winning standard skill.
		seen[localskills.Identity(skill)] = true
		if !skill.UserInvocable {
			continue
		}
		result = append(result, ComposerSkillOption{Name: skill.Name, Trigger: "/" + skill.Name, Description: skill.Description, Path: skill.Path, SourceKind: skill.SourceKind, Invocation: "promptItem"})
	}
	for _, skill := range native {
		key := skill.Name
		if skill.PluginName != "" {
			key = skill.PluginName + ":" + key
		}
		if seen[key] || (skill.PluginName == "tutti-cli" && seen[skill.Name]) {
			continue
		}
		seen[key] = true
		result = append(result, skill)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SourceKind != result[j].SourceKind {
			return skillSourceRank(result[i].SourceKind) < skillSourceRank(result[j].SourceKind)
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (s *Service) discoverComposerSkillOptionsForLaunch(
	ctx context.Context,
	provider string,
	cwd string,
	env []string,
	providerTargetRef map[string]any,
) []ComposerSkillOption {
	if providerTargetRefKind(providerTargetRef) != "agent_extension" {
		return s.discoverComposerSkillOptions(provider, cwd, env)
	}
	profile, err := s.extensionComposerProfileForLaunch(ctx, providerTargetRef)
	if err != nil || profile.Skills == nil {
		return mergeLocalComposerSkills(cwd, nil)
	}
	roots := extensionComposerSkillRoots(cwd, profile.Skills.Roots)
	triggerFor := extensionSkillTrigger(profile.Skills.TriggerPrefix)
	if triggerFor == nil {
		return nil
	}
	options := discoverComposerSkillOptionsFromRoots(roots, triggerFor)
	for index := range options {
		options[index].Invocation = strings.TrimSpace(profile.Skills.Invocation)
	}
	return mergeLocalComposerSkills(cwd, options)
}

func extensionComposerSkillRoots(cwd string, declarations []ExtensionComposerSkillRoot) []composerSkillRoot {
	roots := make([]composerSkillRoot, 0, len(declarations))
	for _, declaration := range declarations {
		relativePath, ok := safeExtensionSkillRootPath(declaration.Path)
		if !ok {
			continue
		}
		switch strings.TrimSpace(declaration.Scope) {
		case "workspace":
			roots = append(roots, ancestorDeclaredSkillRoots(cwd, relativePath)...)
		case "user":
			if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
				roots = append(roots, composerSkillRoot{
					path:       filepath.Join(home, relativePath),
					sourceKind: composerSkillSourcePersonal,
				})
			}
		}
	}
	return roots
}

func ancestorDeclaredSkillRoots(cwd string, relativePath string) []composerSkillRoot {
	current := strings.TrimSpace(cwd)
	if current == "" {
		return nil
	}
	current, err := filepath.Abs(current)
	if err != nil {
		return nil
	}
	roots := make([]composerSkillRoot, 0)
	for {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(current, relativePath),
			sourceKind: composerSkillSourceProject,
		})
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return roots
}

func extensionSkillTrigger(prefix string) skillTriggerFunc {
	if prefix == "" || prefix != strings.TrimSpace(prefix) || utf8.RuneCountInString(prefix) > 8 ||
		(!strings.HasPrefix(prefix, "/") && !strings.HasPrefix(prefix, "$")) ||
		strings.ContainsFunc(prefix, unicode.IsSpace) {
		return nil
	}
	return func(_ composerSkillRoot, name string) string {
		name = strings.TrimSpace(name)
		if name == "" {
			return ""
		}
		return prefix + name
	}
}

func composerSkillDiscoveryPlan(provider string, cwd string, env []string) ([]composerSkillRoot, skillTriggerFunc) {
	profile := composerProfileFor(provider)
	triggerFor := providerComposerSkillTrigger(provider)
	if triggerFor == nil {
		return nil, nil
	}
	switch providerregistry.SkillKind(profile.SkillKind) {
	case providerregistry.SkillKindCodex:
		return codexComposerSkillRoots(cwd, env), triggerFor
	case providerregistry.SkillKindClaudeCode:
		return claudeCodeComposerSkillRoots(cwd, env), triggerFor
	case providerregistry.SkillKindCursor:
		return cursorComposerSkillRoots(cwd, env), triggerFor
	case providerregistry.SkillKindOpenCode:
		return openCodeComposerSkillRoots(cwd, env, profile.SkillConfigDirSuffix), triggerFor
	default:
		return nil, nil
	}
}

func providerComposerSkillTrigger(provider string) skillTriggerFunc {
	if _, ok := providerregistry.Find(provider); !ok {
		return nil
	}
	return func(root composerSkillRoot, name string) string {
		projection, ok := providerregistry.ProjectComposerSkill(provider, name, root.pluginName)
		if !ok {
			return ""
		}
		return projection.Trigger
	}
}

func codexComposerSkillRoots(cwd string, env []string) []composerSkillRoot {
	roots := make([]composerSkillRoot, 0)
	roots = append(roots, ancestorSkillRoots(cwd, ".codex", "skills", composerSkillSourceProject)...)
	if userHome, err := os.UserHomeDir(); err == nil && strings.TrimSpace(userHome) != "" {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(userHome, ".agents", "skills"),
			sourceKind: composerSkillSourcePersonal,
		})
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(userHome, ".codex", "skills"),
			sourceKind: composerSkillSourcePersonal,
		})
	}
	if codexHome := envValue(env, "CODEX_HOME"); codexHome != "" {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(codexHome, "skills", ".system"),
			sourceKind: composerSkillSourceSystem,
		})
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(codexHome, "skills"),
			sourceKind: composerSkillSourceTuttiInjected,
		})
	}
	return roots
}

func claudeCodeComposerSkillRoots(cwd string, env []string) []composerSkillRoot {
	roots := make([]composerSkillRoot, 0)
	roots = append(roots, ancestorSkillRoots(cwd, ".claude", "skills", composerSkillSourceProject)...)
	if userHome, err := os.UserHomeDir(); err == nil && strings.TrimSpace(userHome) != "" {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(userHome, ".claude", "skills"),
			sourceKind: composerSkillSourcePersonal,
		})
	}
	if pluginDir := envValue(env, "TUTTI_CLAUDE_PLUGIN_DIR"); pluginDir != "" {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(pluginDir, "skills"),
			sourceKind: composerSkillSourcePlugin,
			pluginName: claudePluginName(pluginDir),
		})
	}
	return roots
}

func cursorComposerSkillRoots(cwd string, env []string) []composerSkillRoot {
	roots := make([]composerSkillRoot, 0)
	roots = append(roots, ancestorSkillRoots(cwd, ".cursor", "skills", composerSkillSourceProject)...)
	if userHome, err := os.UserHomeDir(); err == nil && strings.TrimSpace(userHome) != "" {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(userHome, ".cursor", "skills"),
			sourceKind: composerSkillSourcePersonal,
		})
	}
	if pluginDir := envValue(env, "TUTTI_CURSOR_PLUGIN_DIR"); pluginDir != "" {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(pluginDir, "skills"),
			sourceKind: composerSkillSourcePlugin,
			pluginName: claudePluginName(pluginDir),
		})
	}
	return roots
}

func openCodeComposerSkillRoots(cwd string, env []string, configDirSuffix string) []composerSkillRoot {
	roots := make([]composerSkillRoot, 0)
	roots = append(roots, ancestorSkillRoots(cwd, ".opencode", "skills", composerSkillSourceProject)...)
	roots = append(roots, ancestorSkillRoots(cwd, ".claude", "skills", composerSkillSourceProject)...)
	roots = append(roots, ancestorSkillRoots(cwd, ".agents", "skills", composerSkillSourceProject)...)
	if userHome, err := os.UserHomeDir(); err == nil && strings.TrimSpace(userHome) != "" {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(userHome, ".config", "opencode", "skills"),
			sourceKind: composerSkillSourcePersonal,
		})
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(userHome, ".claude", "skills"),
			sourceKind: composerSkillSourcePersonal,
		})
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(userHome, ".agents", "skills"),
			sourceKind: composerSkillSourcePersonal,
		})
	}
	if configDir := openCodeConfigDir(env, configDirSuffix); configDir != "" {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(configDir, "skills"),
			sourceKind: composerSkillSourcePersonal,
		})
	}
	return roots
}

func openCodeConfigDir(env []string, configDirSuffix string) string {
	configDir := envValue(env, "OPENCODE_CONFIG_DIR")
	if configDir == "" {
		return ""
	}
	configDirSuffix = strings.TrimSpace(configDirSuffix)
	if configDirSuffix != "" && filepath.Base(filepath.Clean(configDir)) != configDirSuffix {
		configDir = filepath.Join(configDir, configDirSuffix)
	}
	return configDir
}

type composerSkillRoot struct {
	path       string
	sourceKind string
	pluginName string
}

type skillTriggerFunc func(composerSkillRoot, string) string

func discoverComposerSkillOptionsFromRoots(
	roots []composerSkillRoot,
	triggerFor skillTriggerFunc,
) []ComposerSkillOption {
	return discoverProviderSkillRoots(roots, triggerFor)
}

func discoverProviderSkillRoots(roots []composerSkillRoot, triggerFor skillTriggerFunc) []ComposerSkillOption {
	inputs := make([]localskills.Root, 0, len(roots))
	for _, root := range roots {
		inputs = append(inputs, localskills.Root{Path: root.path, SourceKind: root.sourceKind, PluginName: root.pluginName})
	}
	catalog := localskills.Discover(inputs)
	for _, diagnostic := range catalog.Diagnostics {
		slog.Warn("skill discovery", "diagnostic", diagnostic)
	}
	options := make([]ComposerSkillOption, 0, len(catalog.Skills))
	for _, skill := range catalog.Skills {
		root := composerSkillRoot{path: filepath.Dir(filepath.Dir(skill.Path)), sourceKind: skill.SourceKind, pluginName: skill.PluginName}
		if !skill.UserInvocable || shouldHideComposerSkill(root, skill.Name) {
			continue
		}
		trigger := triggerFor(root, skill.Name)
		if trigger == "" {
			continue
		}
		options = append(options, ComposerSkillOption{Name: skill.Name, Trigger: trigger, SourceKind: skill.SourceKind, PluginName: skill.PluginName, Description: skill.Description, Path: skill.Path})
	}
	sort.SliceStable(options, func(i, j int) bool {
		if options[i].SourceKind != options[j].SourceKind {
			return skillSourceRank(options[i].SourceKind) < skillSourceRank(options[j].SourceKind)
		}
		return options[i].Name < options[j].Name
	})
	return options
}

func ancestorSkillRoots(cwd string, parent string, child string, sourceKind string) []composerSkillRoot {
	current := strings.TrimSpace(cwd)
	if current == "" {
		return nil
	}
	abs, err := filepath.Abs(current)
	if err == nil {
		current = abs
	}
	info, err := os.Stat(current)
	if err == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}
	roots := make([]composerSkillRoot, 0)
	for {
		roots = append(roots, composerSkillRoot{
			path:       filepath.Join(current, parent, child),
			sourceKind: sourceKind,
		})
		next := filepath.Dir(current)
		if next == current {
			break
		}
		current = next
	}
	return roots
}

type skillMetadata struct {
	name        string
	description string
}

func readSkillMetadata(path string) (skillMetadata, bool) {
	skill, err := localskills.Read(path)
	return skillMetadata{name: skill.Name, description: skill.Description}, err == nil
}

func shouldHideComposerSkill(root composerSkillRoot, name string) bool {
	if root.sourceKind == composerSkillSourceTuttiInjected {
		return true
	}
	if _, ok := hiddenTuttiProviderSkills[strings.TrimSpace(name)]; ok {
		return true
	}
	return false
}

func composerSkillOptionsRuntimeContext(options []ComposerSkillOption) []map[string]any {
	if len(options) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		value := map[string]any{
			"name":       option.Name,
			"trigger":    option.Trigger,
			"sourceKind": option.SourceKind,
		}
		if option.Description != "" {
			value["description"] = option.Description
		}
		if option.PluginName != "" {
			value["pluginName"] = option.PluginName
		}
		if option.Path != "" {
			value["path"] = option.Path
		}
		if option.Invocation != "" {
			value["invocation"] = option.Invocation
		}
		result = append(result, value)
	}
	return result
}

func composerCapabilityCatalogFromSkills(provider string, skills []ComposerSkillOption) []ComposerCapabilityOption {
	if len(skills) == 0 {
		return []ComposerCapabilityOption{}
	}
	result := make([]ComposerCapabilityOption, 0, len(skills))
	for _, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		trigger := strings.TrimSpace(skill.Trigger)
		if name == "" || trigger == "" {
			continue
		}
		invocation := strings.TrimSpace(skill.Invocation)
		if invocation == "" {
			invocation = strings.TrimSpace(composerProfileFor(provider).SkillInvocation)
		}
		if invocation == "" {
			invocation = "textTrigger"
		}
		result = append(result, ComposerCapabilityOption{
			ID:          "skill:" + name,
			Kind:        "skill",
			Name:        name,
			Label:       name,
			Description: strings.TrimSpace(skill.Description),
			Status:      "available",
			PluginName:  strings.TrimSpace(skill.PluginName),
			Trigger:     trigger,
			Path:        strings.TrimSpace(skill.Path),
			Invocation:  invocation,
		})
	}
	return result
}

func composerCapabilityOptionsRuntimeContext(options []ComposerCapabilityOption) []map[string]any {
	if len(options) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		value := map[string]any{
			"id":         option.ID,
			"kind":       option.Kind,
			"name":       option.Name,
			"label":      option.Label,
			"status":     option.Status,
			"invocation": option.Invocation,
		}
		for key, text := range map[string]string{
			"description": option.Description,
			"source":      option.Source,
			"pluginName":  option.PluginName,
			"serverName":  option.ServerName,
			"toolName":    option.ToolName,
			"trigger":     option.Trigger,
			"path":        option.Path,
		} {
			if strings.TrimSpace(text) != "" {
				value[key] = strings.TrimSpace(text)
			}
		}
		result = append(result, value)
	}
	return result
}

func skillSourceRank(sourceKind string) int {
	switch sourceKind {
	case composerSkillSourceProject:
		return 0
	case composerSkillSourcePersonal:
		return 1
	case composerSkillSourcePlugin:
		return 2
	case composerSkillSourceSystem:
		return 3
	default:
		return 9
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(entry, prefix))
		}
	}
	return ""
}

func claudePluginName(pluginDir string) string {
	pluginDir = strings.TrimSpace(pluginDir)
	if pluginDir == "" {
		return ""
	}
	return strings.TrimSpace(filepath.Base(pluginDir))
}
