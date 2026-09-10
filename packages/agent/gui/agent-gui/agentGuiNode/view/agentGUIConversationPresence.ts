import type { AgentGUIConversationSummary } from "../model/agentGuiConversationTypes";
import type { HostSessionLivenessState } from "../../../shared/agentConversation/sessionLivenessHost";

/**
 * 会话栏行图标右下角那颗「在线状态」圆点的取值。
 *
 * - `"working"`（蓝）：这一轮还开着，agent 正在干活或正等用户答话。
 * - `"idle"`（绿）：会话还活着，但当前没有进行中的轮次。
 * - `null`：会话已结束，不画圆点。
 */
export type AgentGUIConversationPresence = "working" | "idle" | null;

/**
 * 由会话摘要（外加宿主给的死活）推出圆点状态。判定顺序是有意的：
 *
 * 0. **先看宿主怎么说**（补丁 0122）。`hostLiveness === "closed"` 一票否决，
 *    压过下面所有判据 —— 包括「这一轮在跑」。rndmaster 派工的会话被后端重启
 *    打成 failed 时，Tutti 侧的快照会永远停在 `working`（那一轮再也不会收尾），
 *    正是「后端重启中断」那个状态；此时画蓝点等于骗人。
 *    `"live"` / `"unknown"` / 没数据都当没这回事，往下走 0119 的判据。
 *
 * 1. **再看结束**。`endedAtUnixMs > 0` 一票否决 —— 已结束的会话既不「在线」
 *    也不「工作中」，哪怕最后一次快照里的 `status` 还停在 `working`
 *    （后端先落 ended、状态字段慢一拍是常态）。
 * 2. **最后看这一轮开没开**。`status === "working"` 或 `status === "waiting"`
 *    或 `activeTurn` 还在 → 蓝点。
 *
 *    `waiting` 归到「工作中」：那是 agent 反过来问用户话，**这一轮并没有收尾**，
 *    对用户来说这条会话仍然是「有事在身」，画成绿色的「闲着」会误导。
 * 3. 其余（`ready` / `completed` / `failed` / `canceled` 且没有活跃轮次）→ 绿点。
 *
 * 纯函数，无副作用：会话栏行渲染与单测共用同一份判据。
 */
export function agentGUIConversationPresence(
  conversation: Pick<
    AgentGUIConversationSummary,
    "status" | "activeTurn" | "endedAtUnixMs"
  >,
  hostLiveness?: HostSessionLivenessState | null
): AgentGUIConversationPresence {
  // 0. 宿主说这条已经关了：不画点（补丁 0122）。
  if (hostLiveness === "closed") {
    return null;
  }
  // 1. 已结束：不画点。用 `?? 0` 把 undefined/null 一起收敛成「没结束」。
  if ((conversation.endedAtUnixMs ?? 0) > 0) {
    return null;
  }
  // 2. 这一轮还开着：蓝点。
  if (
    conversation.status === "working" ||
    conversation.status === "waiting" ||
    Boolean(conversation.activeTurn)
  ) {
    return "working";
  }
  // 3. 活着但闲着：绿点。
  return "idle";
}
