// 会话栏圆点按宿主任务行终态隐藏（补丁 0122）的宿主端实现：
// 把 gui 包声明的端口接到 tutti-host-request 桥。形状照 0117 的
// embeddedPeerPairRequestHost；未注册时会话栏退回 0119 的纯 Tutti 判据。
import { registerSessionLivenessHost } from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  HostBridgeUnavailableError,
  requestHostSessionLiveness
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

export function installEmbeddedSessionLivenessHost(): () => void {
  return registerSessionLivenessHost({
    querySessionLiveness: (input) =>
      requestHostSessionLiveness(input).catch(normalizeBridgeError)
  });
}
