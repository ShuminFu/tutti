package agent

import (
	"context"
	"log/slog"
	"strings"

	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep/localskills"
)

// The native app-server owns enabled plugins and installed-version selection.
// Never enumerate plugin cache versions, copy credentials, or enable plugin MCP
// merely to expose its instruction files to an isolated runtime.
func (s *Service) nativeSkillReferences(ctx context.Context, provider, cwd string) []localskills.Skill {
	if provider != "codex" {
		return nil
	}
	if _, ok := s.RuntimePreparer.(*runtimeprep.DefaultPreparer); !ok && s.CapabilityLister == nil {
		return nil
	}
	lister, ok, err := composerCapabilityCatalogLister(composerProfileFor(provider))
	if err != nil || !ok {
		return nil
	}
	lister.RequestSet = appServerCatalogRequestSetLocalSkills
	var options []ComposerCapabilityOption
	if s.CapabilityLister != nil {
		options, _ = s.CapabilityLister.ListComposerCapabilityOptions(ctx, provider, cwd, nil)
	} else {
		options, err = lister.List(ctx, cwd)
	}
	if err != nil {
		slog.Warn("native skill catalog incomplete", "error", err)
	}
	return nativeSkillReferencesFromOptions(options)
}

func nativeSkillReferencesFromOptions(options []ComposerCapabilityOption) []localskills.Skill {
	result := make([]localskills.Skill, 0)
	for _, option := range options {
		if option.Kind != "skill" || option.Status != "available" || option.Path == "" {
			continue
		}
		skill, err := localskills.Read(option.Path)
		if err != nil {
			slog.Warn("native skill skipped", "error", err)
			continue
		}
		if option.Name != skill.Name {
			prefix, suffix, ok := strings.Cut(option.Name, ":")
			if !ok || suffix != skill.Name || prefix == "" {
				continue
			}
			skill.PluginName = prefix
		} else {
			skill.PluginName = option.PluginName
		}
		skill.SourceKind = "plugin"
		result = append(result, skill)
	}
	return result
}
