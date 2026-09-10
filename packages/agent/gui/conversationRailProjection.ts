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
