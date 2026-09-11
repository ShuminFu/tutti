// 会话顶部「我起了 N 个监控器」胶囊（补丁 0135）的宿主端实现：
// 把 gui 包声明的端口接到 tutti-host-request 桥。形状照 0122 的
// embeddedSessionLivenessHost；未注册时胶囊整个不显示。
//
// 为什么要走宿主：这里说的「监控器」是 cliagent-backend 的自动化项
// preset=monitor，不是 Claude 的 `Monitor` 工具（后者在 DinTalDock 里被
// disallowedTools 禁掉了）。自动化项不在会话投影里，iframe 也不直连后端。
import { registerMonitorAutomationHost } from "@tutti-os/agent-gui/monitor-automation-host";
import {
  HostBridgeUnavailableError,
  requestHostMonitorAutomations,
  requestHostSetMonitorAutomationsEnabled
} from "../../../platform/desktop/web/webHostBridgeClient.ts";

// 桥不可用（未嵌入 / 宿主没实现这个能力 / 超时）统一归一成 "unsupported"，
// gui 侧只认这一个词就永久停拍。
function normalizeBridgeError(error: unknown): never {
  if (error instanceof HostBridgeUnavailableError) {
    throw new Error("unsupported");
  }
  if (error instanceof Error && error.message === "unsupported") {
    throw new Error("unsupported");
  }
  throw error;
}

export function installEmbeddedMonitorAutomationHost(): () => void {
  return registerMonitorAutomationHost({
    queryMonitorAutomations: (input) =>
      requestHostMonitorAutomations(input).catch(normalizeBridgeError),
    // 停用那一档单独归一：老宿主可能只实现了查询，那时胶囊照常显示、
    // 只是点不出「停用」按钮（gui 侧 canStop 会是 false）。
    setMonitorAutomationsEnabled: (input) =>
      requestHostSetMonitorAutomationsEnabled(input).catch(normalizeBridgeError)
  });
}
