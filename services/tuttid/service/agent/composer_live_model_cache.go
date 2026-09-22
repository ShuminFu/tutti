package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
	workspacedata "github.com/tutti-os/tutti/services/tuttid/data/workspace"
)

// defaultLiveModelCacheTTL is the composer model-list lifetime. 24 hours
// replaces the old 10 minute memory TTL so a tuttid restart can keep serving
// the last list. Service.LiveModelCacheTTL still overrides this when set.
// Providers with PreserveLiveModelCache keep a non-expiring effective TTL.
const defaultLiveModelCacheTTL = 24 * time.Hour

const liveModelCacheDiskHitMessage = "模型列表命中落盘缓存"
const liveModelCacheMissMessage = "模型列表缓存缺失，等待用户刷新"

type composerLiveModelCacheStore interface {
	GetComposerLiveModelCache(context.Context, string) (workspacedata.ComposerLiveModelCacheRow, bool, error)
	PutComposerLiveModelCache(context.Context, workspacedata.ComposerLiveModelCacheRow) error
	DeleteComposerLiveModelCachePrefix(context.Context, string) error
}

type composerLiveModelCache struct {
	mu             sync.Mutex
	entries        map[string]composerLiveModelCacheEntry
	targetCatalogs map[string]composerLiveModelCacheEntry
}

type composerLiveModelCacheEntry struct {
	agentTargetID  string
	cachedAt       time.Time
	lastError      string
	options        []ComposerConfigOptionValue
	provider       string
	runtimeContext map[string]any
}

func newComposerLiveModelCache() *composerLiveModelCache {
	return &composerLiveModelCache{
		entries:        make(map[string]composerLiveModelCacheEntry),
		targetCatalogs: make(map[string]composerLiveModelCacheEntry),
	}
}

func (c *composerLiveModelCache) get(key string, now time.Time, ttl time.Duration) ([]ComposerConfigOptionValue, bool, bool) {
	if c == nil {
		return nil, false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false, false
	}
	// ttl <= 0 means the entry never expires (last-known-good). Cursor uses this
	// via PreserveLiveModelCache. An expired entry stays in memory so the picker
	// can still show the previous list; only an explicit open or refresh probes.
	if ttl > 0 && now.Sub(entry.cachedAt) > ttl {
		return nil, false, true
	}
	return cloneComposerConfigOptionValues(entry.options), true, false
}

func (c *composerLiveModelCache) peek(key string) (composerLiveModelCacheEntry, bool) {
	if c == nil {
		return composerLiveModelCacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return composerLiveModelCacheEntry{}, false
	}
	entry.options = cloneComposerConfigOptionValues(entry.options)
	entry.runtimeContext = clonePayload(entry.runtimeContext)
	return entry, true
}

func (c *composerLiveModelCache) set(scope composerLiveModelScope, now time.Time, options []ComposerConfigOptionValue) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := scope.key()
	entry := c.entries[key]
	entry.agentTargetID = strings.TrimSpace(scope.agentTargetID)
	entry.cachedAt = now
	entry.lastError = ""
	entry.options = cloneComposerConfigOptionValues(options)
	entry.provider = agentprovider.NormalizeOpen(scope.provider)
	c.entries[key] = entry
	if entry.agentTargetID != "" {
		// The scoped entry serves composer presentation and may expire. Keep a
		// separate last-known-good target catalog for sparse defaults patches,
		// which carry no workspace/cwd and therefore cannot safely rediscover
		// the menu's descriptor at mutation time.
		c.targetCatalogs[key] = entry
	}
}

func (c *composerLiveModelCache) optionsForTarget(
	provider string,
	agentTargetID string,
) ([]ComposerConfigOptionValue, bool) {
	if c == nil {
		return nil, false
	}
	provider = agentprovider.NormalizeOpen(provider)
	agentTargetID = strings.TrimSpace(agentTargetID)
	if provider == "" || agentTargetID == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]ComposerConfigOptionValue, 0)
	seen := make(map[string]struct{})
	for _, entry := range c.targetCatalogs {
		if entry.provider != provider || entry.agentTargetID != agentTargetID {
			continue
		}
		for _, option := range entry.options {
			value := strings.TrimSpace(option.Value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, option)
		}
	}
	return cloneComposerConfigOptionValues(result), len(result) > 0
}

