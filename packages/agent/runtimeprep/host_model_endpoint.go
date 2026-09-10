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
