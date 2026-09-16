// 分栏结对模式（peer-pair-mode 票 04/05）在 agent-gui 作曲区里的两处接入。
//
// 为什么接在作曲区里、而不是像栏头那样画在覆盖层上：单选要**占位**——两栏都画
// （焦点栏可点、非焦点栏是压暗的镜像），两栏输入框才等高；覆盖层是浮在壳上的，
// 画在那里只会盖住转录的最后一行。agent-gui 包不能 import 分栏层，所以走 gui 包
// 声明的作曲区宿主扩展口（agentComposerHostExtension）：
//   - renderAboveComposer → 这里的 <EmbeddedSplitPairModeBar>，自己订阅分栏快照；
//   - wantsSubmitPreparation / prepareSubmit → 分栏 controller 的开工卡准备（票 05）。
// 两处都在调用时现取 controller（注册口），分栏还没起来时一律「不画 / 不拦」。
import {
  useCallback,
  useRef,
  useSyncExternalStore,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode
} from "react";
import { registerAgentComposerHostExtension } from "@tutti-os/agent-gui/conversation-rail-projection";
import { useTranslation } from "@renderer/i18n";
import {
  EMBEDDED_SPLIT_PAIR_MODE_CHOICES,
  buildEmbeddedSplitPairModeBar,
  nextEmbeddedSplitPairModeChoice
} from "./embeddedSplitPairModeBar.ts";
import {
  embeddedSplitViewController,
  embeddedSplitViewSnapshot,
  subscribeEmbeddedSplitView,
  type EmbeddedSplitPairModeChoice
} from "./embeddedSplitView.ts";

const PAIR_MODE_LABEL_KEYS: Record<EmbeddedSplitPairModeChoice, string> = {
  developer: "agentHost.agentGui.splitPairRoleDeveloper",
  reviewer: "agentHost.agentGui.splitPairRoleReviewer",
  solo: "agentHost.agentGui.splitPairModeSolo"
};

export function EmbeddedSplitPairModeBar({
  agentSessionId
}: {
  agentSessionId: string | null;
}): ReactNode {
  const { i18n } = useTranslation();
  const snapshot = useSyncExternalStore(
    subscribeEmbeddedSplitView,
    embeddedSplitViewSnapshot,
    embeddedSplitViewSnapshot
  );
  const buttonsRef = useRef<Map<EmbeddedSplitPairModeChoice, HTMLButtonElement>>(
    new Map()
  );
  const model = buildEmbeddedSplitPairModeBar(snapshot, agentSessionId);
  const side = model?.side ?? null;
  const interactive = model?.interactive === true;

  const choose = useCallback(
    (choice: EmbeddedSplitPairModeChoice) => {
      // 评审补充 2：非焦点栏 / 写在途时点了也不写。两次点击间隔短于一次重渲染时这里的
      // interactive 还是旧值，所以 controller.setPairMode 自己也按 busy 再挡一次。
      if (!side || !interactive) return;
      void embeddedSplitViewController()?.setPairMode(side, choice);
    },
    [interactive, side]
  );

  if (!model) return null;

  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>): void => {
    if (!model.interactive) return;
    const next = nextEmbeddedSplitPairModeChoice(model.value, event.key);
    if (!next) return;
    event.preventDefault();
    buttonsRef.current.get(next)?.focus();
    choose(next);
  };

  const renderChoice = (choice: EmbeddedSplitPairModeChoice): ReactNode => {
    const checked = model.value === choice;
    return (
      <button
        aria-checked={checked}
        className="rndmaster-split-pair-mode__option"
        data-choice={choice}
        data-checked={checked ? "true" : "false"}
        key={choice}
        onClick={() => choose(choice)}
        ref={(element) => {
          if (element) buttonsRef.current.set(choice, element);
          else buttonsRef.current.delete(choice);
        }}
        role="radio"
        // roving tabindex：整组只有选中那一项进 Tab 序列，组内用方向键走。
        tabIndex={model.interactive && checked ? 0 : -1}
        type="button"
      >
        {i18n.t(PAIR_MODE_LABEL_KEYS[choice])}
      </button>
    );
  };

  const [solo, ...roles] = EMBEDDED_SPLIT_PAIR_MODE_CHOICES;
  return (
    <div
      aria-disabled={!model.interactive}
      aria-label={i18n.t("agentHost.agentGui.splitPairModeLabel")}
      className="rndmaster-split-pair-mode"
      data-focused={model.focused ? "true" : "false"}
      data-side={model.side}
      // 非焦点栏是镜像：不进无障碍树的交互，也不吃点击（CSS 里 pointer-events:none）。
      inert={model.focused ? undefined : true}
      onKeyDown={onKeyDown}
      role="radiogroup"
    >
      {solo ? renderChoice(solo) : null}
      <span aria-hidden="true" className="rndmaster-split-pair-mode__divider" />
      <span className="rndmaster-split-pair-mode__label">
        {i18n.t("agentHost.agentGui.splitPairModeLabel")}
      </span>
      {roles.map(renderChoice)}
    </div>
  );
}

/**
 * 把分栏结对模式接进 agent-gui 作曲区（只在嵌入 DinTalDock 时装）。返回卸载函数。
 * 提交拦截的判据全部在分栏 controller 里（本栏会话所在的一对是 pair 模式且
 * kickoffState=pending），这里只做转发。
 */
export function installEmbeddedSplitComposerHost(): () => void {
  return registerAgentComposerHostExtension({
    prepareSubmit: (input) =>
      embeddedSplitViewController()?.preparePairKickoff(input) ??
      Promise.resolve(null),
    renderAboveComposer: ({ agentSessionId }) => (
      <EmbeddedSplitPairModeBar agentSessionId={agentSessionId} />
    ),
    wantsSubmitPreparation: ({ agentSessionId }) =>
      embeddedSplitViewController()?.wantsPairKickoff(agentSessionId) === true
  });
}
