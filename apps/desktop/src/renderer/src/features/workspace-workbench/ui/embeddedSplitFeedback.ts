// 分栏配对（补丁 0108）在 React 树之外的两件外设：toast 与文案。
//
// toast 走 apps/desktop 既有的那一条：`@renderer/lib/toast` 的 `Toast` 是模块级
// 队列（DesktopToastProvider 在 main.tsx 根上渲染它），任何非组件代码都能直接推，
// 与 workspaceFilesLaunchFeedback.ts 同一条路，不需要新造 toast 桥。
//
// 文案走 gui 包合并进来的字典：agentGuiI18nResources 在 i18n/appRuntime.ts 里
// 已并入运行时，键前缀是 `agentHost.agentGui.*`（先例：WorkspaceFileManagerPane
// 的 referencePicker 那张表）。`translate()` 的入参类型只认 desktop 自己的键，
// 所以这里直接拿运行时的 `t(string)`。
import { getAppI18nRuntime } from "@renderer/i18n/appRuntime";
import { getActiveLocale } from "@renderer/i18n/runtime";
import { Toast } from "@renderer/lib/toast";
import type {
  EmbeddedSplitPairingLabels,
  EmbeddedSplitToast
} from "./embeddedSplitPairing.ts";

export function translateEmbeddedSplitLabel(key: string): string {
  return getAppI18nRuntime(getActiveLocale()).t(key);
}

export const embeddedSplitToast: EmbeddedSplitToast = {
  error(message) {
    Toast.Error(message);
  },
  info(message) {
    Toast.tips(message);
  },
  success(message) {
    Toast.Success(message);
  }
};

/** 每次读都重新取：语言可以在运行时切。 */
export function embeddedSplitPairingLabels(): EmbeddedSplitPairingLabels {
  return {
    paired: translateEmbeddedSplitLabel("agentHost.agentGui.splitPaired"),
    relaunchTimeout: translateEmbeddedSplitLabel(
      "agentHost.agentGui.splitRelaunchTimeout"
    ),
    unmanaged: translateEmbeddedSplitLabel(
      "agentHost.agentGui.peerPairUnmanaged"
    )
  };
}
