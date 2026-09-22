package agent

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	workspacedata "github.com/tutti-os/tutti/services/tuttid/data/workspace"
)

func TestComposerLiveModelListProbeCounts(t *testing.T) {
	t.Setenv("TUTTI_STATE_DIR", t.TempDir())
	store := openComposerModelCacheStore(t, filepath.Join(t.TempDir(), "tutti.sqlite"))
	runtime := newCursorModelProbeRuntime(t)
	service := newCursorModelProbeService(runtime, store)

	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	input := cursorModelProbeInput()
	if _, err := service.GetComposerOptions(context.Background(), input); err != nil {
		t.Fatalf("mount GetComposerOptions: %v", err)
	}
	if len(runtime.startCalls) != 0 {
		t.Fatalf("mount probes = %d, want 0", len(runtime.startCalls))
	}
	if !strings.Contains(output.String(), "模型列表缓存缺失，等待用户刷新") {
		t.Fatalf("mount log %q, want cache-miss line", output.String())
	}

	opened, err := service.OpenComposerModelDropdown(context.Background(), input)
	if err != nil {
		t.Fatalf("open dropdown: %v", err)
	}
	if len(runtime.startCalls) != 1 {
		t.Fatalf("open probes = %d, want 1", len(runtime.startCalls))
	}
	if len(opened.ModelConfig.Options) != 3 || opened.ModelConfig.Options[0].Value != "default[]" {
		t.Fatalf("opened models = %#v", opened.ModelConfig.Options)
	}
	row := mustComposerModelCacheRow(t, store, input)
	if row.LastError != "" || row.FetchedAtUnixMS == 0 {
		t.Fatalf("stored row = %#v, want fetched_at and empty last_error", row)
	}

	if _, err := service.OpenComposerModelDropdown(context.Background(), input); err != nil {
		t.Fatalf("fresh open: %v", err)
	}
	if len(runtime.startCalls) != 1 {
		t.Fatalf("fresh open probes = %d, want 1", len(runtime.startCalls))
	}

	clearFakeRuntimeSessions(runtime)
	if _, err := service.RefreshComposerModelList(context.Background(), input); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(runtime.startCalls) != 2 {
		t.Fatalf("forced refresh probes = %d, want 2", len(runtime.startCalls))
	}

	clearFakeRuntimeSessions(runtime)
	runtime.startErr = errLiveModelDiscoverySessionFailed
	failed, err := service.RefreshComposerModelList(context.Background(), input)
	if err != nil {
		t.Fatalf("failed refresh error = %v, want the previous list", err)
	}
	if len(failed.ModelConfig.Options) != 3 {
		t.Fatalf("failed refresh models = %#v, want the previous list kept", failed.ModelConfig.Options)
	}
	failedRow := mustComposerModelCacheRow(t, store, input)
	if failedRow.LastError == "" {
		t.Fatal("failed refresh stored empty last_error")
	}
	if cache, ok := failed.RuntimeContext["composerModelListCache"].(map[string]any); !ok || cache["lastError"] == "" {
		t.Fatalf("runtime cache = %#v, want last_error", failed.RuntimeContext["composerModelListCache"])
	}
}

