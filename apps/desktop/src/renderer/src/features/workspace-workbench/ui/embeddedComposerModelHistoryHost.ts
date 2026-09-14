// 模型收藏/最近使用（composer 菜单的星星与「最近」分组）的宿主端实现：把 gui 包声明的
// 端口接到 tutti-host-request 桥。形状照 0122 的 embeddedSessionLivenessHost。
//
// 为什么必须走宿主：这两个列表原先只写在 iframe 的 localStorage 里，而嵌入 DinTalDock
// 的 iframe origin 是**随机回环端口**，宿主每次重启/重装都会换一个 —— localStorage 跟着
// 换 origin，于是用户看到的是「收藏每隔一阵子自己没了」。宿主的记录按 target 落盘，
// 与端口、会话、版本都无关。未注册（普通 web / 老宿主）时 gui 侧退回 localStorage，
// 与补丁前逐字一致。
import {
  COMPOSER_MODEL_HISTORY_HOST_UNSUPPORTED_MESSAGE,
  registerComposerModelHistoryHost
} from "@tutti-os/agent-gui/composer-model-history";
import {
  HostBridgeUnavailableError,
  requestHostComposerModelHistory,
  requestHostUpdateComposerModelHistory
} from "../../../platform/desktop/web/webHostBridgeClient.ts";

// 「宿主根本没有这个能力」与「这次调用没成功」必须分开，否则一次超时就会让整个会话
// 永远退化成 localStorage：
// - 未嵌入（host_bridge_unavailable）或老宿主没实现（host_capability_unsupported）
//   → 归一成 "unsupported"，gui 侧不再问这个宿主，永久退回 localStorage 行为；
// - 超时（host_request_timeout）、宿主临时出错 → 原样抛出，gui 侧保留本地值，并在
//   下次打开菜单时重试。
function normalizeBridgeError(error: unknown): never {
  if (error instanceof HostBridgeUnavailableError) {
    const code = (error as { code?: unknown }).code;
    if (
      code === "host_capability_unsupported" ||
      code === "host_bridge_unavailable"
    ) {
      throw new Error(COMPOSER_MODEL_HISTORY_HOST_UNSUPPORTED_MESSAGE);
    }
    throw error;
  }
  if (
    error instanceof Error &&
    error.message === COMPOSER_MODEL_HISTORY_HOST_UNSUPPORTED_MESSAGE
  ) {
    throw new Error(COMPOSER_MODEL_HISTORY_HOST_UNSUPPORTED_MESSAGE);
  }
  throw error;
}

export function installEmbeddedComposerModelHistoryHost(): () => void {
  return registerComposerModelHistoryHost({
    readComposerModelHistory: (input) =>
      requestHostComposerModelHistory(input).catch(normalizeBridgeError),
    updateComposerModelHistory: (input) =>
      requestHostUpdateComposerModelHistory(input).catch(normalizeBridgeError)
  });
}