func (c *composerLiveModelCache) getRuntimeContext(key string, now time.Time, ttl time.Duration) (map[string]any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || len(entry.runtimeContext) == 0 {
		return nil, false
	}
	if ttl > 0 && now.Sub(entry.cachedAt) > ttl {
		return nil, false
	}
	return clonePayload(entry.runtimeContext), true
}

func (c *composerLiveModelCache) setRuntimeContext(key string, now time.Time, runtimeContext map[string]any) {
	if c == nil || len(runtimeContext) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[key]
	entry.cachedAt = now
	entry.runtimeContext = clonePayload(runtimeContext)
	c.entries[key] = entry
}

func (c *composerLiveModelCache) invalidateProvider(provider string) int {
	if c == nil {
		return 0
	}
	prefix := "live-model:" + agentprovider.NormalizeOpen(provider) + ":"
	c.mu.Lock()
	defer c.mu.Unlock()
	deleted := 0
	for key := range c.entries {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
			deleted++
		}
	}
	for key := range c.targetCatalogs {
		if strings.HasPrefix(key, prefix) {
			delete(c.targetCatalogs, key)
			deleted++
		}
	}
	return deleted
}

// InvalidateLiveComposerModels drops the discovered model lists (and the
// once-per-key discovery attempt markers) for the given provider so composer
// options can re-discover models after the provider's auth or config files
// changed on disk.
func (s *Service) InvalidateLiveComposerModels(provider string) {
	if s == nil {
		return
	}
	normalized := agentprovider.NormalizeOpen(provider)
	if normalized == "" {
		return
	}
	nowUnixMS := time.Now().UnixMilli()
	deletedCacheEntries := s.liveComposerModelCache().invalidateProvider(normalized)
	s.deletePersistedComposerModelList(normalized)
	// Target-scoped runtime evidence is a projection of the same observation as
	// the model cache, so a provider-level invalidation must drop both. Keeping
	// the evidence would leave sparse defaults patches validating against a
	// catalog the daemon just decided was wrong.
	deletedTargetEvidence := s.InvalidateComposerTargetRuntimeEvidenceForProvider(normalized)
	prefix := "live-model:" + normalized + ":"
	s.liveModelDiscoveryMu.Lock()
	if s.liveModelInvalidatedAtUnixMS == nil {
		s.liveModelInvalidatedAtUnixMS = make(map[string]int64)
	}
	s.liveModelInvalidatedAtUnixMS[normalized] = nowUnixMS
	deletedAttemptMarkers := 0
	for key := range s.liveModelDiscoveryAttempted {
		if strings.HasPrefix(key, prefix) {
			delete(s.liveModelDiscoveryAttempted, key)
			deletedAttemptMarkers++
		}
	}
	for key := range s.liveModelPersistedScanMissAtUnixMS {
		if strings.HasPrefix(key, prefix) {
			delete(s.liveModelPersistedScanMissAtUnixMS, key)
		}
	}
	logClaudeModelCatalogInvalidationDebug("live_composer_models_invalidated", map[string]any{
		"provider":              normalized,
		"deletedCacheEntries":   deletedCacheEntries,
		"deletedAttemptMarkers": deletedAttemptMarkers,
		"deletedTargetEvidence": deletedTargetEvidence,
		"occurredAtUnixMs":      nowUnixMS,
	})
	s.liveModelDiscoveryMu.Unlock()
}

func (s *Service) liveModelCacheTTL(provider string) time.Duration {
	if s.LiveModelCacheTTL != 0 {
		return s.LiveModelCacheTTL
	}
	// Providers with a preserved cache keep their last-known-good catalog until
	// an explicit invalidation or a running session advertises a fresher list.
	if composerProfileFor(provider).Behavior.PreserveLiveModelCache {
		return 0
	}
	return defaultLiveModelCacheTTL
}