func TestComposerLiveModelListRestartReadsDiskWithoutProbe(t *testing.T) {
	t.Setenv("TUTTI_STATE_DIR", t.TempDir())
	dbPath := filepath.Join(t.TempDir(), "tutti.sqlite")
	store := openComposerModelCacheStore(t, dbPath)
	runtime := newCursorModelProbeRuntime(t)
	service := newCursorModelProbeService(runtime, store)
	input := cursorModelProbeInput()
	if _, err := service.RefreshComposerModelList(context.Background(), input); err != nil {
		t.Fatalf("seed refresh: %v", err)
	}
	seeded := mustComposerModelCacheRow(t, store, input)
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	reopened := openComposerModelCacheStore(t, dbPath)
	restartRuntime := newCursorModelProbeRuntime(t)
	restarted := newCursorModelProbeService(restartRuntime, reopened)
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	options, err := restarted.GetComposerOptions(context.Background(), input)
	if err != nil {
		t.Fatalf("restarted GetComposerOptions: %v", err)
	}
	if len(restartRuntime.startCalls) != 0 {
		t.Fatalf("restart probes = %d, want 0", len(restartRuntime.startCalls))
	}
	if len(options.ModelConfig.Options) != 3 || options.ModelConfig.Options[0].Value != "default[]" {
		t.Fatalf("restarted models = %#v", options.ModelConfig.Options)
	}
	if !strings.Contains(output.String(), "模型列表命中落盘缓存") {
		t.Fatalf("restart log %q, want disk-hit line", output.String())
	}
	loaded := mustComposerModelCacheRow(t, reopened, input)
	if loaded.FetchedAtUnixMS != seeded.FetchedAtUnixMS || loaded.ModelsJSON != seeded.ModelsJSON {
		t.Fatalf("reopened row = %#v, seeded %#v", loaded, seeded)
	}

	expired := newCursorModelProbeService(newCursorModelProbeRuntime(t), reopened)
	expired.LiveModelCacheTTL = time.Minute
	backdated := loaded
	backdated.FetchedAtUnixMS = time.Now().UTC().Add(-2 * time.Minute).UnixMilli()
	if err := reopened.PutComposerLiveModelCache(context.Background(), backdated); err != nil {
		t.Fatalf("backdate cache: %v", err)
	}
	if _, err := expired.OpenComposerModelDropdown(context.Background(), input); err != nil {
		t.Fatalf("expired open: %v", err)
	}
	if len(expired.Runtime.(*fakeRuntime).startCalls) != 1 {
		t.Fatalf("expired open probes = %d, want 1", len(expired.Runtime.(*fakeRuntime).startCalls))
	}
}

