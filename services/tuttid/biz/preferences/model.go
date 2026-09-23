package preferences

import (
	"strings"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	agentproviderbiz "github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
)

const (
	DesktopAgentDockLayoutLegacySplit = "legacySplit"
	DesktopAgentDockLayoutUnified     = "unified"

	DesktopAgentConversationDetailModeCoding  = "coding"
	DesktopAgentConversationDetailModeGeneral = "general"

	DefaultDesktopAppCatalogChannel              = "production"
	DefaultDesktopAgentDockLayout                = DesktopAgentDockLayoutUnified
	DefaultDesktopAgentConversationDetailMode    = DesktopAgentConversationDetailModeCoding
	DefaultDesktopAgentCLIUpdateCheckEnabled     = true
	DefaultDesktopDockIconStyle                  = "default"
	DefaultDesktopDockPlacement                  = "bottom"
	DefaultDeletedAgentConversationRetentionDays = 30

	// Agent 进程常驻（DINTAL-5308）。
	//
	// 默认保持历史行为：进程留着，空闲 30 分钟由回收器收走。留着的意义是
	// 下一句话不用冷启动；代价是每条会话一份常驻内存。
	//
	// AgentRuntimeIdleMinutes = 0 表示**永不回收**（只在退出应用时清），
	// 给那些宁可吃内存也要秒回的人。上限 1440 分钟（一天），再长与「永不」
	// 没有实际区别，却会让人以为还会被收。
	//
	// AgentRuntimeMaxResident 是第二道闸：TTL 管「一条会话留多久」，它管
	// 「一共留几条」。半小时里开二十条、每条都刚聊过，按 TTL 全都还新鲜，
	// 内存却已经见底。默认 10 条（实测每个 provider 进程约 180MB，10 条 ≈ 1.8G），
	// 超了从最久没说话的那条开始挤；0 = 不限。
	DefaultDesktopAgentRuntimeKeepAliveEnabled = true
	DefaultDesktopAgentRuntimeIdleMinutes      = 30
	MaxDesktopAgentRuntimeIdleMinutes          = 1440
	DefaultDesktopAgentRuntimeMaxResident      = 10
	MaxDesktopAgentRuntimeMaxResident          = 100
	DefaultDesktopBrowserUseConnectionMode     = "isolated"
	DefaultDesktopLocale                       = "en"
	DefaultDesktopMinimizeAnimation            = "scale"
	DefaultDesktopSleepPreventionMode          = "never"
	DefaultDesktopShowAppDeveloperSources      = false
	DefaultDesktopThemeSource                  = "dark"
	DefaultDesktopUpdateChannel                = "rc"
	DefaultDesktopUpdatePolicy                 = "prompt"
	DefaultDesktopWindowSnappingEnabled        = false
	DefaultDesktopWindowSnappingShortcut       = "commandArrows"
)

var DefaultDesktopDefaultAgentProvider = defaultDesktopAgentProvider()

func defaultDesktopAgentProvider() string {
	selected := ""
	selectedPriority := int(^uint(0) >> 1)
	for _, descriptor := range providerregistry.Migrated() {
		priority := descriptor.Desktop.DefaultProviderPriority
		if priority > 0 && priority < selectedPriority {
			selected = descriptor.Identity.ID
			selectedPriority = priority
		}
	}
	if selected == "" {
		panic("provider registry has no desktop default agent provider")
	}
	return selected
}

