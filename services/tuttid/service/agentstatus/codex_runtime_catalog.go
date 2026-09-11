package agentstatus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	agentproviderbiz "github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

type CodexRuntimeSelectionState string

const (
	CodexRuntimeSelectionUnavailable       CodexRuntimeSelectionState = "unavailable"
	CodexRuntimeSelectionImplicitUnique    CodexRuntimeSelectionState = "implicit_unique"
	CodexRuntimeSelectionSelectionRequired CodexRuntimeSelectionState = "selection_required"
	CodexRuntimeSelectionSelected          CodexRuntimeSelectionState = "selected"
	CodexRuntimeSelectionStale             CodexRuntimeSelectionState = "stale"
)

type CodexRuntimeCatalog struct {
	CapturedAt time.Time
	Provider   string
	Revision   string
	Selection  CodexRuntimeSelection
	Candidates []CodexRuntimeCatalogCandidate
}

type CodexRuntimeCatalogCandidate struct {
	ID              string
	LauncherPath    string
	PackageRoot     string
	Sources         []string
	Version         string
	State           string
	ReasonCode      string
	AppServerReady  bool
	PackageLayoutOK bool
}

type CodexRuntimeSelection struct {
	State        CodexRuntimeSelectionState
	CandidateID  string
	LauncherPath string
	UpdatedAt    *time.Time
}

type SetCodexRuntimeSelectionInput struct {
	Provider    string
	CandidateID string
	Revision    string
}

type codexRuntimeResolvedSelection struct {
	Selection   agentproviderbiz.RuntimeSelection
	Explicit    bool
	Validations []codexRuntimeCandidateValidation
	Index       int
	Launchable  bool
	ReasonCode  string
	State       CodexRuntimeSelectionState
}

var ErrRuntimeCatalogRevisionConflict = errors.New("codex runtime catalog revision conflicts with current discovery")
var ErrRuntimeCandidateNotFound = errors.New("codex runtime candidate not found")
var ErrRuntimeCandidateNotLaunchable = errors.New("codex runtime candidate is not launchable")
var ErrRuntimeSelectionStoreUnavailable = errors.New("codex runtime selection store is unavailable")

// GetCodexRuntimeCatalog discovers and validates every logically distinct
// Codex installation. It is intentionally independent from status caching: a
// user choosing a local executable must see the current filesystem state.
func (s Service) GetCodexRuntimeCatalog(ctx context.Context, provider string) (CodexRuntimeCatalog, error) {
	specs, err := s.registry().Select([]string{provider})
	if err != nil || len(specs) != 1 || !isCodexStatusSpec(specs[0]) {
		return CodexRuntimeCatalog{}, ErrInvalidProvider
	}
	resolved, err := s.resolveCodexRuntimeSelection(ctx, specs[0])
	if err != nil {
		return CodexRuntimeCatalog{}, err
	}
	catalog := codexRuntimeCatalogFromValidations(specs[0].Provider, resolved.Validations)
	catalog.Selection = codexRuntimeCatalogSelection(catalog.Candidates, resolved)
	return catalog, nil
}

func (s Service) SetCodexRuntimeSelection(ctx context.Context, input SetCodexRuntimeSelectionInput) (CodexRuntimeCatalog, error) {
	if s.CodexRuntimeSelectionStore == nil {
		return CodexRuntimeCatalog{}, ErrRuntimeSelectionStoreUnavailable
	}
	catalog, err := s.GetCodexRuntimeCatalog(ctx, input.Provider)
	if err != nil {
		return CodexRuntimeCatalog{}, err
	}
	if strings.TrimSpace(input.CandidateID) == "" || strings.TrimSpace(input.Revision) == "" || input.Revision != catalog.Revision {
		return CodexRuntimeCatalog{}, ErrRuntimeCatalogRevisionConflict
	}
	for _, candidate := range catalog.Candidates {
		if candidate.ID != input.CandidateID {
			continue
		}
		if candidate.State != string(codexRuntimeCandidateValidationReady) {
			return CodexRuntimeCatalog{}, ErrRuntimeCandidateNotLaunchable
		}
		if _, err := s.CodexRuntimeSelectionStore.PutAgentProviderRuntimeSelection(ctx, agentproviderbiz.RuntimeSelection{
			Provider:     catalog.Provider,
			LauncherPath: candidate.LauncherPath,
		}); err != nil {
			return CodexRuntimeCatalog{}, err
		}
		s.invalidateProviderStatus(catalog.Provider)
		return s.GetCodexRuntimeCatalog(ctx, catalog.Provider)
	}
	return CodexRuntimeCatalog{}, ErrRuntimeCandidateNotFound
}

