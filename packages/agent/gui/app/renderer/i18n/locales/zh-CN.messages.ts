export const zhCNMessages = {
  agentLaunchFailed: "Agent 启动失败：{{message}}",
  agentResumeFailed: "Agent 继续失败：{{message}}",
  agentProviderSessionNotFound:
    "这条会话历史仍可查看，但底层 Provider 会话已经无法恢复。",
  agentTargetRemoved: "该 agent 不存在或已被移除，历史会话记录仍可查看。",
  agentResumeSessionNotLocal:
    "这个会话没法在当前设备里直接恢复，你可以在新会话里 @这段对话，接着继续聊。",
  agentImportedSessionResumeUnavailable:
    "这段对话已导入成功，新开会话并 @ 这段对话，接着继续聊。",
  agentSessionReconnecting: "正在重新连接 Agent 会话…",
  agentSettingsRequireNewSession: "为了保留上下文，这个模型只能在新会话中使用",
  agentConfigDependencyUnavailable:
    "{{provider}} 的配置引用了当前不可用的文件，请检查本机配置后重试",
  agentSessionTitleTooLong: "会话标题不能超过 {{maxCharacters}} 个字符。",
  agentSessionTitleTooLongWithoutLimit: "会话标题过长。",
  agentPermissionModeAppliesNextTurn: "权限模式将从你的下一条消息开始生效。",
  agentComposerDefaultsNotSaved: "{{fields}}未能保存为默认值：{{reason}}",
  agentComposerDefaultsFieldCodexSaverMode: "省流模式",
  agentComposerDefaultsFieldModel: "模型",
  agentComposerDefaultsFieldPermissionModeId: "权限模式",
  agentComposerDefaultsFieldReasoningEffort: "推理强度",
  agentComposerDefaultsFieldSpeed: "速度",
  agentComposerDefaultsFieldSeparator: "、",
  agentComposerDefaultsReasonInvalidValue: "该值不被接受",
  agentComposerDefaultsReasonUnsupportedValue: "当前模型或 Provider 不支持该值",
  agentComposerDefaultsReasonNotConfigurable: "该 Provider 不允许在此设置",
  agentComposerDefaultsReasonUnsupportedField: "该 Provider 不支持这项设置",
  agentComposerDefaultsReasonInternalError: "改动未能写入存储",
  agentComposerDefaultsReasonUnknown: "当前 Provider 拒绝了该改动",
  agentFastModeFallbackToStandard: "当前模型不支持快速模式，已使用标准模式。",
  agentModelFallback: "原模型已下架，已切换到 {{model}}。",
  agentThisSessionMentionLabel: "本 session",
  terminalLaunchFailed: "终端启动失败：{{message}}",
  fallbackTerminalFailed: "兜底终端启动也失败了：{{message}}",
  agentPromptRequired: "Agent 提示词不能为空。",
  resumeSessionMissing: "该 Agent 还没有已验证的 resumeSessionId。",
  noTerminalSlotNearby:
    "当前视图附近没有可用空位，请先移动或关闭部分终端窗口。",
  noWindowSlotOnRight: "当前 Agent 右侧没有可用空位，请先移动或关闭部分窗口。",
  noWindowSlotNearby: "当前视图附近没有可用空位，请先移动或关闭部分窗口。",
  agentManageSyncSuccess: "同步成功",
  agentManageInstallSuccess: "安装成功"
} as const;
