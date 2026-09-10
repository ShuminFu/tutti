// 侧栏配对（补丁 0103）的宿主端实现：把 gui 包声明的端口接到 tutti-host-request 桥。
//
// 注册口在 gui 包里（形状照 0099 的 embeddedHostCreatedSessionOpener），
// 因为 packages/agent/gui 不能反向依赖 apps/desktop 的 webHostBridgeClient。
// 未注册（普通 web / 老宿主）时侧栏整组配对 UI 不出现。
import { registerConversationRailPeerPairingHost } from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  HostBridgeUnavailableError,
  requestHostCreatePeerPair,
  requestHostDeletePeerPair,
  requestHostListPeerPairs
} from "../../../platform/desktop/web/webHostBridgeClient.ts";

// 桥不可用（未嵌入 / 宿主没实现这个能力 / 超时）统一归一成 "unsupported"，
// gui 侧只认这一个词就把整组 UI 收起来，不会把后端的中文拒绝文案误判成不支持。
function normalizeBridgeError(error: unknown): never {
  if (error instanceof HostBridgeUnavailableError) {
    throw new Error("unsupported");
  }
  throw error;
}

export function installEmbeddedRailPeerPairingHost(): () => void {
  return registerConversationRailPeerPairingHost({
    createPeerPair: (input) =>
      requestHostCreatePeerPair(input).catch(normalizeBridgeError),
    deletePeerPair: (input) =>
      requestHostDeletePeerPair(input).catch(normalizeBridgeError),
    listPeerPairs: () =>
      requestHostListPeerPairs().catch(normalizeBridgeError)
  });
}