func (s Service) resolveCodexRuntimeSelection(ctx context.Context, spec ProviderSpec) (codexRuntimeResolvedSelection, error) {
	// 宿主投影（RNDMASTER_CLI_RUNTIME_SELECTION_FILE）排在 Tutti 自己的 catalog 与持久化选择
	// 之前：它比进程环境新，而且**不要求**这条路径出现在 discovery 的结果里——宿主的注册表已经
	// 探测过它（可执行 + 版本下限），这里再走一遍同样的验证（--version + app-server 握手 + 包
	// 布局）就直接采用。这正是「宿主填的 codex 路径不在 catalog 里 → codex 起不来」那条硬失败
	// 的解法，也是 codex 与 claude 对齐的关键：两者都由宿主说了算，而宿主的选择在**使用时**才读。
	//
	// 宿主说「没有选定项」（cleared）或这条路径验证不过时**不吞掉**：落到下面 Tutti 自己的逻辑。
	// 这里与 claude 侧刻意不同——claude 的 cleared 让调用方跳过那个已经过期的内联值，而 codex 的
	// 回落是一整套现发现 + 现验证（不是冻住的旧值），让它继续兜底严格更安全。
	if hostPath, hostState := hostRuntimeSelection(spec.Provider); hostState == hostRuntimeSelectionSet {
		validation := s.validateCodexRuntimeCandidate(ctx, spec, codexRuntimeCandidate{
			LauncherPath: hostPath,
			Sources:      []codexRuntimeCandidateSource{codexRuntimeCandidateSourceHost},
		})
		if validation.State == codexRuntimeCandidateValidationReady {
			return codexRuntimeResolvedSelection{
				Selection:   agentproviderbiz.RuntimeSelection{Provider: spec.Provider, LauncherPath: hostPath},
				Explicit:    true,
				Validations: []codexRuntimeCandidateValidation{validation},
				Index:       0,
				Launchable:  true,
				ReasonCode:  "codex_runtime_host_selected",
				State:       CodexRuntimeSelectionSelected,
			}, nil
		}
	}
	selection, explicit, err := s.codexRuntimeSelection(ctx, spec.Provider)
	if err != nil {
		return codexRuntimeResolvedSelection{}, err
	}
	validations := s.validateCodexRuntimeCandidates(ctx, spec, s.discoverCodexRuntimeCandidates(ctx, spec))
	result := codexRuntimeResolvedSelection{Selection: selection, Explicit: explicit, Validations: validations, Index: -1}
	if !explicit {
		implicit := decideCodexRuntimeImplicitSelection(validations)
		result.Index, result.Launchable, result.ReasonCode, result.State = implicit.CandidateIndex, implicit.Launchable, implicit.ReasonCode, implicit.State
		return result, nil
	}
	for index, validation := range validations {
		if validation.Candidate.LauncherPath != selection.LauncherPath {
			continue
		}
		result.Index = index
		result.Launchable = validation.State == codexRuntimeCandidateValidationReady
		if result.Launchable {
			result.ReasonCode = "codex_runtime_selected_candidate"
			result.State = CodexRuntimeSelectionSelected
		} else {
			result.ReasonCode = "codex_runtime_selection_stale"
			result.State = CodexRuntimeSelectionStale
		}
		return result, nil
	}
	result.ReasonCode = "codex_runtime_selection_stale"
	result.State = CodexRuntimeSelectionStale
	return result, nil
}

