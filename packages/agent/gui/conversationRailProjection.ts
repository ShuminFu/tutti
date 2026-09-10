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
  registerConversationRailPeerPairingHost,
  type ConversationRailCreatePeerPairInput,
  type ConversationRailCreatePeerPairResult,
  type ConversationRailPeerPairingHost
} from "./agent-gui/agentGuiNode/model/conversationRailPeerPairingHost.ts";
export {
  applyConversationRailPeerPairAdjacency,
  conversationRailPeerAliasPreview,
  conversationRailPeerPairCount,
  conversationRailPeerPairIndex,
  conversationRailPeerPairLinks,
  isConversationRailPeerClosedStatus,
  isConversationRailPeerPairable,
  type ConversationRailPeerPair,
  type ConversationRailPeerPairEndpoint,
  type ConversationRailPeerPairIndex,
  type ConversationRailPeerPairLink
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
