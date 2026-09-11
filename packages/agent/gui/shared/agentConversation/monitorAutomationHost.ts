// 会话顶部「我起了 N 个监控器」胶囊（补丁 0135）的宿主端口注册口，
// 形状照 0122 的 sessionLivenessHost。
//
// 背景：补丁 0134 做了一条胶囊，数的是 `Monitor` 工具起的后台子会话。可
// DinTalDock 的 Claude 运行时把 `Monitor` 写进了 disallowedTools
//（`claude_provider_meta.go`，上游自己的决定），那条胶囊在这个产品里永远不会亮。
//
// 这个产品里真正能用的「监控」只有一种：**自动化项 preset=monitor**，Grok 与
// Claude 都经 MCP 的 `automation_create` 起。它和 Monitor 工具是两码事：
//
//   Monitor 工具      一直挂着的子进程，有「已经跑了多久」
//   监控自动化        cliagent-backend 里的一条记录 + 一个闹钟，平时**零进程**，
//                     只有「下次几点响」（心跳最细 1 分钟）
//
// 所以这条胶囊显示的是**下次心跳**，不是已运行时长——后者对它不成立，硬写会是假的。
//
// 自动化项住在 cliagent-backend，会话投影里没有它，iframe 也不直连后端：
// apps/desktop 在嵌入 DinTalDock 时注册一份走 tutti-host-request 桥的实现；
// 未注册（普通 web / 老宿主）时这里返回 null，胶囊整个不显示。

export interface HostMonitorAutomation {
  /** 自动化项 id。停用时原样递回宿主。 */
  id: string;
  /** 人话名字，例如「盯 grok-8014」。 */
  name: string;
  /** 被盯的那条会话（task id 或配对别名）。宿主可能给空串。 */
  peerTarget: string;
  /**
   * 下一次心跳的时刻（毫秒）。宿主解析不出时给 `null`，
   * 界面就只显示条数，不编一个时间出来。
   */
  nextFireAtUnixMs: number | null;
}

export interface MonitorAutomationHost {
  /**
   * 批量问一组会话「各自起过哪些还启用着的监控自动化」。
   *
   * 归属与「是不是 monitor」都由宿主过滤：口径只放一处，iframe 不自己筛，
   * 也拿不到别人会话的自动化项。
   */
  queryMonitorAutomations(input: { creatorSessionIds: string[] }): Promise<{
    monitors: HostMonitorAutomation[];
  }>;
  /**
   * 停用（或重新启用）一批监控自动化。
   *
   * 可选：宿主只实现了查询时，胶囊照常显示、只是不给「停用」按钮。
   * 宿主会再核对一次归属与 preset，回的 `updated` 是真正改到的条数。
   */
  setMonitorAutomationsEnabled?(input: {
    ids: string[];
    enabled: boolean;
  }): Promise<{ updated: number }>;
}

let registeredHost: MonitorAutomationHost | null = null;

export function registerMonitorAutomationHost(
  host: MonitorAutomationHost
): () => void {
  registeredHost = host;
  return () => {
    // 只有还是自己那份实现时才摘掉，避免两次安装时后者被前者的 dispose 误摘。
    if (registeredHost === host) {
      registeredHost = null;
    }
  };
}

export function monitorAutomationHost(): MonitorAutomationHost | null {
  return registeredHost;
}

/** 一次最多问多少个会话号（与宿主侧契约一致）。 */
export const MONITOR_AUTOMATIONS_MAX_IDS = 200;