func (s *Service) liveComposerModelCache() *composerLiveModelCache {
	if s.liveModelCache == nil {
		s.liveModelCache = newComposerLiveModelCache()
	}
	return s.liveModelCache
}

func (s *Service) getLiveComposerModelOptions(provider, workspaceID, cwd string, now time.Time) ([]ComposerConfigOptionValue, bool) {
	return s.getLiveComposerModelOptionsForScope(newComposerLiveModelScope(provider, workspaceID, cwd, ""), now)
}

func (s *Service) setLiveComposerModelOptions(provider, workspaceID, cwd string, now time.Time, options []ComposerConfigOptionValue) {
	if len(options) == 0 {
		return
	}
	s.setLiveComposerModelOptionsForScope(newComposerLiveModelScope(provider, workspaceID, cwd, ""), now, options)
}

func (s *Service) getLiveComposerModelOptionsForScope(scope composerLiveModelScope, now time.Time) ([]ComposerConfigOptionValue, bool) {
	snapshot := s.readComposerModelList(context.Background(), scope, now)
	if snapshot.State == composerModelListFresh {
		return snapshot.Options, true
	}
	if snapshot.State == composerModelListStale {
		s.clearLiveModelDiscoveryAttempt(scope.key())
	}
	return nil, false
}

func (s *Service) setLiveComposerModelOptionsForScope(scope composerLiveModelScope, now time.Time, options []ComposerConfigOptionValue) {
	if len(options) == 0 {
		return
	}
	s.liveComposerModelCache().set(scope, now, options)
	s.persistComposerModelList(context.Background(), scope, now, options, "")
}

type composerModelListState string

const (
	composerModelListMissing composerModelListState = "missing"
	composerModelListFresh   composerModelListState = "fresh"
	composerModelListStale   composerModelListState = "stale"
)

type composerModelListSnapshot struct {
	FetchedAt time.Time
	LastError string
	Options   []ComposerConfigOptionValue
	State     composerModelListState
	FromDisk  bool
}

// readComposerModelList checks memory first, then the tuttid state database.
// It never starts a hidden probe. A disk hit logs 模型列表命中落盘缓存.
func (s *Service) readComposerModelList(
	ctx context.Context,
	scope composerLiveModelScope,
	now time.Time,
) composerModelListSnapshot {
	if ctx == nil {
		ctx = context.Background()
	}
	key := scope.key()
	ttl := s.liveModelCacheTTL(scope.provider)
	if entry, ok := s.liveComposerModelCache().peek(key); ok && (len(entry.options) > 0 || entry.lastError != "") {
		return composerModelListSnapshot{
			FetchedAt: entry.cachedAt,
			LastError: entry.lastError,
			Options:   entry.options,
			State:     composerModelListFreshness(entry.cachedAt, now, ttl, len(entry.options) > 0),
		}
	}
	row, found, err := s.loadComposerModelListRow(ctx, key)
	if err != nil {
		slog.Warn("composer live model cache read failed", "provider", scope.provider, "error", err.Error())
		return composerModelListSnapshot{State: composerModelListMissing}
	}
	if !found {
		return composerModelListSnapshot{State: composerModelListMissing}
	}
	options := decodeComposerModelList(row.ModelsJSON)
	fetchedAt := time.UnixMilli(row.FetchedAtUnixMS).UTC()
	if len(options) > 0 {
		s.hydrateComposerModelList(scope, fetchedAt, options, row.LastError)
	}
	snapshot := composerModelListSnapshot{
		FetchedAt: fetchedAt,
		LastError: row.LastError,
		Options:   options,
		State:     composerModelListFreshness(fetchedAt, now, ttl, len(options) > 0),
		FromDisk:  true,
	}
	if len(options) > 0 {
		// 内存是空的（典型情况是 tuttid 刚重启）。这次能给出列表，是因为库里还有上次的结果。
		slog.Info(liveModelCacheDiskHitMessage, "provider", scope.provider)
	}
	return snapshot
}

