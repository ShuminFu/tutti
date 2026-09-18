package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	agentactivitybiz "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	agentproviderbiz "github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
	"github.com/tutti-os/tutti/services/tuttid/data/externalimportcatalog"
)

func (s *Service) ScanExternalImports(ctx context.Context, input ExternalImportScanInput) (ExternalImportScanResult, error) {
	data, err := s.scanExternalAgentSessions(
		ctx,
		normalizeExternalImportProviders(input.Providers),
		input.Days,
		input.ArchivePath,
		input.ArchiveKind,
		externalScanOptions{},
	)
	if err != nil {
		return ExternalImportScanResult{}, err
	}
	return data.result, nil
}

func (s *Service) ImportExternalSessions(ctx context.Context, workspaceID string, input ExternalImportInput) (ExternalImportResult, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || len(input.Projects) == 0 {
		return ExternalImportResult{}, ErrInvalidArgument
	}
	if s == nil || s.ExternalImportStore == nil {
		return ExternalImportResult{}, errors.New("external agent import store is unavailable")
	}
	selections := normalizeExternalImportSelections(input.Projects)
	if len(selections) == 0 {
		return ExternalImportResult{}, ErrInvalidArgument
	}
	sessionIDs, allHaveIDs := externalImportSelectionSessionIDs(selections)
	opts := externalScanOptions{}
	if strings.TrimSpace(input.ArchivePath) != "" && allHaveIDs {
		opts.keepBodies = true
		opts.sessionIDs = sessionIDs
	}
	data, err := s.scanExternalAgentSessions(
		ctx,
		providersFromExternalImportSelections(selections),
		-1,
		input.ArchivePath,
		input.ArchiveKind,
		opts,
	)
	if err != nil {
		return ExternalImportResult{}, err
	}
	result := ExternalImportResult{
		SkippedSessions: data.result.SkippedSessions,
		Errors:          append([]ExternalImportError(nil), data.result.Errors...),
	}
	importedProjectPaths := map[string]struct{}{}
	validProjectPaths := map[string]int64{}
	for _, session := range data.sessions {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		projectPath, selected := matchingExternalImportProject(session, selections)
		if !selected {
			continue
		}
		body := session
		if len(session.Messages) == 0 {
			loaded, loadErr := s.loadExternalImportBody(ctx, session, input)
			if loadErr != nil {
				result.Errors = append(result.Errors, ExternalImportError{
					Provider:   session.Provider,
					SourcePath: session.SourcePath,
					Message:    loadErr.Error(),
				})
				continue
			}
			body = loaded
		}
		importedMessages, imported, err := s.importExternalSession(ctx, workspaceID, body, projectPath)
		if err != nil {
			result.Errors = append(result.Errors, ExternalImportError{
				Provider:   session.Provider,
				SourcePath: session.SourcePath,
				Message:    err.Error(),
			})
			continue
		}
		// Only mark projectPath as a candidate for ProjectPaths once its session
		// has actually landed in the store without error. Marking it here
		// unconditionally (before knowing whether importExternalSession
		// succeeded) let a project whose only session failed to import (a
		// transient store error, a write timeout, etc.) still come back in
		// ProjectPaths — and registerExternalImportUserProjects registers
		// whatever ProjectPaths contains, so that project would surface as a
		// folder card with "No chats yet" and no way to tell it apart from a
		// project that imported cleanly.
		if !session.NoProject && session.UpdatedAtUnixMS > validProjectPaths[projectPath] {
			validProjectPaths[projectPath] = session.UpdatedAtUnixMS
		}
		if imported {
			result.ImportedSessions++
		}
		if importedMessages > 0 {
			result.ImportedMessages += importedMessages
			if !session.NoProject {
				importedProjectPaths[projectPath] = struct{}{}
			}
		}
	}
	result.ImportedProjects = len(importedProjectPaths)
	result.ProjectPaths = sortedProjectPathsByLatest(validProjectPaths)
	return result, nil
}