func (selection codexRuntimeResolvedSelection) candidate() (codexRuntimeCandidateValidation, bool) {
	if selection.Index < 0 || selection.Index >= len(selection.Validations) {
		return codexRuntimeCandidateValidation{}, false
	}
	return selection.Validations[selection.Index], true
}

func (s Service) codexRuntimeSelection(ctx context.Context, provider string) (agentproviderbiz.RuntimeSelection, bool, error) {
	if s.CodexRuntimeSelectionStore == nil {
		return agentproviderbiz.RuntimeSelection{}, false, ErrRuntimeSelectionStoreUnavailable
	}
	return s.CodexRuntimeSelectionStore.GetAgentProviderRuntimeSelection(ctx, provider)
}

func codexRuntimeCatalogFromValidations(provider string, validations []codexRuntimeCandidateValidation) CodexRuntimeCatalog {
	candidates := make([]CodexRuntimeCatalogCandidate, 0, len(validations))
	for _, validation := range validations {
		candidate := validation.Candidate
		candidates = append(candidates, CodexRuntimeCatalogCandidate{
			ID:              codexRuntimeCandidateID(candidate),
			LauncherPath:    candidate.LauncherPath,
			PackageRoot:     candidate.PackageRoot,
			Sources:         codexRuntimeCandidateSourceStrings(candidate.Sources),
			Version:         validation.Version,
			State:           string(validation.State),
			ReasonCode:      validation.ReasonCode,
			AppServerReady:  validation.Probe.ProtocolReady,
			PackageLayoutOK: codexRuntimePackageLayoutOK(validation.PackageLayout),
		})
	}
	return CodexRuntimeCatalog{
		CapturedAt: time.Now().UTC(),
		Provider:   provider,
		Revision:   codexRuntimeCatalogRevision(candidates),
		Candidates: candidates,
	}
}

func codexRuntimeCandidateSourceStrings(sources []codexRuntimeCandidateSource) []string {
	result := make([]string, 0, len(sources))
	for _, source := range sources {
		result = append(result, string(source))
	}
	return result
}

func codexRuntimePackageLayoutOK(layout CodexPackageLayoutEvidence) bool {
	return (layout.PlatformPackagePresence == CodexPathPresent && layout.PlatformBinaryPresence == CodexPathPresent) ||
		(layout.PlatformPackagePresence == CodexPathNotApplicable && layout.PlatformBinaryPresence == CodexPathNotApplicable)
}

func codexRuntimeCatalogSelection(candidates []CodexRuntimeCatalogCandidate, resolved codexRuntimeResolvedSelection) CodexRuntimeSelection {
	if !resolved.Explicit {
		result := CodexRuntimeSelection{State: resolved.State}
		if resolved.Launchable && resolved.Index >= 0 && resolved.Index < len(candidates) {
			result.CandidateID = candidates[resolved.Index].ID
			result.LauncherPath = candidates[resolved.Index].LauncherPath
		}
		return result
	}
	result := CodexRuntimeSelection{
		State:        CodexRuntimeSelectionStale,
		LauncherPath: resolved.Selection.LauncherPath,
		UpdatedAt:    &resolved.Selection.UpdatedAt,
	}
	for index, candidate := range candidates {
		if candidate.LauncherPath == resolved.Selection.LauncherPath {
			result.State = CodexRuntimeSelectionSelected
			result.CandidateID = candidate.ID
			if !resolved.Launchable || resolved.Index != index {
				result.State = CodexRuntimeSelectionStale
			}
			break
		}
	}
	return result
}

func codexRuntimeCandidateID(candidate codexRuntimeCandidate) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(candidate.LauncherPath)))
	return "codex-" + hex.EncodeToString(hash[:8])
}

func codexRuntimeCatalogRevision(candidates []CodexRuntimeCatalogCandidate) string {
	hash := sha256.New()
	for _, candidate := range candidates {
		_, _ = hash.Write([]byte(candidate.ID))
		_, _ = hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil)[:16])
}
