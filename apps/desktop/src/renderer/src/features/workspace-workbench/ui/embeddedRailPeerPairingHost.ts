// 侧栏配对（补丁 0103）的宿主端实现：把 gui 包声明的端口接到 tutti-host-request 桥。
//
// 注册口在 gui 包里（形状照 0099 的 embeddedHostCreatedSessionOpener），
// 因为 packages/agent/gui 不能反向依赖 apps/desktop 的 webHostBridgeClient。
// 未注册（普通 web / 老宿主）时侧栏整组配对 UI 不出现。
import { registerConversationRailPeerPairingHost } from "@tutti-os/agent-gui/conversation-rail-projection";
import {
  HostBridgeUnavailableError,
  requestHostCommitPairKickoff,
  requestHostCreatePeerPair,
  requestHostDeletePeerPair,
  requestHostListPeerPairs,
  requestHostPreviewPairKickoff,
  requestHostSetPeerPairMode
} from "../../../platform/desktop/web/webHostBridgeClient.ts";

// 桥不可用（未嵌入 / 宿主没实现这个能力 / 超时）统一归一成 "unsupported"，
// gui 侧只认这一个词就把整组 UI 收起来，不会把后端的中文拒绝文案误判成不支持。
function normalizeBridgeError(error: unknown): never {
  if (error instanceof HostBridgeUnavailableError) {
    throw new Error("unsupported");
  }
  throw error;
}

/**
 * 结对模式三件套专用（评审补充 1）：只有「宿主没注册这个能力」才算 unsupported——
 * 分栏层会据此**永久**收起单选、不再拦截第一句。超时只是这一次没等到回话
 * （宿主忙 / 后端慢），按普通失败提示，下一次照常再试；commit 超时时后端可能其实
 * 已经投了卡，调用方随后会重拉配对表对齐。
 */
export function normalizePairModeBridgeError(error: unknown): never {
  if (error instanceof HostBridgeUnavailableError) {
    const code = (error as { code?: unknown }).code;
    // 没嵌入宿主（这个安装只在嵌入时跑，正常到不了）与能力未注册一样，都是「这里根本没有」。
    if (code === "host_capability_unsupported" || code === "host_bridge_unavailable") {
      throw new Error("unsupported");
    }
    if (code === "host_request_timeout") {
      throw new Error("宿主响应超时，请稍后再试");
    }
    throw new Error(error.message);
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
      requestHostListPeerPairs().catch(normalizeBridgeError),
    // 结对模式三件套（peer-pair-mode）：只有宿主没注册时才归一成 unsupported（分栏层据此
    // 把整排单选收起来）；超时是普通失败，见 normalizePairModeBridgeError。
    commitPairKickoff: (input) =>
      requestHostCommitPairKickoff(input).catch(normalizePairModeBridgeError),
    previewPairKickoff: (input) =>
      requestHostPreviewPairKickoff(input).catch(normalizePairModeBridgeError),
    setPeerPairMode: (input) =>
      requestHostSetPeerPairMode(input).catch(normalizePairModeBridgeError)
  });
}
