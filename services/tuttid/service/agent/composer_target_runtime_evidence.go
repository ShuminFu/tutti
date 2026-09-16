package agent

import (
	"context"
	"strings"
	"time"

	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

// composerTargetRuntimeEvidence is the last runtime ACP config-option evidence
// the daemon observed for one agent target, keyed by agent target id rather than
// by workspace/cwd scope.
//
// Why a separate store exists at all
// ----------------------------------
// The desktop composer publishes "remember these as my defaults" as ONE sparse
// patch containing no workspace and no cwd (see
// preferences.agent.composer.defaults.patch.requested). Runtime evidence,
// however, is only ever produced under a full launch scope (provider + workspace
// + cwd + extension installation + settings signature), because that is what
// the hidden model-discovery session needs to start. Without a target-scoped
// projection, validating a defaults patch for an extension-backed target could
// only consult the provider registry's static composer profile — which declares
// nothing for extension providers — so a perfectly legal `reasoningEffort`
// value was rejected as "not configurable for agent target".
//
// Freshness policy: last-known-good, never hard-expired
// ----------------------------------------------------
// The same reasoning that keeps `composerLiveModelCache.targetCatalogs` around
// applies here, and more sharply: a rejected defaults patch is a user-visible
// failure with no retry path, because the patch carries no scope with which the
// daemon could re-derive the evidence. So this store reports the entry as usable
// as long as it exists; `stale` merely says the entry is older than the display
// TTL. Callers decide what to do with stale evidence (today: use it anyway,
// which is exactly what an unrecoverable sparse patch needs).
//
// Update and invalidation
// -----------------------
// Update: every successful live-model discovery for a target mirrors the
// observed runtime config options into this store, keyed by agent target id
// (see setComposerRuntimeContextForScope). Ordering is by observation time —
// the ACP session's creation timestamp — not by write time, so a stale
// discovery session that finishes late cannot overwrite a newer observation.
//
// Invalidation: a provider's auth/config-file change drops the provider's
// whole model catalog and this evidence with it
// (InvalidateLiveComposerModels). An extension (re)install also drops the
// provider's projections via agentextension.Manager.OnInstallationChanged,
// because a replaced runtime package may advertise a different option set.
// Daemon restarts clear the store; the next session repopulates it.
type composerTargetRuntimeEvidence struct {
	agentTargetID  string
	configOptions  []map[string]any
	provider       string
	revision       uint64
	updatedAt      time.Time
	installationID string
}

// composerTargetRuntimeEvidenceFreshnessThreshold mirrors the display cache TTL:
// evidence older than this is still served, but the caller can tell it is stale.
func (s *Service) composerTargetRuntimeEvidenceStaleAfter() time.Duration {
	if s.LiveModelCacheTTL != 0 {
		return s.LiveModelCacheTTL
	}
	return defaultLiveModelCacheTTL
}

// recordComposerTargetRuntimeEvidence stores the runtime config options observed
// for the scope's agent target.
//
// Only a strictly newer observation may replace an existing entry. Two
// discovery sessions for the same target can finish out of order (different
// workspaces and cwds share one target), and the newer observation must win
// regardless of which goroutine writes last; observedAt is the ACP session's
// creation timestamp, not the write time.
func (s *Service) recordComposerTargetRuntimeEvidence(
	scope composerLiveModelScope,
	observedAt time.Time,
	runtimeContext map[string]any,
) {
	if s == nil {
		return
	}
	agentTargetID := strings.TrimSpace(scope.agentTargetID)
	if agentTargetID == "" {
		return
	}
	configOptions := runtimeConfigOptionsAsMapSlice(runtimeContext["configOptions"])
	if len(configOptions) == 0 {
		return
	}
	s.composerTargetEvidenceMu.Lock()
	defer s.composerTargetEvidenceMu.Unlock()
	if s.composerTargetEvidence == nil {
		s.composerTargetEvidence = make(map[string]composerTargetRuntimeEvidence)
	}
	if existing, ok := s.composerTargetEvidence[agentTargetID]; ok &&
		!existing.updatedAt.IsZero() && existing.updatedAt.After(observedAt) {
		return
	}
	s.composerTargetEvidenceRevision++
	s.composerTargetEvidence[agentTargetID] = composerTargetRuntimeEvidence{
		agentTargetID:  agentTargetID,
		configOptions:  cloneRuntimeConfigOptions(configOptions),
		provider:       agentprovider.NormalizeOpen(scope.provider),
		revision:       s.composerTargetEvidenceRevision,
		updatedAt:      observedAt,
		installationID: strings.TrimSpace(scope.installationID),
	}
}

// composerTargetRuntimeEvidenceFor returns the last observed runtime config
// options for the agent target. The second result reports whether usable
// evidence exists; the third reports whether it is older than the staleness
// threshold. Stale evidence is still returned — see the type comment.
func (s *Service) composerTargetRuntimeEvidenceFor(
	agentTargetID string,
	now time.Time,
) ([]map[string]any, bool, bool) {
	if s == nil {
		return nil, false, false
	}
	agentTargetID = strings.TrimSpace(agentTargetID)
	if agentTargetID == "" {
		return nil, false, false
	}
	s.composerTargetEvidenceMu.Lock()
	defer s.composerTargetEvidenceMu.Unlock()
	entry, ok := s.composerTargetEvidence[agentTargetID]
	if !ok || len(entry.configOptions) == 0 {
		return nil, false, false
	}
	stale := false
	if threshold := s.composerTargetRuntimeEvidenceStaleAfter(); threshold > 0 && !entry.updatedAt.IsZero() {
		stale = now.Sub(entry.updatedAt) > threshold
	}
	return cloneRuntimeConfigOptions(entry.configOptions), true, stale
}

// InvalidateComposerTargetRuntimeEvidence drops the stored evidence for one
// agent target so the next composer-options read falls back to a live session,
// a fresh discovery probe, or the static declaration.
func (s *Service) InvalidateComposerTargetRuntimeEvidence(agentTargetID string) {
	if s == nil {
		return
	}
	agentTargetID = strings.TrimSpace(agentTargetID)
	if agentTargetID == "" {
		return
	}
	s.composerTargetEvidenceMu.Lock()
	defer s.composerTargetEvidenceMu.Unlock()
	delete(s.composerTargetEvidence, agentTargetID)
}

// InvalidateComposerTargetRuntimeEvidenceForProvider drops stored evidence for
// every target of the provider. Provider auth/config changes invalidate the
// whole provider's catalogs (see InvalidateLiveComposerModels), and the stored
// evidence is the same kind of observation, so it must move with them.
func (s *Service) InvalidateComposerTargetRuntimeEvidenceForProvider(provider string) int {
	if s == nil {
		return 0
	}
	normalized := agentprovider.NormalizeOpen(provider)
	if normalized == "" {
		return 0
	}
	s.composerTargetEvidenceMu.Lock()
	defer s.composerTargetEvidenceMu.Unlock()
	deleted := 0
	for agentTargetID, entry := range s.composerTargetEvidence {
		if entry.provider != normalized {
			continue
		}
		delete(s.composerTargetEvidence, agentTargetID)
		deleted++
	}
	return deleted
}

// applyTargetRuntimeEvidence folds the last runtime ACP config-option evidence
// observed for the target into composer options, but only when the caller's own
// scope produced no runtime config options of its own.
//
// Precedence, highest first:
//  1. the caller scope's live runtime context (a running session, or the scoped
//     discovery cache) — already merged before this runs;
//  2. this target-scoped last-known-good evidence;
//  3. the extension's static composer-profile declaration, applied after this.
//
// Scoping down to the caller's own scope first matters: a live session in the
// caller's workspace is strictly better evidence than a stored observation from
// some other workspace, and merging both would let a stale target-wide entry
// shadow the session the caller is actually talking to.
func (s *Service) applyTargetRuntimeEvidence(
	_ context.Context,
	input ComposerOptionsInput,
	extensionProfile ExtensionComposerProfile,
	locale string,
	requestedPermissionModeID string,
	options ComposerOptions,
) (ComposerOptions, error) {
	if !input.IncludeTargetRuntimeEvidence {
		return options, nil
	}
	if providerTargetRefKind(input.providerTargetRef) != "agent_extension" {
		// Non-extension providers have a static provider-registry composer
		// profile that already describes their configurable options; the
		// evidence store exists only to fill the extension gap.
		return options, nil
	}
	if len(runtimeConfigOptionsAsMapSlice(options.RuntimeContext["configOptions"])) > 0 {
		return options, nil
	}
	evidence, ok, stale := s.composerTargetRuntimeEvidenceFor(input.AgentTargetID, time.Now().UTC())
	if !ok {
		return options, nil
	}
	// Stale evidence is still applied on purpose: the sparse defaults patch has
	// no scope with which to refresh it, so refusing it would convert a
	// recoverable staleness into a hard "not configurable" rejection.
	if options.RuntimeContext == nil {
		options.RuntimeContext = map[string]any{}
	}
	options.RuntimeContext["configOptions"] = mergeRuntimeConfigOptions(
		runtimeConfigOptionsAsMapSlice(options.RuntimeContext["configOptions"]),
		evidence,
	)
	options.RuntimeContext["composerDefaultsTargetEvidence"] = map[string]any{
		"source": "targetRuntimeEvidence",
		"stale":  stale,
	}
	return projectRuntimeConfigOptionsForComposerOptions(extensionRuntimeConfigProjectionInput{
		agentTargetID:             input.AgentTargetID,
		configOptions:             evidence,
		extensionProfile:          extensionProfile,
		fallbackPermissionModeID:  input.Settings.PermissionModeID,
		locale:                    locale,
		options:                   options,
		provider:                  input.Provider,
		requestedPermissionModeID: requestedPermissionModeID,
	})
}