type DesktopPreferences struct {
	AgentCLIUpdateCheckEnabled bool
	// AgentRuntimeKeepAliveEnabled=false 表示「回合一结束就把 provider 进程还回去」，
	// 此时 AgentRuntimeIdleMinutes 不参与判断。=true 时才按 IdleMinutes 当 TTL。
	//
	// 注意这两项只管**用户自己的会话**。visible=false 的机器派工（工作流、自动化）
	// 恒按「回合结束即放」走，不看这里 —— 它没有「下一句话」，留着纯占内存。
	AgentRuntimeKeepAliveEnabled                bool
	AgentRuntimeIdleMinutes                     int
	AgentRuntimeMaxResident                     int
	AgentComposerDefaultsByProvider             map[string]AgentComposerDefaults
	AgentComposerDefaultsByAgentTarget          map[string]AgentComposerDefaults
	AgentGUIConversationRailCollapsedByProvider map[string]bool
	AgentSessionLaunchModesByWorkspace          map[string]map[string]string
	AgentConversationDetailMode                 string
	AgentDockLayout                             string
	AppCatalogChannel                           string
	BrowserUseConnectionMode                    string
	DefaultAgentProvider                        string
	DockIconStyle                               string
	DockPlacement                               string
	DeletedAgentConversationRetentionDays       int
	FeatureFlags                                map[string]bool
	FileDefaultOpenersByExtension               map[string]string
	Initialized                                 bool
	Locale                                      string
	MinimizeAnimation                           string
	SleepPreventionMode                         string
	ShowAppDeveloperSources                     bool
	ThemeSource                                 string
	UpdateChannel                               string
	UpdatePolicy                                string
	WindowSnappingEnabled                       bool
	WindowSnappingShortcutPreset                string
	WorkbenchShortcuts                          DesktopWorkbenchShortcuts
}

// AgentRuntimeRetentionPatch changes only explicitly supplied retention fields.
// Pointer values preserve the difference between omitted and false or zero.
type AgentRuntimeRetentionPatch struct {
	KeepAliveEnabled *bool
	IdleMinutes      *int
	MaxResident      *int
}

type AgentComposerDefaults struct {
	CodexSaverMode   bool
	Model            string
	PermissionModeID string
	ReasoningEffort  string
	Speed            string
}

const (
	AgentComposerDefaultsFieldModel            = "model"
	AgentComposerDefaultsFieldCodexSaverMode   = "codexSaverMode"
	AgentComposerDefaultsFieldPermissionModeID = "permissionModeId"
	AgentComposerDefaultsFieldReasoningEffort  = "reasoningEffort"
	AgentComposerDefaultsFieldSpeed            = "speed"
)

// AgentComposerDefaultsPatch is a sparse field mutation. Text fields accept a
// string or nil (clear); codexSaverMode accepts a boolean. An absent key is
// left unchanged.
type AgentComposerDefaultsPatch map[string]any

func (d AgentComposerDefaults) IsZero() bool {
	return !d.CodexSaverMode && d.Model == "" && d.PermissionModeID == "" && d.ReasoningEffort == "" && d.Speed == ""
}

// LocalAgentTargetIDForProvider maps a provider to the id of its built-in
// local agent target (see biz/agenttarget.IDLocalCodex and friends).
func LocalAgentTargetIDForProvider(provider string) string {
	normalized := agentproviderbiz.Normalize(provider)
	if normalized == "" {
		return ""
	}
	return "local:" + normalized
}

type DesktopWorkbenchShortcuts struct {
	NewAgentConversation string
	NewSameTypeWindow    string
	// CaptureScreenshot empty means the built-in default accelerator applies,
	// not "unbound" like the other bindings.
	CaptureScreenshot string
}