// 扩展缓存键含有效 ComposerSettings 的签名。写入和重启后的读取必须带同一份非空设置，
// 否则落盘的那一行对不上，下一次打开又会探测。
func TestComposerLiveModelListRestartReadsExtensionSettingsWithoutProbe(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tutti.sqlite")
	store := openComposerModelCacheStore(t, dbPath)
	runtime := newExtensionModelProbeRuntime()
	service := newExtensionModelProbeService(runtime, store)
	cwd := t.TempDir()
	settings := ComposerSettings{
		Model:            "example-pro",
		PermissionModeID: "default",
		ReasoningEffort:  "deep",
	}
	input := extensionModelProbeInput(cwd, settings)

	seeded, err := service.RefreshComposerModelList(context.Background(), input)
	if err != nil {
		t.Fatalf("seed refresh: %v", err)
	}
	if !composerModelValuesContain(seeded.ModelConfig.Options, "example-pro") {
		t.Fatalf("seeded models = %#v, want example-pro", seeded.ModelConfig.Options)
	}
	if len(runtime.startCalls) != 1 {
		t.Fatalf("seed probes = %d, want 1", len(runtime.startCalls))
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	reopened := openComposerModelCacheStore(t, dbPath)
	restartRuntime := newExtensionModelProbeRuntime()
	restarted := newExtensionModelProbeService(restartRuntime, reopened)
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	options, err := restarted.GetComposerOptions(context.Background(), input)
	if err != nil {
		t.Fatalf("restarted GetComposerOptions: %v", err)
	}
	if len(restartRuntime.startCalls) != 0 {
		t.Fatalf("restart probes = %d, want 0", len(restartRuntime.startCalls))
	}
	if !composerModelValuesContain(options.ModelConfig.Options, "example-pro") {
		t.Fatalf("restarted models = %#v, want the stored extension list", options.ModelConfig.Options)
	}
	if !strings.Contains(output.String(), liveModelCacheDiskHitMessage) {
		t.Fatalf("restart log %q, want disk-hit line", output.String())
	}

	// 空 settings 的签名不同。还能读到 example-pro，就说明键没有带上那份设置。
	missRuntime := newExtensionModelProbeRuntime()
	missed, err := newExtensionModelProbeService(missRuntime, reopened).GetComposerOptions(
		context.Background(),
		extensionModelProbeInput(cwd, ComposerSettings{}),
	)
	if err != nil {
		t.Fatalf("empty-settings read: %v", err)
	}
	if len(missRuntime.startCalls) != 0 {
		t.Fatalf("empty-settings probes = %d, want 0", len(missRuntime.startCalls))
	}
	if composerModelValuesContain(missed.ModelConfig.Options, "example-pro") {
		t.Fatalf("empty-settings models = %#v, want a miss of the settings-scoped row", missed.ModelConfig.Options)
	}
}

func openComposerModelCacheStore(t *testing.T, dbPath string) *workspacedata.SQLiteStore {
	t.Helper()
	store, err := workspacedata.OpenSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func newCursorModelProbeRuntime(t *testing.T) *fakeRuntime {
	t.Helper()
	runtime := newFakeRuntime()
	runtime.startHook = func(input RuntimeStartInput, session ProviderRuntimeSession) ProviderRuntimeSession {
		if input.Provider != "cursor" {
			t.Fatalf("start provider = %q, want cursor", input.Provider)
		}
		if input.Visible == nil || *input.Visible {
			t.Fatalf("visible = %#v, want hidden discovery session", input.Visible)
		}
		session.RuntimeContext = cursorModelRuntimeContext()
		return session
	}
	return runtime
}

func newCursorModelProbeService(runtime *fakeRuntime, store *workspacedata.SQLiteStore) *Service {
	service := newIsolatedAgentService(runtime)
	service.LiveModelDiscoveryDeleteDelay = time.Hour
	service.UseComposerLiveModelCacheStore(store)
	return service
}

func cursorModelProbeInput() ComposerOptionsInput {
	return ComposerOptionsInput{
		Provider:    "cursor",
		WorkspaceID: "ws-1",
		Cwd:         "/repo",
	}
}

func mustComposerModelCacheRow(t *testing.T, store *workspacedata.SQLiteStore, input ComposerOptionsInput) workspacedata.ComposerLiveModelCacheRow {
	t.Helper()
	row, found, err := store.GetComposerLiveModelCache(context.Background(), newComposerLiveModelScope(input.Provider, input.WorkspaceID, input.Cwd, "").key())
	if err != nil || !found {
		t.Fatalf("cache row found=%v err=%v", found, err)
	}
	return row
}

func clearFakeRuntimeSessions(runtime *fakeRuntime) {
	runtime.sessions = map[string]ProviderRuntimeSession{}
}

func newExtensionModelProbeRuntime() *fakeRuntime {
	runtime := newFakeRuntime()
	runtime.startHook = func(input RuntimeStartInput, session ProviderRuntimeSession) ProviderRuntimeSession {
		if input.Visible == nil || *input.Visible {
			return session
		}
		runtimeContext := clonePayload(session.RuntimeContext)
		runtimeContext["configOptions"] = []any{
			map[string]any{
				"id": "model-choice",
				"options": []any{
					map[string]any{"value": "example-pro", "name": "Example Pro"},
					map[string]any{"value": "example-fast", "name": "Example Fast"},
				},
			},
		}
		session.RuntimeContext = runtimeContext
		return session
	}
	return runtime
}

func newExtensionModelProbeService(runtime *fakeRuntime, store *workspacedata.SQLiteStore) *Service {
	service := newIsolatedAgentService(runtime)
	service.LiveModelDiscoveryDeleteDelay = time.Hour
	service.UseComposerLiveModelCacheStore(store)
	service.AgentTargetStore = fakeAgentTargetStore{targets: map[string]agenttargetbiz.Target{
		"extension:example": {
			ID:            "extension:example",
			Provider:      "acp:example",
			LaunchRefJSON: `{"type":"agent_extension","extensionInstallationId":"example@1.0.0"}`,
			Name:          "Example Agent",
			Enabled:       true,
			Source:        agenttargetbiz.SourceSystem,
		},
	}}
	service.ExtensionComposerProfiles = extensionComposerProfileResolverStub{
		profile: ExtensionComposerProfile{
			ModelConfigOptionID:     "model-choice",
			ReasoningConfigOptionID: "thought_level",
			PermissionModes: []ExtensionComposerPermissionMode{
				{RuntimeID: "default", Semantic: PermissionModeSemanticAskBeforeWrite},
			},
		},
	}
	return service
}

func extensionModelProbeInput(cwd string, settings ComposerSettings) ComposerOptionsInput {
	return ComposerOptionsInput{
		Provider:      "acp:example",
		WorkspaceID:   "workspace-1",
		Cwd:           cwd,
		AgentTargetID: "extension:example",
		Settings:      settings,
	}
}

func composerModelValuesContain(options []ComposerConfigOptionValue, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}