func normalizeExternalImportProviders(input []string) []string {
	if len(input) == 0 {
		out := make([]string, 0)
		for _, descriptor := range providerregistry.Migrated() {
			if descriptor.ExternalImport.Enabled {
				out = append(out, descriptor.Identity.ID)
			}
		}
		return out
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(input))
	for _, provider := range input {
		normalized := agentproviderbiz.Normalize(provider)
		if normalized == "" {
			// Import-only archive providers (e.g. the ChatGPT data export) are
			// deliberately not runnable registry providers, so they never
			// resolve through providerregistry. Preserve their identity here so
			// import selections that reference them survive normalization.
			normalized = normalizeExternalArchiveImportProvider(provider)
			if normalized == "" {
				continue
			}
		} else {
			descriptor, ok := providerregistry.Find(normalized)
			if !ok || !descriptor.ExternalImport.Enabled {
				continue
			}
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

// normalizeExternalArchiveImportProvider recognizes import-only archive
// providers that have no runnable providerregistry descriptor. Returns the
// canonical identity, or "" if the value is not a known archive-only provider.
func normalizeExternalArchiveImportProvider(provider string) string {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case chatgptExportProvider:
		return chatgptExportProvider
	default:
		return ""
	}
}

func normalizeExternalImportSelections(input []ExternalImportProjectSelection) []ExternalImportProjectSelection {
	out := make([]ExternalImportProjectSelection, 0, len(input))
	for _, selection := range input {
		path, ok := canonicalExistingDir(selection.Path)
		if !ok {
			continue
		}
		out = append(out, ExternalImportProjectSelection{
			Path:       path,
			Providers:  normalizeExternalImportProviders(selection.Providers),
			SessionIDs: normalizeExternalImportSessionIDs(selection.SessionIDs),
		})
	}
	return out
}

func normalizeExternalImportSessionIDs(input []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(input))
	for _, id := range input {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func providersFromExternalImportSelections(selections []ExternalImportProjectSelection) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 2)
	for _, selection := range selections {
		for _, provider := range normalizeExternalImportProviders(selection.Providers) {
			if _, ok := seen[provider]; ok {
				continue
			}
			seen[provider] = struct{}{}
			out = append(out, provider)
		}
	}
	return out
}

// externalImportedSessionSettings carries the provider-reported model/effort
// forward into the imported session's composer settings so continuing the
// conversation in Tutti reuses the same model configuration the user's local
// CLI had, instead of silently falling back to workspace defaults.
func externalImportedSessionSettings(session externalImportedSession) map[string]any {
	reasoningEffort := strings.TrimSpace(session.ReasoningEffort)
	if !composerProviderUsesModelReasoningCatalog(session.Provider) {
		reasoningEffort = normalizeReasoningEffortForProvider(session.Provider, reasoningEffort)
	}
	settings := ComposerSettings{
		Model:           strings.TrimSpace(session.Model),
		ReasoningEffort: reasoningEffort,
	}
	if composerSettingsIsEmpty(settings) {
		return nil
	}
	return composerSettingsToPayload(settings)
}

func externalImportAgentTargetID(provider string) string {
	normalized := agentproviderbiz.Normalize(provider)
	if descriptor, ok := providerregistry.Find(normalized); ok {
		return descriptor.Target.ID
	}
	return ""
}

func externalImportSelectionSessionIDs(selections []ExternalImportProjectSelection) (map[string]struct{}, bool) {
	ids := map[string]struct{}{}
	allHaveIDs := true
	for _, selection := range selections {
		if len(selection.SessionIDs) == 0 {
			allHaveIDs = false
			continue
		}
		for _, id := range selection.SessionIDs {
			ids[id] = struct{}{}
		}
	}
	return ids, allHaveIDs && len(ids) > 0
}

func (s *Service) loadExternalImportBody(
	ctx context.Context,
	summary externalImportedSession,
	input ExternalImportInput,
) (externalImportedSession, error) {
	if strings.TrimSpace(input.ArchivePath) != "" {
		data, err := s.scanExternalAgentSessions(
			ctx,
			[]string{summary.Provider},
			-1,
			input.ArchivePath,
			input.ArchiveKind,
			externalScanOptions{
				keepBodies: true,
				sessionIDs: map[string]struct{}{
					externalImportedSessionID(summary.Provider, summary.ProviderSessionID): {},
				},
			},
		)
		if err != nil {
			return externalImportedSession{}, err
		}
		for _, session := range data.sessions {
			if session.Provider == summary.Provider && session.ProviderSessionID == summary.ProviderSessionID {
				return session, nil
			}
		}
		return externalImportedSession{}, fmt.Errorf("selected session %s was not found in the archive", summary.ProviderSessionID)
	}
	const attempts = 3
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return externalImportedSession{}, err
		}
		before, err := inspectExternalImportSource(summary.SourcePath)
		if err != nil {
			return externalImportedSession{}, err
		}
		descriptor, ok := providerregistry.Find(summary.Provider)
		if !ok {
			return externalImportedSession{}, fmt.Errorf("provider %q is not importable", summary.Provider)
		}
		session, parsed, err := parseExternalProviderJSONL(ctx, descriptor, summary.SourcePath)
		if err != nil {
			lastErr = err
			continue
		}
		after, err := inspectExternalImportSource(summary.SourcePath)
		if err != nil {
			return externalImportedSession{}, err
		}
		if !externalimportcatalog.ContentSignaturesEqual(before, after) {
			lastErr = fmt.Errorf("source file changed while reading")
			continue
		}
		if !parsed {
			return externalImportedSession{}, fmt.Errorf("selected session %s is empty", summary.ProviderSessionID)
		}
		if session.Provider != summary.Provider || session.ProviderSessionID != summary.ProviderSessionID {
			return externalImportedSession{}, fmt.Errorf("source file identity changed for %s", summary.ProviderSessionID)
		}
		return session, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("source file changed while reading")
	}
	return externalImportedSession{}, lastErr
}

