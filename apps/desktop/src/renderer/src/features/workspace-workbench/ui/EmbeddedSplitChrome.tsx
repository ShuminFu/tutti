// 分栏配对（补丁 0108）的覆盖层：拖影、放下区、分隔线、每栏的栏头（票 04）。
//
// 它挂在嵌入 `<main>` 里、盖在两个 agent-gui 窗口壳之上，本身不参与布局：
// 几何（每栏的 left/width）全部由它投影到 `<main>` 的 `--rndmaster-split-ratio`
// 与壳元素的 `data-rndmaster-pane` 上，由 EmbeddedDintalDock.css 负责画。
//
// 票 04 之后：焦点不再画 2px 蓝线、正文里不再叠任何浮动按钮（原来的 ✕ 会压在
// agent-gui 自己的设置按钮上）。每栏顶上一条 44px 栏头，左边写「这是谁」
// （provider 图标 + 标题 + 项目胶囊），右边一排纯图标：链条 / ⋯ / ✕。
import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
  type ReactNode
} from "react";
import {
  CloseIcon,
  GridVerticalLinedIcon,
  LinkIcon
} from "@tutti-os/ui-system";
import { MoreHorizontalIcon } from "@tutti-os/ui-system/icons";
import { useTranslation } from "@renderer/i18n";
import {
  beginEmbeddedSplitDividerDrag,
  embeddedSplitDividerDragRatio,
  type EmbeddedSplitDividerDrag
} from "./embeddedSplitDividerDrag.ts";
import {
  buildEmbeddedSplitPaneHeaders,
  type EmbeddedSplitPaneHeaderMenuItem
} from "./embeddedSplitPaneHeader.ts";
import {
  embeddedSplitViewController,
  embeddedSplitViewSnapshot,
  subscribeEmbeddedSplitView
} from "./embeddedSplitView.ts";

const SPLIT_LABEL_KEYS = {
  closePane: "agentHost.agentGui.splitClosePane",
  dropPair: "agentHost.agentGui.splitDropPair",
  dropRelaunch: "agentHost.agentGui.splitDropRelaunch",
  pair: "agentHost.agentGui.splitPair",
  paneMenu: "agentHost.agentGui.splitPaneMenu",
  resetRatio: "agentHost.agentGui.splitResetRatio",
  swapPanes: "agentHost.agentGui.splitSwapPanes",
  unpair: "agentHost.agentGui.splitUnpair",
  untitled: "agentHost.agentGui.splitUntitledSession"
} as const;

const CLOSED_STATUSES = new Set([
  "completed",
  "failed",
  "canceled",
  "cancelled"
]);

