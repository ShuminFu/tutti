package claudesidecar

import (
	"strings"
)

// configurableQuery is the query capability surface SessionConfiguration
// needs.
type configurableQuery interface {
	InitializationResult() (map[string]any, error)
	SetPermissionMode(mode string) error
	SetModel(model string) error
	ApplyFlagSettings(settings map[string]any) error
}

// SessionConfiguration mirrors sessionConfiguration.ts: mutable session
// settings, model config options, and pending live-flag application.
//
// Callers hold the session runtime lock; blocking query round trips release
// it through the roundTrip hook.
type SessionConfiguration struct {
	Settings            *SessionSettings
	configOptions       []ConfigOption
	pendingFlagSettings PendingFlagSettings
	getQuery            func() configurableQuery
	testDriver          bool
	isInitialized       func() bool
	markInitialized     func()
	emitFastModeState   func(state string)
	roundTrip           func(func() error) error
}

// SessionConfigurationOptions configures a SessionConfiguration.
type SessionConfigurationOptions struct {
	Settings          *SessionSettings
	GetQuery          func() configurableQuery
	TestDriver        bool
	IsInitialized     func() bool
	MarkInitialized   func()
	EmitFastModeState func(state string)
	// RoundTrip runs a blocking query call with the runtime lock released.
	RoundTrip func(func() error) error
}

func NewSessionConfiguration(options SessionConfigurationOptions) *SessionConfiguration {
	roundTrip := options.RoundTrip
	if roundTrip == nil {
		roundTrip = func(callback func() error) error { return callback() }
	}
	configuration := &SessionConfiguration{
		Settings:          options.Settings,
		getQuery:          options.GetQuery,
		testDriver:        options.TestDriver,
		isInitialized:     options.IsInitialized,
		markInitialized:   options.MarkInitialized,
		emitFastModeState: options.EmitFastModeState,
		roundTrip:         roundTrip,
	}
	configuration.mergePendingFlagSettings(flagSettingsFromSessionSettings(options.Settings))
	return configuration
}

// Apply handles an apply_settings payload.
func (c *SessionConfiguration) Apply(payload map[string]any) error {
	if _, present := payload["planMode"]; present {
		c.Settings.PlanMode = booleanValue(payload["planMode"])
	}
	if _, present := payload["permissionMode"]; present {
		if err := c.applyPermissionMode(stringValue(payload["permissionMode"])); err != nil {
			return err
		}
	}
	if _, present := payload["model"]; present {
		if err := c.applyModel(stringValue(payload["model"])); err != nil {
			return err
		}
	}
	if _, present := payload["effort"]; present {
		if err := c.applyEffort(stringValue(payload["effort"])); err != nil {
			return err
		}
	}
	if _, present := payload["speed"]; present {
		speed := stringValue(payload["speed"])
		if speed == "fast" || speed == "standard" {
			if err := c.applyFastMode(speed == "fast"); err != nil {
				return err
			}
		}
	}
	return nil
}

// ApplyPendingFlags flushes queued flag settings to the live query.
func (c *SessionConfiguration) ApplyPendingFlags() error {
	if c.pendingFlagSettings.isEmpty() {
		return nil
	}
	if c.testDriver {
		pending := c.pendingFlagSettings
		c.pendingFlagSettings = PendingFlagSettings{}
		if pending.FastModeSet {
			if pending.FastMode {
				c.emitFastModeState("on")
			} else {
				c.emitFastModeState("off")
			}
		}
		return nil
	}
	query := c.getQuery()
	if query == nil {
		return nil
	}
	if !c.isInitialized() {
		if err := c.roundTrip(func() error {
			_, initErr := query.InitializationResult()
			return initErr
		}); err != nil {
			return err
		}
		c.markInitialized()
	}
	settings := c.pendingFlagSettings
	c.pendingFlagSettings = PendingFlagSettings{}
	if err := c.roundTrip(func() error {
		return query.ApplyFlagSettings(settings.toWire())
	}); err != nil {
		return err
	}
	if settings.FastModeSet {
		if settings.FastMode {
			c.emitFastModeState("on")
		} else {
			c.emitFastModeState("off")
		}
	}
	return nil
}

func (c *SessionConfiguration) ApplyInitializationResult(value map[string]any) {
	if value == nil {
		return
	}
	modelOptions := sidecarModelOptionsFromInitializationResult(value)
	if len(modelOptions) == 0 {
		return
	}
	currentModel := c.resolveModelOptionValue(c.Settings.Model)
	if currentModel == "" {
		currentModel = defaultSidecarModelOptionValue(modelOptions)
	}
	c.Settings.Model = currentModel
	optionValue := currentModel
	if optionValue == "" {
		optionValue = "default"
	}
	c.configOptions = []ConfigOption{
		{
			ID:           "model",
			Name:         "Model",
			Description:  "AI model to use",
			Category:     "model",
			Type:         "select",
			CurrentValue: optionValue,
			Options:      modelOptions,
		},
	}
}