func DefaultDesktopPreferences() DesktopPreferences {
	return DesktopPreferences{
		AgentCLIUpdateCheckEnabled:                  DefaultDesktopAgentCLIUpdateCheckEnabled,
		AgentRuntimeKeepAliveEnabled:                DefaultDesktopAgentRuntimeKeepAliveEnabled,
		AgentRuntimeIdleMinutes:                     DefaultDesktopAgentRuntimeIdleMinutes,
		AgentRuntimeMaxResident:                     DefaultDesktopAgentRuntimeMaxResident,
		AgentComposerDefaultsByProvider:             map[string]AgentComposerDefaults{},
		AgentComposerDefaultsByAgentTarget:          map[string]AgentComposerDefaults{},
		AgentGUIConversationRailCollapsedByProvider: map[string]bool{},
		AgentSessionLaunchModesByWorkspace:          map[string]map[string]string{},
		AgentConversationDetailMode:                 DefaultDesktopAgentConversationDetailMode,
		AgentDockLayout:                             DefaultDesktopAgentDockLayout,
		AppCatalogChannel:                           DefaultDesktopAppCatalogChannel,
		BrowserUseConnectionMode:                    DefaultDesktopBrowserUseConnectionMode,
		DefaultAgentProvider:                        DefaultDesktopDefaultAgentProvider,
		DockIconStyle:                               DefaultDesktopDockIconStyle,
		DockPlacement:                               DefaultDesktopDockPlacement,
		DeletedAgentConversationRetentionDays:       DefaultDeletedAgentConversationRetentionDays,
		FeatureFlags:                                map[string]bool{},
		FileDefaultOpenersByExtension: map[string]string{
			"htm":   "appBrowser",
			"html":  "appBrowser",
			"shtml": "appBrowser",
			"xhtml": "appBrowser",
		},
		Initialized:                  false,
		Locale:                       DefaultDesktopLocale,
		MinimizeAnimation:            DefaultDesktopMinimizeAnimation,
		SleepPreventionMode:          DefaultDesktopSleepPreventionMode,
		ShowAppDeveloperSources:      DefaultDesktopShowAppDeveloperSources,
		ThemeSource:                  DefaultDesktopThemeSource,
		UpdateChannel:                DefaultDesktopUpdateChannel,
		UpdatePolicy:                 DefaultDesktopUpdatePolicy,
		WindowSnappingEnabled:        DefaultDesktopWindowSnappingEnabled,
		WindowSnappingShortcutPreset: DefaultDesktopWindowSnappingShortcut,
		WorkbenchShortcuts:           DesktopWorkbenchShortcuts{},
	}
}

// NormalizeDesktopAgentRuntimeIdleMinutes 把越界值收回默认，而不是夹到边界：
// 夹边界会把一个明显写错的值（比如 999999）变成一个看起来合理的值，
// 用户再也不知道自己填错过。
func NormalizeDesktopAgentRuntimeIdleMinutes(value int) int {
	if IsDesktopAgentRuntimeIdleMinutes(value) {
		return value
	}
	return DefaultDesktopAgentRuntimeIdleMinutes
}

// IsDesktopAgentRuntimeIdleMinutes：0 = 永不回收，1..1440 = 空闲这么多分钟后回收。
func IsDesktopAgentRuntimeIdleMinutes(value int) bool {
	return value >= 0 && value <= MaxDesktopAgentRuntimeIdleMinutes
}

// NormalizeDesktopAgentRuntimeMaxResident 同样把越界值收回默认，理由见上。
func NormalizeDesktopAgentRuntimeMaxResident(value int) int {
	if IsDesktopAgentRuntimeMaxResident(value) {
		return value
	}
	return DefaultDesktopAgentRuntimeMaxResident
}

// IsDesktopAgentRuntimeMaxResident：0 = 不限条数，1..100 = 最多常驻这么多条。
func IsDesktopAgentRuntimeMaxResident(value int) bool {
	return value >= 0 && value <= MaxDesktopAgentRuntimeMaxResident
}

func NormalizeDeletedAgentConversationRetentionDays(value int) int {
	if IsDeletedAgentConversationRetentionDays(value) {
		return value
	}
	return DefaultDeletedAgentConversationRetentionDays
}

func IsDeletedAgentConversationRetentionDays(value int) bool {
	return value == 15 || value == 30
}

func NormalizeDesktopAgentDockLayout(value string) string {
	normalized := strings.TrimSpace(value)
	if IsDesktopAgentDockLayout(normalized) {
		return normalized
	}
	return DefaultDesktopAgentDockLayout
}

func IsDesktopAgentDockLayout(value string) bool {
	switch value {
	case DesktopAgentDockLayoutLegacySplit, DesktopAgentDockLayoutUnified:
		return true
	default:
		return false
	}
}