func composerModelListFreshness(fetchedAt, now time.Time, ttl time.Duration, hasModels bool) composerModelListState {
	if !hasModels {
		return composerModelListMissing
	}
	if ttl > 0 && now.Sub(fetchedAt) > ttl {
		return composerModelListStale
	}
	return composerModelListFresh
}

func (s *Service) loadComposerModelListRow(
	ctx context.Context,
	scopeKey string,
) (workspacedata.ComposerLiveModelCacheRow, bool, error) {
	if s == nil || s.liveModelCacheStore == nil {
		return workspacedata.ComposerLiveModelCacheRow{}, false, nil
	}
	return s.liveModelCacheStore.GetComposerLiveModelCache(ctx, scopeKey)
}

func (s *Service) hydrateComposerModelList(
	scope composerLiveModelScope,
	fetchedAt time.Time,
	options []ComposerConfigOptionValue,
	lastError string,
) {
	cache := s.liveComposerModelCache()
	cache.set(scope, fetchedAt, options)
	if lastError == "" {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := cache.entries[scope.key()]
	entry.lastError = lastError
	cache.entries[scope.key()] = entry
}

func (s *Service) persistComposerModelList(
	ctx context.Context,
	scope composerLiveModelScope,
	fetchedAt time.Time,
	options []ComposerConfigOptionValue,
	lastError string,
) {
	if s == nil || s.liveModelCacheStore == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	encoded := cloneComposerConfigOptionValues(options)
	if encoded == nil {
		encoded = []ComposerConfigOptionValue{}
	}
	payload, err := json.Marshal(encoded)
	if err != nil {
		slog.Warn("composer live model cache encode failed", "provider", scope.provider, "error", err.Error())
		return
	}
	if err := s.liveModelCacheStore.PutComposerLiveModelCache(ctx, workspacedata.ComposerLiveModelCacheRow{
		ScopeKey:        scope.key(),
		Provider:        agentprovider.NormalizeOpen(scope.provider),
		ModelsJSON:      string(payload),
		FetchedAtUnixMS: fetchedAt.UTC().UnixMilli(),
		LastError:       truncateLiveModelCacheError(lastError),
	}); err != nil {
		slog.Warn("composer live model cache write failed", "provider", scope.provider, "error", err.Error())
	}
}

func (s *Service) rememberComposerModelListFailure(
	scope composerLiveModelScope,
	previous composerModelListSnapshot,
	probeErr error,
) {
	// 探测失败留下旧列表。清掉它会让下拉变空白，用户连上次能用的模型都看不到。
	message := ""
	if probeErr != nil {
		message = probeErr.Error()
	}
	fetchedAt := previous.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	options := cloneComposerConfigOptionValues(previous.Options)
	if len(options) > 0 {
		s.hydrateComposerModelList(scope, fetchedAt, options, message)
	} else {
		cache := s.liveComposerModelCache()
		cache.mu.Lock()
		entry := cache.entries[scope.key()]
		entry.cachedAt = fetchedAt
		entry.lastError = message
		entry.provider = agentprovider.NormalizeOpen(scope.provider)
		entry.agentTargetID = strings.TrimSpace(scope.agentTargetID)
		cache.entries[scope.key()] = entry
		cache.mu.Unlock()
	}
	s.persistComposerModelList(context.Background(), scope, fetchedAt, options, message)
}

func (s *Service) logComposerModelListMiss(scope composerLiveModelScope) {
	if s == nil {
		return
	}
	key := scope.key()
	s.liveModelDiscoveryMu.Lock()
	if s.liveModelCacheMissLogged == nil {
		s.liveModelCacheMissLogged = map[string]struct{}{}
	}
	if _, logged := s.liveModelCacheMissLogged[key]; logged {
		s.liveModelDiscoveryMu.Unlock()
		return
	}
	s.liveModelCacheMissLogged[key] = struct{}{}
	s.liveModelDiscoveryMu.Unlock()
	slog.Info(liveModelCacheMissMessage, "provider", scope.provider)
}

// UseComposerLiveModelCacheStore attaches the tuttid state database. Reads
// check memory first and fall through to this store.
func (s *Service) UseComposerLiveModelCacheStore(store composerLiveModelCacheStore) {
	if s == nil {
		return
	}
	s.liveModelCacheStore = store
}

func (s *Service) deletePersistedComposerModelList(provider string) {
	if s == nil || s.liveModelCacheStore == nil {
		return
	}
	prefix := "live-model:" + agentprovider.NormalizeOpen(provider) + ":"
	if err := s.liveModelCacheStore.DeleteComposerLiveModelCachePrefix(context.Background(), prefix); err != nil {
		slog.Warn("composer live model cache delete failed", "provider", provider, "error", err.Error())
	}
}

func decodeComposerModelList(payload string) []ComposerConfigOptionValue {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "null" {
		return nil
	}
	var options []ComposerConfigOptionValue
	if err := json.Unmarshal([]byte(payload), &options); err != nil {
		return nil
	}
	return cloneComposerConfigOptionValues(options)
}

func truncateLiveModelCacheError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 500 {
		return message
	}
	return message[:500]
}

