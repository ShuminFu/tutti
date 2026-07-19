package claudesidecar

import (
	"os"
	"sync/atomic"
)

// SessionSettings mirrors SidecarSessionSettings.
type SessionSettings struct {
	Model            string
	PermissionModeID string
	PlanMode         bool
	Effort           string
	Speed            string
}

// ConfigOption mirrors SidecarConfigOption.
type ConfigOption struct {
	ID           string             `json:"id"`
	Name         string             `json:"name,omitempty"`
	Description  string             `json:"description,omitempty"`
	Category     string             `json:"category,omitempty"`
	Type         string             `json:"type,omitempty"`
	CurrentValue string             `json:"currentValue,omitempty"`
	Options      []ConfigOptionItem `json:"options"`
}

// ConfigOptionItem is one selectable value of a ConfigOption.
type ConfigOptionItem struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PendingFlagSettings carries queued live-settings changes for
// apply_flag_settings. A nil pointer field is "unset"; effortLevel may be an
// explicit null on the wire, mirrored by NullEffort.
type PendingFlagSettings struct {
	EffortLevel  string
	EffortSet    bool
	EffortIsNull bool
	FastMode     bool
	FastModeSet  bool
}

func (p PendingFlagSettings) isEmpty() bool {
	return !p.EffortSet && !p.FastModeSet
}

func (p PendingFlagSettings) toWire() map[string]any {
	settings := map[string]any{}
	if p.EffortSet {
		if p.EffortIsNull {
			settings["effortLevel"] = nil
		} else {
			settings["effortLevel"] = p.EffortLevel
		}
	}
	if p.FastModeSet {
		settings["fastMode"] = p.FastMode
	}
	return settings
}

func sidecarSessionSettings(payload map[string]any) *SessionSettings {
	settings := recordValue(payload["settings"])
	permissionModeID := firstNonEmptyText(
		stringValue(payload["permissionModeId"]),
		stringValue(settings["permissionModeId"]),
	)
	if permissionModeID == "" {
		permissionModeID = "default"
	}
	effort := firstNonEmptyText(
		stringValue(payload["effort"]),
		stringValue(settings["effort"]),
		stringValue(settings["reasoningEffort"]),
	)
	return &SessionSettings{
		Model:            stringValue(settings["model"]),
		PermissionModeID: permissionModeID,
		PlanMode:         booleanValue(settings["planMode"]),
		Effort:           effort,
		Speed:            stringValue(settings["speed"]),
	}
}

func effectivePermissionMode(settings *SessionSettings) string {
	if settings.PlanMode {
		return "plan"
	}
	permissionMode := permissionModeValue(settings.PermissionModeID)
	if permissionMode == "bypassPermissions" && !canBypassPermissions() {
		return "default"
	}
	return permissionMode
}

func permissionModeValue(value string) string {
	switch value {
	case "default", "acceptEdits", "bypassPermissions", "plan", "dontAsk", "auto":
		return value
	default:
		return ""
	}
}

func modelOptionValue(value string) string {
	model := normalizeTitle(value)
	if model == "" || model == "default" {
		return ""
	}
	return model
}

func sidecarModelOptionsFromInitializationResult(value map[string]any) []ConfigOptionItem {
	rawModels, _ := value["models"].([]any)
	options := make([]ConfigOptionItem, 0, len(rawModels))
	seen := map[string]struct{}{}
	for _, item := range rawModels {
		model := recordValue(item)
		if model == nil {
			continue
		}
		modelValue := firstNonEmptyText(
			stringValue(model["value"]),
			stringValue(model["id"]),
			stringValue(model["modelId"]),
			stringValue(model["model_id"]),
		)
		if modelValue == "" {
			continue
		}
		if _, duplicate := seen[modelValue]; duplicate {
			continue
		}
		seen[modelValue] = struct{}{}
		name := firstNonEmptyText(
			stringValue(model["displayName"]),
			stringValue(model["display_name"]),
			stringValue(model["name"]),
			modelValue,
		)
		options = append(options, ConfigOptionItem{
			Value:       modelValue,
			Name:        name,
			Description: stringValue(model["description"]),
		})
	}
	return options
}

func defaultSidecarModelOptionValue(options []ConfigOptionItem) string {
	for _, option := range options {
		if option.Value == "default" {
			return option.Value
		}
	}
	if len(options) > 0 {
		return options[0].Value
	}
	return "default"
}

func effortLevelValue(value string) (string, bool) {
	switch value {
	case "low", "medium", "high", "xhigh":
		return value, true
	default:
		return "", false
	}
}

func flagSettingsFromSessionSettings(settings *SessionSettings) PendingFlagSettings {
	result := PendingFlagSettings{}
	if settings.Effort != "" {
		level, valid := effortLevelValue(settings.Effort)
		result.EffortSet = true
		if valid {
			result.EffortLevel = level
		} else {
			result.EffortIsNull = true
		}
	}
	switch settings.Speed {
	case "fast":
		result.FastModeSet = true
		result.FastMode = true
	case "standard":
		result.FastModeSet = true
		result.FastMode = false
	}
	return result
}

func querySettingsFromSessionSettings(settings *SessionSettings) map[string]any {
	result := map[string]any{}
	switch settings.Speed {
	case "fast":
		result["fastMode"] = true
	case "standard":
		result["fastMode"] = false
	}
	return result
}

func approvalOptions() []map[string]any {
	return []map[string]any{
		{"kind": "allow_always", "name": "Allow for session", "optionId": "allow_always"},
		{"kind": "allow_once", "name": "Allow", "optionId": "allow"},
		{"kind": "reject_once", "name": "Reject", "optionId": "reject"},
	}
}

func exitPlanOptions() []map[string]any {
	options := []map[string]any{
		{"kind": "allow_always", "name": `Yes, and use "auto" mode`, "optionId": "auto"},
		{"kind": "allow_always", "name": "Yes, and auto-accept edits", "optionId": "acceptEdits"},
		{"kind": "allow_once", "name": "Yes, and manually approve edits", "optionId": "default"},
		{"kind": "reject_once", "name": "No, keep planning", "optionId": "plan"},
	}
	if canBypassPermissions() {
		options = append([]map[string]any{
			{"kind": "allow_always", "name": "Yes, and bypass permissions", "optionId": "bypassPermissions"},
		}, options...)
	}
	return options
}

func isAllowOption(optionID string) bool {
	switch optionID {
	case "allow", "allow_always", "accept", "acceptEdits", "default", "auto", "bypassPermissions":
		return true
	default:
		return false
	}
}

func isExitPlanAllowOption(optionID string) bool {
	if optionID == "bypassPermissions" {
		return canBypassPermissions()
	}
	switch optionID {
	case "default", "acceptEdits", "auto":
		return true
	default:
		return false
	}
}

// sandboxed marks the sidecar as daemon-embedded. The daemon always spawned
// the TypeScript sidecar with IS_SANDBOX=1; the in-process Go sidecar sets
// this instead so bypass-permission behavior stays identical.
var sandboxed atomic.Bool

// SetSandboxed marks the process as sandboxed the way IS_SANDBOX=1 does.
func SetSandboxed(value bool) {
	sandboxed.Store(value)
}

func canBypassPermissions() bool {
	isRoot := os.Geteuid() == 0
	return !isRoot || sandboxed.Load() || os.Getenv("IS_SANDBOX") != ""
}