func inspectExternalImportSource(path string) (externalimportcatalog.Signature, error) {
	return externalimportcatalog.InspectPath(path)
}

// normalizeExternalImportArchiveKind resolves a request archive kind, defaulting
// an empty value to Claude for backward compatibility with clients that only
// send archivePath.
func normalizeExternalImportArchiveKind(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case ExternalImportArchiveKindChatGPT:
		return ExternalImportArchiveKindChatGPT
	case "", ExternalImportArchiveKindClaude:
		return ExternalImportArchiveKindClaude
	default:
		// Unknown kinds fall back to Claude rather than silently importing
		// nothing; the API layer validates the enum before reaching here.
		return ExternalImportArchiveKindClaude
	}
}

func providersIncludeArchiveImportParser(providers []string) bool {
	for _, provider := range providers {
		descriptor, ok := providerregistry.Find(provider)
		if ok && descriptor.ExternalImport.ParserKind == providerregistry.ExternalImportParserKindClaudeJSONL {
			return true
		}
	}
	return false
}

// externalScanCutoffUnixMS resolves the "updated since" cutoff for a scan
// window expressed in days. 0 keeps the historical 30-day default; a negative
// value disables the cutoff so all available history is scanned (used by the
// import path, which filters by explicit selections instead).
func externalScanCutoffUnixMS(days int) int64 {
	if days < 0 {
		return 0
	}
	if days == 0 {
		days = 30
	}
	return time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
}

// externalProviderRoots returns the local transcript roots to scan, in priority
// order: the env-var root the process was pointed at (falling back to the
// descriptor's default root when it is unset), then any extra roots the
// embedding host declared through ExtraRootsEnvVar.
//
// The extra roots exist because a host that redirects the primary variable for
// isolation — an embedded deployment pointing CLAUDE_CONFIG_DIR at its own
// managed config dir so the user's own ~/.claude is never touched — would
// otherwise make the user's real CLI history invisible to import. Declaring the
// user's root as an extra root keeps both sides importable at once. Unset means
// exactly one root, i.e. the historical behaviour.
func externalProviderRoots(descriptor providerregistry.ExternalImportDescriptor) []string {
	roots := make([]string, 0, 4)
	appendRoot := func(root string) {
		root = strings.TrimSpace(root)
		if root == "" {
			return
		}
		for _, existing := range roots {
			if existing == root {
				return
			}
		}
		roots = append(roots, root)
	}
	if descriptor.RootEnvVar != "" {
		appendRoot(os.Getenv(descriptor.RootEnvVar))
	}
	if len(roots) == 0 {
		home, _ := os.UserHomeDir()
		if home != "" && strings.HasPrefix(descriptor.DefaultRoot, "~/") {
			appendRoot(filepath.Join(home, strings.TrimPrefix(descriptor.DefaultRoot, "~/")))
		} else {
			appendRoot(descriptor.DefaultRoot)
		}
	}
	if descriptor.ExtraRootsEnvVar != "" {
		for _, root := range filepath.SplitList(os.Getenv(descriptor.ExtraRootsEnvVar)) {
			appendRoot(root)
		}
	}
	return roots
}

