// 会话栏圆点按宿主任务行终态隐藏（补丁 0122）的宿主端口注册口
//（形状照 0117 的 peerPairRequestHost）。
//
// 背景：0119 给会话栏每行的 provider 图标加了在线圆点，「不画点」这一档靠
// `endedAtUnixMs > 0` 判定。但 tuttid 从来不写这个字段（本机 263/263 条会话
// 都是 0），于是「已关闭」这一档在真机上永远不出现。产品决定：**关没关由
// rndmaster 宿主说了算** —— 宿主那边有 `cli_agent_tasks` 的任务行状态，
// 后端重启打成 failed、用户主动关闭都记在那里。
//
// iframe 不直连 cliagent-backend：apps/desktop 在嵌入 DinTalDock 时注册一份
// 走 tutti-host-request 桥的实现；未注册（普通 web / 老宿主）时这里返回
// null，会话栏退回 0119 的纯 Tutti 判据（蓝 / 绿）。

/**
 * 一条会话在宿主那边的死活：
 *
 * - `"live"`：宿主任务行还活着，圆点照 0119 的判据画。
 * - `"closed"`：宿主任务行已终态（failed / completed / cancelled），**不画点**。
 * - `"unknown"`：宿主认不出这条会话（例如不是宿主派工的），不下结论，
 *   与 `"live"` 一样退回 0119 判据。
 */
export type HostSessionLivenessState = "live" | "closed" | "unknown";

export interface HostSessionLivenessEntry {
  state: HostSessionLivenessState;
  /** 宿主 `cli_agent_tasks` 的任务行 id，排查时用。 */
  taskId: string;
  /** 任务行原始状态串（running / failed / …），排查时用。 */
  status: string;
  /**
   * 「rndmaster 那边还有非终态任务行盯着这条会话吗」（补丁 0128）。
   *
   * 与 `state` 正交，两个字段回答的是两个不同的问题：
   * - `state` = tuttid 里这条会话有没有活的 ACP 进程 → **只给圆点用**；
   * - `attached` = rndmaster 有没有在记账 → **只给补挂用**。
   *
   * 老宿主不回这个字段，缺就当 `false`（没在盯）。
   */
  attached: boolean;
}

export interface SessionLivenessHost {
  /**
   * 批量问一组会话号的死活。宿主保证**每个问到的 id 都在结果里**，
   * 认不出的回 `"unknown"`，调用方不必自己补齐。
   */
  querySessionLiveness(input: { agentSessionIds: string[] }): Promise<{
    sessions: Record<string, HostSessionLivenessEntry>;
  }>;
  /**
   * 告诉宿主「用户又在这条会话里说话了」，让它把被后端重启打断的任务行重新挂上
   *（补丁 0124）。
   *
   * 为什么需要：宿主一重启，在跑的任务行全被打成 failed，而在 DinTalDock 里直接
   * 聊天**不经过宿主**、一行都不会新建 —— 那条会话从此在宿主眼里永远死着，
   * 圆点再也不出现（真机：会话 ac85f3bc 名下唯一一行 07:45 被打成 failed，
   * 10:41 还在正常对话，宿主仍回 closed）。
   *
   * 可选：老宿主只实现了 querySessionLiveness，缺这一项时补挂整档不启用。
   * 宿主保证幂等（已有活行原样返回，短时间内重复调不会多挂）。
   */
  attachSession?(input: { agentSessionId: string }): Promise<{
    state: HostSessionLivenessState;
    taskId: string;
    status: string;
    /** 补挂之后宿主有没有在盯（补丁 0128）；缺字段按 false 处理。 */
    attached: boolean;
  }>;
}

let registeredHost: SessionLivenessHost | null = null;

export function registerSessionLivenessHost(
  host: SessionLivenessHost
): () => void {
  registeredHost = host;
  return () => {
    // 只有还是自己那份实现时才摘掉，避免两次安装时后者被前者的 dispose 误摘。
    if (registeredHost === host) {
      registeredHost = null;
    }
  };
}

export function sessionLivenessHost(): SessionLivenessHost | null {
  return registeredHost;
}

/** 一次最多问多少个会话号（与宿主侧契约一致）。 */
export const SESSION_LIVENESS_MAX_IDS = 200;
