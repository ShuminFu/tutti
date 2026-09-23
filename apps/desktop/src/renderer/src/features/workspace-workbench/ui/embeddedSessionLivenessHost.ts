// 会话栏圆点按宿主任务行终态隐藏（补丁 0122）的宿主端实现：
// 把 gui 包声明的端口接到 tutti-host-request 桥。形状照 0117 的
// embeddedPeerPairRequestHost；未注册时会话栏退回 0119 的纯 Tutti 判据。
import { registerSessionLivenessHost } from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  HostBridgeUnavailableError,
  requestHostSessionAttach,
  requestHostSessionLiveness
} from "../../../platform/desktop/web/webHostBridgeClient.ts";

// 仅能力确实不存在时归一成 "unsupported"，让 gui 永久停拍；超时仍可重试。
function normalizeBridgeError(error: unknown): never {
  if (error instanceof HostBridgeUnavailableError) {
    const code = (error as { code?: unknown }).code;
    if (
      code === "host_capability_unsupported" ||
      code === "host_bridge_unavailable"
    ) {
      throw new Error("unsupported");
    }
    // A timeout says nothing about provider liveness. Keep polling so a
    // temporary busy host can recover without reloading the panel.
    throw error;
  }
  if (error instanceof Error && error.message === "unsupported") {
    throw new Error("unsupported");
  }
  throw error;
}

export function installEmbeddedSessionLivenessHost(): () => void {
  return registerSessionLivenessHost({
    querySessionLiveness: (input) =>
      requestHostSessionLiveness(input).catch(normalizeBridgeError),
    // 补挂（补丁 0124）。老宿主只实现了 sessionLiveness，这里同样归一成
    // "unsupported"，gui 侧收到就永久停止补挂、圆点行为逐字回到 0122。
    attachSession: (input) =>
      requestHostSessionAttach(input).catch(normalizeBridgeError)
  });
}