func (s *Service) liveComposerModelOptionsForTarget(
	provider string,
	agentTargetID string,
) ([]ComposerConfigOptionValue, bool) {
	// Defaults are target-scoped rather than workspace/cwd-scoped. Union only
	// last-known-good catalogs the daemon actually observed for this exact
	// target. Display cache TTL does not expire this validation evidence because
	// the sparse patch has no scope with which to rediscover it. Create still
	// validates the selected default against its actual caller scope.
	return s.liveComposerModelCache().optionsForTarget(
		provider,
		agentTargetID,
	)
}

func (s *Service) getComposerRuntimeContextForScope(scope composerLiveModelScope, now time.Time) (map[string]any, bool) {
	return s.liveComposerModelCache().getRuntimeContext(scope.key(), now, s.liveModelCacheTTL(scope.provider))
}

func (s *Service) setComposerRuntimeContextForScope(
	scope composerLiveModelScope,
	now time.Time,
	observedAt time.Time,
	runtimeContext map[string]any,
) {
	s.liveComposerModelCache().setRuntimeContext(scope.key(), now, runtimeContext)
	// Mirror the scoped runtime context into the target-scoped evidence store so
	// a later sparse defaults patch — which has no workspace/cwd with which to
	// rediscover it — can still validate against real runtime ACP options.
	// observedAt (the ACP session's creation time) orders competing observations
	// for the same target; see recordComposerTargetRuntimeEvidence.
	s.recordComposerTargetRuntimeEvidence(scope, observedAt, runtimeContext)
}

// composerLiveModelCacheKey buckets the cache by provider, workspace, cwd scope,
// and (for auth-sensitive providers) an auth-context fingerprint. Providers
// with credential-scoped catalogs use one account-level workspace/cwd scope so
// UI project selection cannot duplicate hidden discovery. Other providers
// retain their caller scope. The same key also scopes the once-per-key hidden
// discovery guard.
func composerLiveModelCacheKey(provider, workspaceID, cwd, authScope string) string {
	scope := newComposerLiveModelScope(provider, workspaceID, cwd, "")
	scope.authScope = strings.TrimSpace(authScope)
	return scope.key()
}

func cloneComposerConfigOptionValues(options []ComposerConfigOptionValue) []ComposerConfigOptionValue {
	if len(options) == 0 {
		return nil
	}
	result := append([]ComposerConfigOptionValue(nil), options...)
	for index := range result {
		result[index].ReasoningEfforts = append(
			[]AgentModelReasoningEffortOption(nil),
			options[index].ReasoningEfforts...,
		)
	}
	return result
}
