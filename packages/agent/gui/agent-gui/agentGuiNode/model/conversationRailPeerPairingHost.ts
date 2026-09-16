// 侧栏配对的宿主端口注册口（形状照 0099 的 embeddedHostCreatedSessionOpener）。
//
// iframe 不直连 cliagent-backend：apps/desktop 在嵌入 DinTalDock 时注册一份走
// tutti-host-request 桥的实现；未注册（普通 web / 老宿主）时整组配对 UI 不出现。
import type {
  ConversationRailPeerPair,
  ConversationRailPeerPairMode
} from "./conversationRailPeerPairing";

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

/** 分栏结对模式（peer-pair-mode 接口契约）：设模式。 */
export interface ConversationRailSetPeerPairModeInput {
  pairId: string;
  mode: ConversationRailPeerPairMode;
  /** 结对模式下开发者那一侧的 task id（或 Tutti 会话号，后端归一）；solo 时不传。 */
  developerTaskId?: string;
}

/** 开工卡预览 / 提交共用的入参：发送栏的 task id（或会话号）+ 用户那句话。 */
export interface ConversationRailPairKickoffInput {
  pairId: string;
  senderTaskId: string;
  goal: string;
}

export interface ConversationRailCommitPairKickoffInput
  extends ConversationRailPairKickoffInput {
  /**
   * preview 时拍下的开发者（task id 或会话号，后端同样归一）。给了且与行里当前
   * developer 不一致 → 409 kickoff_roles_changed，不投递、保持 pending（契约补充评审 A）。
   */
  expectedDeveloperTaskId?: string;
}

export interface ConversationRailPeerPairingHost {
  createPeerPair(
    input: ConversationRailCreatePeerPairInput
  ): Promise<ConversationRailCreatePeerPairResult>;
  deletePeerPair(input: { pairId: string; taskId: string }): Promise<unknown>;
  listPeerPairs(): Promise<{ pairs: ConversationRailPeerPair[] }>;
  // 结对模式三件套（契约规定三个一组注册）。可选：只接了 0103 那三个能力的
  // 实现（单测桩、老宿主适配）照样成立；缺任一个分栏层都当「不支持」处理。
  setPeerPairMode?(
    input: ConversationRailSetPeerPairModeInput
  ): Promise<{ pair: ConversationRailPeerPair }>;
  /** 纯函数、不写库：返回拼在发送栏用户原文前面的 `<pair-kickoff>` 块。 */
  previewPairKickoff?(
    input: ConversationRailPairKickoffInput
  ): Promise<{ block: string }>;
  /**
   * 发送栏那句被接受之后调：给搭档投卡并记为 sent（幂等）。
   * `delivered: false`（可带 reason，例如搭档那张卡被环路闸 / 限流丢了）时行仍是
   * pending，调用方按「没投到」处理，下一句再带卡重试。
   */
  commitPairKickoff?(
    input: ConversationRailCommitPairKickoffInput
  ): Promise<{
    pair: ConversationRailPeerPair;
    delivered: boolean;
    reason?: string;
  }>;
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