func NormalizeDesktopAgentConversationDetailMode(value string) string {
	normalized := strings.TrimSpace(value)
	if IsDesktopAgentConversationDetailMode(normalized) {
		return normalized
	}
	return DefaultDesktopAgentConversationDetailMode
}

func IsDesktopAgentConversationDetailMode(value string) bool {
	switch value {
	case "coding", "general":
		return true
	default:
		return false
	}
}

func IsDesktopDefaultAgentProvider(value string) bool {
	descriptor, ok := providerregistry.Find(value)
	return ok && descriptor.Desktop.DefaultProviderEligible
}

func IsDesktopAppCatalogChannel(value string) bool {
	switch value {
	case "production", "staging":
		return true
	default:
		return false
	}
}

func IsDesktopFileDefaultOpener(value string) bool {
	switch value {
	case "appBrowser", "defaultBrowser", "fileViewer", "system":
		return true
	default:
		return false
	}
}

func NormalizeDesktopFileExtension(value string) string {
	normalized := strings.TrimLeft(strings.ToLower(strings.TrimSpace(value)), ".")
	if normalized == "" || len(normalized) > 32 {
		return ""
	}
	for index, char := range normalized {
		if (char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') {
			continue
		}
		if index > 0 && (char == '_' || char == '-') {
			continue
		}
		return ""
	}
	return normalized
}

func IsDesktopDockIconStyle(value string) bool {
	switch value {
	case "default", "flat":
		return true
	default:
		return false
	}
}

func IsDesktopDockPlacement(value string) bool {
	switch value {
	case "bottom", "left":
		return true
	default:
		return false
	}
}

func IsDesktopMinimizeAnimation(value string) bool {
	switch value {
	case "scale", "genie", "off":
		return true
	default:
		return false
	}
}

func IsDesktopWindowSnappingShortcutPreset(value string) bool {
	switch value {
	case "commandArrows", "commandShiftArrows":
		return true
	default:
		return false
	}
}

func IsDesktopLocale(value string) bool {
	switch value {
	case "en", "zh-CN":
		return true
	default:
		return false
	}
}

func IsDesktopThemeSource(value string) bool {
	switch value {
	case "system", "dark", "light":
		return true
	default:
		return false
	}
}

func IsDesktopSleepPreventionMode(value string) bool {
	switch value {
	case "never", "whileAgentRunning", "always":
		return true
	default:
		return false
	}
}

func IsDesktopBrowserUseConnectionMode(value string) bool {
	switch value {
	case "isolated", "autoConnect":
		return true
	default:
		return false
	}
}

func IsDesktopUpdateChannel(value string) bool {
	switch value {
	case "stable", "rc":
		return true
	default:
		return false
	}
}

func IsDesktopUpdatePolicy(value string) bool {
	switch value {
	case "off", "prompt", "auto":
		return true
	default:
		return false
	}
}

func NormalizeDesktopShortcutBinding(value string) string {
	normalized := strings.TrimSpace(value)
	if len(normalized) > 80 {
		return ""
	}
	return normalized
}

func NormalizeDesktopFeatureFlags(value map[string]bool) map[string]bool {
	result := make(map[string]bool, len(value))
	for key, enabled := range value {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" || len(trimmed) > 128 {
			continue
		}
		result[trimmed] = enabled
	}
	return result
}

func NormalizeDesktopWorkbenchShortcuts(value DesktopWorkbenchShortcuts) DesktopWorkbenchShortcuts {
	return DesktopWorkbenchShortcuts{
		NewAgentConversation: NormalizeDesktopShortcutBinding(value.NewAgentConversation),
		NewSameTypeWindow:    NormalizeDesktopShortcutBinding(value.NewSameTypeWindow),
		CaptureScreenshot:    NormalizeDesktopShortcutBinding(value.CaptureScreenshot),
	}
}
