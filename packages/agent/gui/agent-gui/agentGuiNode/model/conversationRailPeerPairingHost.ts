// 侧栏配对的宿主端口注册口（形状照 0099 的 embeddedHostCreatedSessionOpener）。
//
// iframe 不直连 cliagent-backend：apps/desktop 在嵌入 DinTalDock 时注册一份走
// tutti-host-request 桥的实现；未注册（普通 web / 老宿主）时整组配对 UI 不出现。
import type { ConversationRailPeerPair } from "./conversationRailPeerPairing";

export interface ConversationRailCreatePeerPairInput {
  /** 发起方 = 当前右键那条的 Tutti 会话号。 */
  from: string;
  /** 被标记那条的 Tutti 会话号。 */
  to: string;
  /** 别名由宿主算，这里保留字段传空串。 */
  aliasForFrom: string;
  aliasForTo: string;
  /** 目标已结束时为 true：后端先重开该会话再配对。 */
  relaunchClosed: boolean;
}

export interface ConversationRailCreatePeerPairResult {
  pairId: string;
  relaunchedSessionId?: string;
  relaunchedTaskId?: string;
}

export interface ConversationRailPeerPairingHost {
  createPeerPair(
    input: ConversationRailCreatePeerPairInput
  ): Promise<ConversationRailCreatePeerPairResult>;
  deletePeerPair(input: { pairId: string; taskId: string }): Promise<unknown>;
  listPeerPairs(): Promise<{ pairs: ConversationRailPeerPair[] }>;
}

let registeredHost: ConversationRailPeerPairingHost | null = null;

export function registerConversationRailPeerPairingHost(
  host: ConversationRailPeerPairingHost
): () => void {
  registeredHost = host;
  return () => {
    if (registeredHost === host) {
      registeredHost = null;
    }
  };
}

export function conversationRailPeerPairingHost(): ConversationRailPeerPairingHost | null {
  return registeredHost;
}