func (c *SessionConfiguration) SessionStatePayload() map[string]any {
	payload := map[string]any{}
	if c.Settings.Model != "" {
		payload["model"] = c.Settings.Model
	}
	if len(c.configOptions) > 0 {
		payload["configOptions"] = configOptionsPayload(c.configOptions)
	}
	return payload
}

func configOptionsPayload(options []ConfigOption) []map[string]any {
	payload := make([]map[string]any, 0, len(options))
	for _, option := range options {
		entry := map[string]any{"id": option.ID}
		if option.Name != "" {
			entry["name"] = option.Name
		}
		if option.Description != "" {
			entry["description"] = option.Description
		}
		if option.Category != "" {
			entry["category"] = option.Category
		}
		if option.Type != "" {
			entry["type"] = option.Type
		}
		if option.CurrentValue != "" {
			entry["currentValue"] = option.CurrentValue
		}
		values := make([]map[string]any, 0, len(option.Options))
		for _, item := range option.Options {
			valueEntry := map[string]any{"value": item.Value, "name": item.Name}
			if item.Description != "" {
				valueEntry["description"] = item.Description
			}
			values = append(values, valueEntry)
		}
		entry["options"] = values
		payload = append(payload, entry)
	}
	return payload
}

func (c *SessionConfiguration) applyPermissionMode(mode string) error {
	permissionMode := permissionModeValue(mode)
	if permissionMode == "" {
		return nil
	}
	if permissionMode == "bypassPermissions" && !canBypassPermissions() {
		permissionMode = "default"
	}
	if permissionMode == "plan" {
		c.Settings.PlanMode = true
	} else {
		c.Settings.PlanMode = false
		c.Settings.PermissionModeID = permissionMode
	}
	query := c.getQuery()
	if c.testDriver || query == nil {
		return nil
	}
	return c.roundTrip(func() error {
		return query.SetPermissionMode(permissionMode)
	})
}

func (c *SessionConfiguration) applyModel(model string) error {
	resolvedModel := c.resolveModelOptionValue(model)
	c.Settings.Model = resolvedModel
	query := c.getQuery()
	if c.testDriver || query == nil {
		return nil
	}
	setModel := resolvedModel
	if setModel == "default" {
		setModel = ""
	}
	if err := c.roundTrip(func() error {
		return query.SetModel(setModel)
	}); err != nil {
		return err
	}
	currentValue := resolvedModel
	if currentValue == "" {
		currentValue = "default"
	}
	c.updateConfigOptionCurrentValue("model", currentValue)
	return nil
}

func (c *SessionConfiguration) applyEffort(effort string) error {
	c.Settings.Effort = effort
	pending := PendingFlagSettings{EffortSet: true}
	if level, valid := effortLevelValue(effort); valid {
		pending.EffortLevel = level
	} else {
		pending.EffortIsNull = true
	}
	c.mergePendingFlagSettings(pending)
	return c.ApplyPendingFlags()
}

func (c *SessionConfiguration) applyFastMode(enabled bool) error {
	if enabled {
		c.Settings.Speed = "fast"
	} else {
		c.Settings.Speed = "standard"
	}
	c.mergePendingFlagSettings(PendingFlagSettings{FastModeSet: true, FastMode: enabled})
	return c.ApplyPendingFlags()
}

func (c *SessionConfiguration) resolveModelOptionValue(model string) string {
	requested := normalizeTitle(model)
	if requested == "" {
		return ""
	}
	var modelOption *ConfigOption
	for index := range c.configOptions {
		if c.configOptions[index].ID == "model" {
			modelOption = &c.configOptions[index]
			break
		}
	}
	if modelOption == nil {
		return requested
	}
	for _, option := range modelOption.Options {
		if option.Value == requested {
			return option.Value
		}
	}
	lower := strings.ToLower(requested)
	for _, option := range modelOption.Options {
		if strings.ToLower(option.Value) == lower || strings.ToLower(option.Name) == lower {
			return option.Value
		}
	}
	return requested
}

func (c *SessionConfiguration) updateConfigOptionCurrentValue(id string, value string) {
	for index := range c.configOptions {
		if c.configOptions[index].ID == id {
			c.configOptions[index].CurrentValue = value
		}
	}
}

func (c *SessionConfiguration) mergePendingFlagSettings(settings PendingFlagSettings) {
	if settings.EffortSet {
		c.pendingFlagSettings.EffortSet = true
		c.pendingFlagSettings.EffortLevel = settings.EffortLevel
		c.pendingFlagSettings.EffortIsNull = settings.EffortIsNull
	}
	if settings.FastModeSet {
		c.pendingFlagSettings.FastModeSet = true
		c.pendingFlagSettings.FastMode = settings.FastMode
	}
}
