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
}

export interface SessionLivenessHost {
  /**
   * 批量问一组会话号的死活。宿主保证**每个问到的 id 都在结果里**，
   * 认不出的回 `"unknown"`，调用方不必自己补齐。
   */
  querySessionLiveness(input: { agentSessionIds: string[] }): Promise<{
    sessions: Record<string, HostSessionLivenessEntry>;
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
