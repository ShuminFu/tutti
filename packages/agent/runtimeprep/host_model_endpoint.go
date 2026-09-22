package runtimeprep

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// HostModelEndpointsEnv carries daemon-owned default model endpoints. A
// workspace Model Plan remains authoritative; this contract is the fallback
// before provider-native credentials.
const HostModelEndpointsEnv = "TUTTI_HOST_MODEL_ENDPOINTS"

// HostModelEndpointsFileEnv points at the private, host-updatable form of the
// same contract. It is preferred over the inline environment value so runtime
// settings can change without restarting the DinTalDock daemon.
const HostModelEndpointsFileEnv = "TUTTI_HOST_MODEL_ENDPOINTS_FILE"

type hostModelEndpointsDocument struct {
	Version   int                            `json:"version"`
	Routes    map[string]HostProviderRoute   `json:"routes"`
	Providers map[string]ModelEndpointConfig `json:"providers"`
	// ModelContext is the host's read-only answer to "how many tokens does this
	// gateway model really take", one entry per model id the document publishes.
	// The keys are bare model ids and the values are token counts -- never the
	// `[1m]` spelling, because this document is read by codex and the extension
	// catalogs too, and a marked id reaching a consumer that does not strip the
	// suffix is routed as a model that does not exist.
	//
	// It exists so the composer stops inventing 1M rows. A `X[1m]` row is not a
	// wish the receiving runtime may ignore: for the runtimes that read the
	// window off the model VALUE (Claude Code), it is a routing key that has to
	// resolve against the ids the CLI was told about, and the only component that
	// knows which models actually have a 1M window is the host (its seed table
	// plus the panel override). Absent entry == unknown == do not offer the row.
	ModelContext map[string]int64 `json:"modelContext,omitempty"`
}

// HostProviderRoute is the host-owned execution route for one provider. An
// absent route keeps standalone/provider-native authentication behavior.
type HostProviderRoute struct {
	Mode   string `json:"mode"`
	Status string `json:"status"`
}

// HostModelRoute returns the validated managed gateway route for provider.
func HostModelRoute(provider string) *HostProviderRoute {
	var document hostModelEndpointsDocument
	if err := json.Unmarshal(hostModelEndpointsPayload(), &document); err != nil || document.Version != 1 {
		return nil
	}
	route, ok := document.Routes[strings.TrimSpace(provider)]
	if !ok || strings.TrimSpace(route.Mode) != "gateway" {
		return nil
	}
	route.Mode = "gateway"
	route.Status = strings.TrimSpace(route.Status)
	switch route.Status {
	case "ready", "gateway_config_missing", "gateway_auth_unavailable":
		return &route
	default:
		return nil
	}
}

// HostModelEndpoint returns a validated copy of the host default for provider.
// Invalid or incomplete host input is ignored so standalone DinTalDock keeps its
// provider-native fallback.
func HostModelEndpoint(provider string) *ModelEndpointConfig {
	var document hostModelEndpointsDocument
	if err := json.Unmarshal(hostModelEndpointsPayload(), &document); err != nil || document.Version != 1 {
		return nil
	}
	endpoint, ok := document.Providers[strings.TrimSpace(provider)]
	if !ok || !endpoint.valid() {
		return nil
	}
	endpoint.Protocol = strings.ToLower(strings.TrimSpace(endpoint.Protocol))
	if endpoint.Protocol != "openai" && endpoint.Protocol != "anthropic" {
		return nil
	}
	endpoint.BaseURL = strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	endpoint.APIKey = strings.TrimSpace(endpoint.APIKey)
	endpoint.PlanID = ""
	if strings.TrimSpace(endpoint.PlanName) == "" {
		endpoint.PlanName = "Host Runtime"
	}
	seen := make(map[string]struct{}, len(endpoint.Models))
	models := make([]ModelEndpointModel, 0, len(endpoint.Models))
	for _, model := range endpoint.Models {
		model.ID = strings.TrimSpace(model.ID)
		model.Name = strings.TrimSpace(model.Name)
		if model.ID == "" {
			continue
		}
		if _, exists := seen[model.ID]; exists {
			continue
		}
		seen[model.ID] = struct{}{}
		models = append(models, model)
	}
	endpoint.Models = models
	return &endpoint
}

// HostModelContextWindows returns the host's model-window table (see
// hostModelEndpointsDocument.ModelContext). A missing, malformed, or versionless
// document yields nil, and callers MUST treat "no entry" as "window unknown"
// rather than as a default window: over-reporting a window is the failure mode
// that makes the CLI never auto-compact and blow up at the upstream 400.
func HostModelContextWindows() map[string]int64 {
	var document hostModelEndpointsDocument
	if err := json.Unmarshal(hostModelEndpointsPayload(), &document); err != nil || document.Version != 1 {
		return nil
	}
	windows := make(map[string]int64, len(document.ModelContext))
	for id, window := range document.ModelContext {
		id = strings.TrimSpace(id)
		if id == "" || window <= 0 {
			continue
		}
		windows[id] = window
	}
	if len(windows) == 0 {
		return nil
	}
	return windows
}

func hostModelEndpointsPayload() []byte {
	path := strings.TrimSpace(os.Getenv(HostModelEndpointsFileEnv))
	if filepath.IsAbs(path) {
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Size() <= 1024*1024 {
			if payload, err := os.ReadFile(path); err == nil {
				return payload
			}
		}
	}
	return []byte(strings.TrimSpace(os.Getenv(HostModelEndpointsEnv)))
}
