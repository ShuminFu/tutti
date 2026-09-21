export {
  projectCanonicalAgentGUIConversationSummaries,
  projectCanonicalAgentGUIConversationSummariesFromState,
  type AgentConversationRailStatus,
  type AgentConversationRailSummary,
  type AgentGUIConsumerSessions
} from "./shared/agentGUIConversationSummaryProjection.ts";

// 侧栏会话配对（补丁 0103）：宿主端口注册口 + 纯模型，挂在既有的
// conversation-rail-projection 子路径上，不新增 package exports 入口。
export {
  conversationRailPeerPairingHost,
  notifyConversationRailPeerPairsChanged,
  registerConversationRailPeerPairingHost,
  subscribeConversationRailPeerPairsChanged,
  type ConversationRailCreatePeerPairInput,
  type ConversationRailCreatePeerPairResult,
  type ConversationRailCommitPairKickoffInput,
  type ConversationRailPairKickoffInput,
  type ConversationRailPeerPairingHost,
  type ConversationRailSetPeerPairModeInput
} from "./agent-gui/agentGuiNode/model/conversationRailPeerPairingHost.ts";
export {
  applyConversationRailPeerPairAdjacency,
  conversationRailPeerAliasPreview,
  conversationRailPeerPairBadgeMode,
  conversationRailPeerPairCount,
  conversationRailPeerPairIndex,
  conversationRailPeerPairLinks,
  isConversationRailPeerClosedStatus,
  isConversationRailPeerPairable,
  type ConversationRailPeerPair,
  type ConversationRailPeerPairBadgeMode,
  type ConversationRailPeerPairEndpoint,
  type ConversationRailPeerPairIndex,
  type ConversationRailPeerPairKickoffState,
  type ConversationRailPeerPairLink,
  type ConversationRailPeerPairMode
} from "./agent-gui/agentGuiNode/model/conversationRailPeerPairing.ts";

// 分栏配对（补丁 0108）：宿主端口注册口 + 状态机，同样挂在这个子路径上。
export {
  conversationRailSplitHost,
  registerConversationRailSplitHost,
  type ConversationRailSplitDragSession,
  type ConversationRailSplitHost,
  type ConversationRailSplitPoint
} from "./agent-gui/agentGuiNode/model/conversationRailSplitHost.ts";
export {
  createEmptySplitLayout,
  isSplitLayoutSplit,
  loadSplitLayout,
  parseSplitLayout,
  reduceSplitLayout,
  saveSplitLayout,
  serializeSplitLayout,
  splitLayoutActiveConversationId,
  splitLayoutStorageKey,
  type SplitLayoutEvent,
  type SplitLayoutState,
  type SplitSide
} from "./agent-gui/agentGuiNode/model/agentGuiSplitLayout.ts";

// 配对请求就地审批（补丁 0116）：宿主端口注册口，同样挂在这个子路径上。
export {
  peerPairRequestHost,
  registerPeerPairRequestHost,
  type PeerPairRequestDecision,
  type PeerPairRequestHost,
  type PeerPairRequestSide,
  type PeerPairRequestView
} from "./shared/agentConversation/peerPairRequestHost.ts";

// 会话栏圆点按宿主任务行终态隐藏（补丁 0122）：宿主端口注册口挂同一个子路径。
export {
  registerSessionLivenessHost,
  sessionLivenessHost,
  SESSION_LIVENESS_MAX_IDS,
  type HostSessionLivenessEntry,
  type HostSessionLivenessState,
  type SessionLivenessHost
} from "./shared/agentConversation/sessionLivenessHost.ts";

// 分栏结对模式（peer-pair-mode 票 04/05）：作曲区宿主扩展口（上方附加行 + 提交前拼块），
// 同样挂在这个子路径上，apps/desktop 的分栏层据此注册，不新增 package exports 入口。
export {
  agentComposerHostExtension,
  agentPromptSubmitText,
  isAgentPromptSubmitPreparable,
  prefixAgentPromptContent,
  registerAgentComposerHostExtension,
  runPreparedAgentPromptSubmit,
  subscribeAgentComposerHostExtension,
  type AgentComposerHostExtension,
  type AgentComposerSubmitPreparationInput,
  type AgentPromptSubmitPreparation,
  type AgentPromptSubmitReceipt,
  type RunPreparedAgentPromptSubmitInput
} from "./shared/agentConversation/agentComposerHostExtension.ts";