func (s *Service) importExternalSession(
	ctx context.Context,
	workspaceID string,
	session externalImportedSession,
	projectPath string,
) (int, bool, error) {
	agentSessionID := externalImportedSessionID(session.Provider, session.ProviderSessionID)
	existingTurnIDs, sessionExists, err := s.existingExternalImportMessageTurnIDs(ctx, workspaceID, agentSessionID)
	if err != nil {
		return 0, false, err
	}
	updates := make([]agentactivitybiz.MessageUpdate, 0, len(session.Messages))
	currentTurnID := ""
	for i, message := range session.Messages {
		messageID := externalImportedMessageIDForMessage(session.Provider, session.ProviderSessionID, message, i)
		if message.Role == "user" && message.Kind == "text" {
			currentTurnID = externalImportedTurnID(messageID)
		}
		existingTurnID, exists := existingTurnIDs[messageID]
		if exists && (currentTurnID == "" || existingTurnID == currentTurnID) {
			continue
		}
		updates = append(updates, agentactivitybiz.MessageUpdate{
			MessageID:         messageID,
			TurnID:            currentTurnID,
			Role:              message.Role,
			Kind:              message.Kind,
			Status:            message.Status,
			Payload:           externalImportedMessagePayload(message),
			OccurredAtUnixMS:  message.OccurredAtUnixMS,
			StartedAtUnixMS:   message.StartedAtUnixMS,
			CompletedAtUnixMS: message.CompletedAtUnixMS,
		})
	}
	runtimeContext := map[string]any{
		"visible":                 true,
		"imported":                true,
		"externalImportNoProject": session.NoProject,
		"externalSourcePath":      session.SourcePath,
	}
	if session.ResumeSupported != nil {
		runtimeContext["externalImportResumeSupported"] = *session.ResumeSupported
	}
	if _, err := s.ExternalImportStore.ReportSessionState(ctx, agentactivitybiz.SessionStateReport{
		WorkspaceID:       workspaceID,
		AgentSessionID:    agentSessionID,
		Origin:            WorkspaceAgentSessionOriginImported,
		AgentTargetID:     externalImportAgentTargetID(session.Provider),
		Provider:          session.Provider,
		ProviderSessionID: session.ProviderSessionID,
		Model:             session.Model,
		Settings:          externalImportedSessionSettings(session),
		RuntimeContext:    runtimeContext,
		Cwd:               session.Cwd,
		ImportProjectPath: projectPath,
		Title:             session.Title,
		Status:            "completed",
		CurrentPhase:      "completed",
		OccurredAtUnixMS:  session.UpdatedAtUnixMS,
		StartedAtUnixMS:   session.StartedAtUnixMS,
		EndedAtUnixMS:     session.UpdatedAtUnixMS,
	}); err != nil {
		return 0, false, err
	}
	if len(updates) == 0 && sessionExists {
		return 0, false, nil
	}
	importedMessages := 0
	for start := 0; start < len(updates); start += 200 {
		end := start + 200
		if end > len(updates) {
			end = len(updates)
		}
		report, err := s.ExternalImportStore.ReportSessionMessages(ctx, agentactivitybiz.SessionMessageReport{
			WorkspaceID:      workspaceID,
			AgentSessionID:   agentSessionID,
			Origin:           WorkspaceAgentSessionOriginImported,
			Provider:         session.Provider,
			HistoricalImport: true,
			Messages:         updates[start:end],
		})
		if err != nil {
			return importedMessages, true, err
		}
		importedMessages += report.AcceptedCount
	}
	return importedMessages, true, nil
}