export function EmbeddedSplitChrome(): ReactNode {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const { i18n } = useTranslation();
  const label = useCallback((key: string) => i18n.t(key), [i18n]);
  const snapshot = useSyncExternalStore(
    subscribeEmbeddedSplitView,
    embeddedSplitViewSnapshot,
    embeddedSplitViewSnapshot
  );
  // ⋯ 菜单一次只开一栏；记的是「哪一栏开着」而不是布尔，免得两栏各开一个。
  const [menuSide, setMenuSide] = useState<"left" | "right" | null>(null);
  const resizeDragRef = useRef<EmbeddedSplitDividerDrag | null>(null);

  const split = snapshot !== null && snapshot.panes.right !== null;
  const ratio = snapshot?.ratio ?? 0.5;
  // 左栏会话栏展开时，两栏几何整体右移这么多（占比，票 05）。分隔线、栏头、
  // 壳的 left/width 全部要一起加，少加一处就会出现「线和栏错位」。
  const railPush = snapshot?.railPushRatio ?? 0;
  const focus = snapshot?.focus ?? "left";
  const collapsed = snapshot?.collapsed === true;

  // 每次渲染都重投影（没有依赖数组）：窗口壳是工作台自己挂的，新壳挂上来的那一次
  // 也要立刻拿到 data-rndmaster-pane，否则会漏出一帧「两栏叠在一起」。
  useLayoutEffect(() => {
    const main = rootRef.current?.closest<HTMLElement>(
      ".rndmaster-dintaldock-embedded"
    );
    const controller = embeddedSplitViewController();
    if (!main) return;
    main.dataset.rndmasterSplit = split ? "split" : "single";
    main.dataset.rndmasterCollapsed = collapsed ? "true" : "false";
    main.dataset.rndmasterFocus = focus;
    main.style.setProperty("--rndmaster-split-ratio", String(ratio));
    main.style.setProperty("--rndmaster-split-rail-push", String(railPush));
    const shells = main.querySelectorAll<HTMLElement>(
      '.workbench-window-shell[data-workbench-node-type-id="agent-gui"]'
    );
    for (const shell of shells) {
      const nodeId = shell.getAttribute("data-workbench-window-id") ?? "";
      const side = controller?.sideForNodeId(nodeId) ?? null;
      if (!side) {
        shell.removeAttribute("data-rndmaster-pane");
        shell.removeAttribute("data-rndmaster-pane-focus");
        continue;
      }
      shell.setAttribute("data-rndmaster-pane", side);
      shell.setAttribute(
        "data-rndmaster-pane-focus",
        side === focus ? "true" : "false"
      );
    }
  });

  // 焦点栏 = 最后一次在里面按下的那一栏（PRD 故事 25）。用捕获阶段，免得被
  // 窗口里的组件 stopPropagation 掉。
  useLayoutEffect(() => {
    const main = rootRef.current?.closest<HTMLElement>(
      ".rndmaster-dintaldock-embedded"
    );
    if (!main) return;
    const onPointerDown = (event: Event): void => {
      const target = event.target as Element | null;
      const shell = target?.closest?.(
        ".workbench-window-shell[data-rndmaster-pane]"
      );
      const side = shell?.getAttribute("data-rndmaster-pane");
      if (side !== "left" && side !== "right") return;
      embeddedSplitViewController()?.setFocus(side);
    };
    main.addEventListener("pointerdown", onPointerDown, true);
    return () => main.removeEventListener("pointerdown", onPointerDown, true);
  }, []);

  // 栏头压在整条壳上，但「这是谁」要对齐**正文**：左栏前面还有 provider 栏和会话栏，
  // 标题贴壳的最左边就浮在侧栏上方了（真机截图，票 04 回归）。上游把栏宽写成
  // `--agent-gui-provider-rail-width` / `--agent-gui-conversation-rail-width`，
  // 但那两个变量挂在 `.agent-gui-node__layout` 上，覆盖层不是它的后代、继承不到，
  // 所以只能量一次真实左边界；顺带量出标题有没有被截断，截断了才挂 hover 气泡。
  // presentation-work: 栏宽可拖可折叠，除了观察尺寸没有别的通知。
  useLayoutEffect(() => {
    const root = rootRef.current;
    const main = root?.closest<HTMLElement>(".rndmaster-dintaldock-embedded");
    if (!root || !main) return;
    const sync = (): void => {
      // 左栏会话栏占多宽 —— 推给 controller，让它把这块地方从右栏挤出来（票 05）。
      // 量的是**面板实际宽度**而不是常量：栏宽可拖，而且上游还会按容器宽度夹逼
      // （`clampAgentGUIConversationRailWidthPx`），写死数字必然对不上。
      // 收起时面板宽 0，推力自然归零、几何退回原样。
      // 注意别按「现在是不是抽屉态」来判要不要推：推了之后左栏变宽会跨过上游
      // 630px 的自动折叠阈值、抽屉态消失，再据此撤销推力就会来回抖。按「栏在不在」
      // 判则两种形态下量到的都是同一个 `--agent-gui-conversation-rail-width`，收敛。
      const leftRail = main.querySelector<HTMLElement>(
        '.workbench-window-shell[data-rndmaster-pane="left"] .agent-gui-node__rail-panel'
      );
      embeddedSplitViewController()?.setRailPushPx(
        leftRail ? Math.round(leftRail.getBoundingClientRect().width) : 0
      );
      for (const header of root.querySelectorAll<HTMLElement>(
        ".rndmaster-split-pane-header"
      )) {
        const shell = main.querySelector<HTMLElement>(
          `.workbench-window-shell[data-rndmaster-pane="${header.dataset.side}"]`
        );
        const detail = shell?.querySelector<HTMLElement>(
          ".agent-gui-node__detail-panel"
        );
        const inset =
          shell && detail
            ? Math.max(
                0,
                Math.round(
                  detail.getBoundingClientRect().left -
                    shell.getBoundingClientRect().left
                )
              )
            : 0;
        header.style.setProperty(
          "--rndmaster-split-pane-content-left",
          `${inset}px`
        );
        const title = header.querySelector<HTMLElement>(
          ".rndmaster-split-pane-header__title"
        );
        const wrap = title?.parentElement;
        if (title && wrap) {
          wrap.dataset.truncated =
            title.scrollWidth > title.clientWidth + 1 ? "true" : "false";
        }
      }
    };
    sync();
    const observer = new ResizeObserver(sync);
    observer.observe(main);
    for (const detail of main.querySelectorAll(
      ".agent-gui-node__detail-panel"
    )) {
      observer.observe(detail);
    }
    // 会话栏自己也要盯：它一展开/收起，主区和详情面板的尺寸可能一帧都不变
    // （壳宽是我们按推力算的，鸡生蛋），只有它自己的宽度变了。
    for (const railPanel of main.querySelectorAll(
      ".agent-gui-node__rail-panel"
    )) {
      observer.observe(railPanel);
    }
    return () => observer.disconnect();
  });

  const onDividerPointerDown = useCallback(
    (event: ReactPointerEvent<HTMLDivElement>): void => {
      const main = event.currentTarget.closest<HTMLElement>(
        ".rndmaster-dintaldock-embedded"
      );
      if (!main) return;
      const rect = main.getBoundingClientRect();
      // 按下这一刻的快照要现读：这个回调的依赖数组是空的，闭包里的 ratio/railPush
      // 是第一次渲染的值。
      const at = embeddedSplitViewSnapshot();
      const drag = beginEmbeddedSplitDividerDrag({
        clientX: event.clientX,
        dividerRatio: (at?.ratio ?? 0.5) + (at?.railPushRatio ?? 0),
        surfaceLeft: rect.left,
        surfaceWidth: rect.width
      });
      if (!drag) return;
      resizeDragRef.current = drag;
      event.currentTarget.setPointerCapture?.(event.pointerId);
      event.preventDefault();
      const onMove = (move: PointerEvent): void => {
        const active = resizeDragRef.current;
        if (!active) return;
        // 夹逼（每栏 >= 320px）在 controller 里做，这里只给原始比例。
        // railPush 每帧现读：会话栏宽度会被右栏下限夹逼，拖动中会变。
        const next = embeddedSplitDividerDragRatio(
          active,
          move.clientX,
          embeddedSplitViewSnapshot()?.railPushRatio ?? 0
        );
        if (next === null) return;
        embeddedSplitViewController()?.resize(next);
      };
      const onUp = (): void => {
        resizeDragRef.current = null;
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
        window.removeEventListener("pointercancel", onUp);
      };
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
      window.addEventListener("pointercancel", onUp);
    },
    []
  );

  const onMenuItem = useCallback((item: EmbeddedSplitPaneHeaderMenuItem) => {
    const controller = embeddedSplitViewController();
    setMenuSide(null);
    if (item.id === "resetRatio") {
      controller?.resetRatio();
      return;
    }
    controller?.swapPanes();
  }, []);

  if (!snapshot) return <div ref={rootRef} hidden />;

  const dragging = snapshot.dragging;
  const controller = embeddedSplitViewController();
  const hoverSide = dragging?.hoverSide ?? null;
  const dropZone = hoverSide
    ? (controller?.dropZoneRect(hoverSide) ?? null)
    : null;
  const draggedClosed = CLOSED_STATUSES.has(
    (dragging?.session.status ?? "").trim().toLowerCase()
  );

  const dividerStyle: CSSProperties = {
    left: `calc(${ratio + railPush} * 100%)`
  };

  const headers = buildEmbeddedSplitPaneHeaders(snapshot, {
    closePane: label(SPLIT_LABEL_KEYS.closePane),
    menu: label(SPLIT_LABEL_KEYS.paneMenu),
    pair: label(SPLIT_LABEL_KEYS.pair),
    resetRatio: label(SPLIT_LABEL_KEYS.resetRatio),
    swapPanes: label(SPLIT_LABEL_KEYS.swapPanes),
    unpair: label(SPLIT_LABEL_KEYS.unpair),
    untitled: label(SPLIT_LABEL_KEYS.untitled)
  });

  return (
    <div className="rndmaster-split-chrome" ref={rootRef}>
      {dragging ? (
        <div
          className="rndmaster-split-ghost"
          style={{
            transform: `translate3d(${dragging.point.x}px, ${dragging.point.y}px, 0) rotate(-2deg)`
          }}
        >
          {dragging.session.iconUrl ? (
            <img
              alt=""
              className="rndmaster-split-ghost__icon"
              src={dragging.session.iconUrl}
            />
          ) : null}
          <span className="rndmaster-split-ghost__title">
            {dragging.session.title}
          </span>
        </div>
      ) : null}

      {dragging && dropZone ? (
        <div
          className="rndmaster-split-dropzone"
          style={{ left: `${dropZone.left}px`, width: `${dropZone.width}px` }}
        >
          <div className="rndmaster-split-dropzone__badge">
            <GridVerticalLinedIcon size={20} />
            <span>{label(SPLIT_LABEL_KEYS.dropPair)}</span>
            {draggedClosed ? (
              <span className="rndmaster-split-dropzone__hint">
                {label(SPLIT_LABEL_KEYS.dropRelaunch)}
              </span>
            ) : null}
          </div>
        </div>
      ) : null}

      {split && !collapsed ? (
        <div
          className="rndmaster-split-divider"
          onDoubleClick={() => embeddedSplitViewController()?.resetRatio()}
          onPointerDown={onDividerPointerDown}
          role="separator"
          aria-orientation="vertical"
          style={dividerStyle}
        />
      ) : null}

      {headers.map((header) => (
        <div
          className="rndmaster-split-pane-header"
          data-focused={header.focused ? "true" : "false"}
          data-side={header.side}
          key={header.side}
          // 点栏头也算「在这一栏里操作」：壳上那个捕获监听收不到覆盖层的按下。
          onPointerDown={() =>
            embeddedSplitViewController()?.setFocus(header.side)
          }
          // 栏头几何跟着壳走：左栏加宽 railPush，右栏起点右移、宽度同步变窄。
          style={{
            left: `calc(${
              header.side === "left"
                ? header.leftFraction
                : header.leftFraction + railPush
            } * 100%)`,
            width: `calc(${
              header.side === "left"
                ? header.widthFraction + railPush
                : header.widthFraction - railPush
            } * 100%)`
          }}
        >
          <div className="rndmaster-split-pane-header__identity">
            {header.iconUrl ? (
              <img
                alt=""
                className="rndmaster-split-pane-header__icon"
                src={header.iconUrl}
              />
            ) : null}
            {/* 气泡挂在**外层**：标题本体要 `overflow: hidden` 才能省略号，
                气泡若长在它身上会被自己裁掉。文字走 `attr()`，是否截断由测量
                effect 写在外层的 `data-truncated` 上——没截断就不弹。 */}
            <span
              className="rndmaster-split-pane-header__title-wrap"
              data-full-title={header.title}
            >
              <span className="rndmaster-split-pane-header__title">
                {header.title}
              </span>
            </span>
            {header.projectLabel ? (
              <span className="rndmaster-split-pane-header__project">
                {header.projectLabel}
              </span>
            ) : null}
          </div>
          <div className="rndmaster-split-pane-header__actions">
            {header.pairingState ? (
              <button
                aria-label={header.pairingLabel}
                className="rndmaster-split-pane-header__action"
                data-action="pair"
                data-state={header.pairingState}
                disabled={!header.pairingEnabled}
                onClick={() => {
                  const active = embeddedSplitViewController();
                  if (header.pairingState === "paired") {
                    void active?.unpairPanes();
                  } else {
                    void active?.pairPanes();
                  }
                }}
                title={header.pairingLabel}
                type="button"
              >
                <LinkIcon size={14} />
              </button>
            ) : null}
            <button
              aria-expanded={menuSide === header.side}
              aria-haspopup="menu"
              aria-label={header.menuLabel}
              className="rndmaster-split-pane-header__action"
              data-action="menu"
              onClick={() =>
                setMenuSide((current) =>
                  current === header.side ? null : header.side
                )
              }
              title={header.menuLabel}
              type="button"
            >
              <MoreHorizontalIcon aria-hidden="true" size={14} />
            </button>
            <button
              aria-label={header.closeLabel}
              className="rndmaster-split-pane-header__action"
              data-action="close"
              onClick={() =>
                void embeddedSplitViewController()?.closePane(header.side)
              }
              title={header.closeLabel}
              type="button"
            >
              <CloseIcon size={12} />
            </button>
          </div>
          {menuSide === header.side ? (
            <div className="rndmaster-split-pane-header__menu" role="menu">
              {header.menuItems.map((item) => (
                <button
                  className="rndmaster-split-pane-header__menu-item"
                  key={item.id}
                  onClick={() => onMenuItem(item)}
                  role="menuitem"
                  type="button"
                >
                  {item.label}
                </button>
              ))}
            </div>
          ) : null}
        </div>
      ))}
    </div>
  );
}
