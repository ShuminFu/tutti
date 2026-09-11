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

// 配对表变更广播（补丁 0130）。
//
// 为什么需要：同一个渲染进程里有**两份**配对表缓存 —— 侧栏右键菜单那份
// （useAgentGUIConversationRailPeerPairingState）和分栏栏头那份
// （embeddedSplitView 的 `pairs`）。各自只在自己写成功后 listPeerPairs，
// 于是拖进分栏配好的对，侧栏右键仍显示 `Unpair (0)`；反过来在侧栏解除，
// 分栏栏头也还停在「解除」。
//
// 这里只广播「配对表变了，自己再拉一次」这一件事，不搬数据：两边的刷新
// 各有自己的 unsupported / busy 语义，合并缓存要动的判据比这条通知多得多。
// 约定：只有**写成功之后**才 notify，刷新路径本身绝不 notify —— 否则
// 「A 刷新 → 通知 → B 刷新 → 通知 → …」会绕成环。
type ConversationRailPeerPairsListener = () => void;

const peerPairsListeners = new Set<ConversationRailPeerPairsListener>();

export function subscribeConversationRailPeerPairsChanged(
  listener: ConversationRailPeerPairsListener
): () => void {
  peerPairsListeners.add(listener);
  return () => {
    peerPairsListeners.delete(listener);
  };
}

export function notifyConversationRailPeerPairsChanged(): void {
  // 复制一份再遍历：订阅者在回调里退订（组件卸载）不影响这一轮派发。
  for (const listener of [...peerPairsListeners]) listener();
}