func (s *Service) existingExternalImportMessageTurnIDs(ctx context.Context, workspaceID string, agentSessionID string) (map[string]string, bool, error) {
	turnIDs := map[string]string{}
	if s == nil || s.ExternalImportStore == nil {
		return turnIDs, false, nil
	}
	if _, ok, err := s.ExternalImportStore.GetSession(ctx, workspaceID, agentSessionID); err != nil || !ok {
		return turnIDs, ok, err
	}
	var after uint64
	for {
		page, ok, err := s.ExternalImportStore.ListSessionMessages(ctx, agentactivitybiz.ListSessionMessagesInput{
			WorkspaceID:    workspaceID,
			AgentSessionID: agentSessionID,
			AfterVersion:   after,
			Limit:          1000,
			Order:          agentactivitybiz.MessageOrderAsc,
		})
		if err != nil || !ok {
			return turnIDs, true, err
		}
		if len(page.Messages) == 0 {
			return turnIDs, true, nil
		}
		for _, message := range page.Messages {
			turnIDs[strings.TrimSpace(message.MessageID)] = strings.TrimSpace(message.TurnID)
			if message.Version > after {
				after = message.Version
			}
		}
		if !page.HasMore {
			return turnIDs, true, nil
		}
	}
}

func externalImportedSessionID(provider string, providerSessionID string) string {
	return "imported-" + externalImportProviderSlug(provider) + "-" + externalStableHash(providerSessionID)[:24]
}

// externalImportProviderSlug resolves a stable, filesystem-safe provider slug
// for imported session identities. Registered runnable providers normalize to
// their canonical id; import-only sources (e.g. the ChatGPT data export, which
// is deliberately not a runnable registry provider) fall back to a sanitized
// form of their raw identity so their session ids stay readable and unique
// instead of collapsing to an empty segment.
func externalImportProviderSlug(provider string) string {
	if normalized := agentproviderbiz.Normalize(provider); normalized != "" {
		return normalized
	}
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(provider)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "import"
	}
	return slug
}

func externalImportedMessageID(provider string, providerSessionID string, rawID string, index int) string {
	return "imported-" + externalStableHash(provider + "\x00" + providerSessionID + "\x00" + rawID + "\x00" + strconv.Itoa(index))[:32]
}

func externalImportedMessageIDForMessage(provider string, providerSessionID string, message externalImportedMessage, index int) string {
	if seed := strings.TrimSpace(message.MessageIDSeed); seed != "" {
		return "imported-" + externalStableHash(provider + "\x00" + providerSessionID + "\x00" + seed)[:32]
	}
	return externalImportedMessageID(provider, providerSessionID, message.RawID, index)
}

func externalImportedTurnID(userMessageID string) string {
	const messagePrefix = "imported-"
	userMessageID = strings.TrimSpace(userMessageID)
	if !strings.HasPrefix(userMessageID, messagePrefix) || len(userMessageID) == len(messagePrefix) {
		return ""
	}
	return "imported-turn-" + strings.TrimPrefix(userMessageID, messagePrefix)
}

func externalImportedMessagePayload(message externalImportedMessage) map[string]any {
	payload := clonePayload(message.Payload)
	if payload == nil {
		payload = map[string]any{}
	}
	if strings.TrimSpace(message.Kind) == "text" {
		payload["text"] = message.Text
	}
	return payload
}

func externalStableHash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func externalSessionTitle(messages []externalImportedMessage) string {
	for _, message := range messages {
		if message.Role == "user" {
			return truncateExternalTitle(message.Text)
		}
	}
	return truncateExternalTitle(messages[0].Text)
}

func truncateExternalTitle(input string) string {
	input = strings.Join(strings.Fields(input), " ")
	const maxTitleRunes = 80
	runes := []rune(input)
	if len(runes) <= maxTitleRunes {
		return input
	}
	return strings.TrimSpace(string(runes[:maxTitleRunes]))
}

func firstExternalMessageUnixMS(messages []externalImportedMessage) int64 {
	for _, message := range messages {
		if message.OccurredAtUnixMS > 0 {
			return message.OccurredAtUnixMS
		}
	}
	return time.Now().UnixMilli()
}

func lastExternalMessageUnixMS(messages []externalImportedMessage) int64 {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].OccurredAtUnixMS > 0 {
			return messages[i].OccurredAtUnixMS
		}
	}
	return firstExternalMessageUnixMS(messages)
}

func normalizeExternalMessageRole(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "user", "assistant", "tool":
		return strings.TrimSpace(strings.ToLower(role))
	default:
		return ""
	}
}

func normalizeExternalMessageKind(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "tool_call":
		return "tool_call"
	case "reasoning":
		return "reasoning"
	default:
		return "text"
	}
}

func normalizeExternalMessageStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "running", "completed", "failed", "canceled", "waiting":
		return strings.TrimSpace(strings.ToLower(status))
	default:
		return "completed"
	}
}
