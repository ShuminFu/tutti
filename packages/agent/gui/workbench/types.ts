/** Open runtime metadata; agentTargetId remains the launch identity. */
export type AgentGuiWorkbenchProvider = string;

export const agentGuiWorkbenchOpenSessionActivationType =
  "agent-gui:open-session";

export const agentGuiWorkbenchPrefillPromptActivationType =
  "agent-gui:prefill-prompt";

/**
 * 宿主要求这个窗口「回首页」：清掉当前会话，退回新建会话的落地页。
 * 和 prefill-prompt 的区别是它不带草稿、不换 provider/target——纯粹是
 * 「这个窗口现在不该再显示任何一条会话了」。分栏覆盖层的 ✕（单栏态）用它。
 */
export const agentGuiWorkbenchGoHomeActivationType = "agent-gui:go-home";

export interface AgentGuiWorkbenchOpenSessionComposerAppend {
  draftPrompt: string;
  focusComposer?: boolean;
}

export interface AgentGuiWorkbenchOpenSessionPayload {
  agentSessionId: string;
  composerAppend?: AgentGuiWorkbenchOpenSessionComposerAppend;
}

export interface AgentGuiWorkbenchPrefillPromptPayload {
  agentTargetId?: string | null;
  autoSubmit?: boolean;
  draftPrompt: string;
  model?: string | null;
  modelPlanId?: string | null;
  provider?: AgentGuiWorkbenchProvider;
  userProjectPath?: string | null;
}

export interface AgentGuiWorkbenchComposerOverrides {
  model?: string | null;
  modelPlanId?: string | null;
  permissionModeId?: string | null;
  planMode?: boolean;
  reasoningEffort?: string | null;
  speed?: string | null;
}

export type AgentGuiWorkbenchComposerOverridesByProvider = Partial<
  Record<AgentGuiWorkbenchProvider, AgentGuiWorkbenchComposerOverrides | null>
>;

export type AgentGuiWorkbenchComposerOverridesByAgentTargetId = Record<
  string,
  AgentGuiWorkbenchComposerOverrides | null
>;

export interface AgentGuiWorkbenchNodeState {
  agentTargetId?: string | null;
  composerOverrides?: AgentGuiWorkbenchComposerOverrides | null;
  composerOverridesByAgentTargetId?: AgentGuiWorkbenchComposerOverridesByAgentTargetId | null;
  composerOverridesByProvider?: AgentGuiWorkbenchComposerOverridesByProvider | null;
  conversationCount?: number | null;
  conversationRailCollapsed?: boolean | null;
  conversationRailWidthPx?: number | null;
  lastActiveAgentSessionId: string | null;
  lastActiveAgentSessionIdByAgentTargetId?: Record<string, string> | null;
  provider: AgentGuiWorkbenchProvider;
}

export interface AgentGuiWorkbenchState {
  agentTargetId?: string | null;
  conversationRailCollapsed?: boolean | null;
  conversationRailWidthPx?: number | null;
  lastActiveAgentSessionId: string | null;
  lastActiveAgentSessionIdByAgentTargetId?: Record<string, string> | null;
}

export interface AgentGuiWorkbenchWorkspaceState {
  workspaceId: string;
}
