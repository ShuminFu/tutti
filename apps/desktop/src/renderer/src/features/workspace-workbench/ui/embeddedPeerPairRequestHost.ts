// 配对请求就地审批（补丁 0116）的宿主端实现：把 gui 包声明的端口接到 tutti-host-request 桥。
// 形状照 0103 的 embeddedRailPeerPairingHost；未注册时对话里整张审批卡不出现。
import { registerPeerPairRequestHost } from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  HostBridgeUnavailableError,
  requestHostDecidePeerRequest,
  requestHostListPeerRequests
} from "../../../platform/desktop/web/webHostBridgeClient.ts";

// 桥不可用（未嵌入 / 宿主没实现这个能力 / 超时）统一归一成 "unsupported"，
// gui 侧只认这一个词就停止轮询，不会把后端的中文拒绝文案误判成不支持。
function normalizeBridgeError(error: unknown): never {
  if (error instanceof HostBridgeUnavailableError) {
    throw new Error("unsupported");
  }
  if (error instanceof Error && error.message === "unsupported") {
    throw new Error("unsupported");
  }
  throw error;
}

export function installEmbeddedPeerPairRequestHost(): () => void {
  return registerPeerPairRequestHost({
    listPendingPeerPairRequests: (input) =>
      requestHostListPeerRequests(input).catch(normalizeBridgeError),
    decidePeerPairRequest: (input) =>
      requestHostDecidePeerRequest(input).catch(normalizeBridgeError)
  });
}
