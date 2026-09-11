import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  activateEmbeddedDintalDockSession,
  applyEmbeddedDintalDockContributions,
  isEmbeddedDintalDock,
  reconcileEmbeddedDintalDock,
  renderEmbeddedDintalDockBody
} from "./embeddedDintalDock.ts";

test("detects only the protected host bootstrap query", () => {
  assert.equal(isEmbeddedDintalDock("?tuttiBootstrap=nonce-1"), true);
  assert.equal(isEmbeddedDintalDock("?workspace=one"), false);
});

test("activates the requested session in the frontmost Agent node", () => {
  const activations: unknown[] = [];
  const focused: string[] = [];
  const result = activateEmbeddedDintalDockSession({
    activateNode: (...args: unknown[]) => activations.push(args),
    closeNode() {},
    exitFullscreenNode() {},
    focusNode: (id: string) => focused.push(id),
    getSnapshot: () => ({
      nodeStack: ["agent-old", "files", "agent-current"],
      nodes: [
        { data: { typeId: "agent-gui" }, id: "agent-old" },
        { data: { typeId: "files" }, id: "files" },
        { data: { typeId: "agent-gui" }, id: "agent-current" }
      ]
    }),
    launchNode: async () => null,
    load: async () => undefined
  } as never, "session-1");

  assert.equal(result, true);
  assert.deepEqual(activations, [[
    { nodeId: "agent-current" },
    {
      payload: { agentSessionId: "session-1" },
      type: "agent-gui:open-session"
    }
  ]]);
  assert.deepEqual(focused, ["agent-current"]);
});

test("projects the embedded Agent frame for its body and header", () => {
  const rawFrame = { height: 192, width: 320, x: 13, y: 52 };
  const projectedFrame = { height: 825, width: 759, x: 0, y: 0 };
  const bodyFrames: (typeof rawFrame)[] = [];
  const headerFrames: (typeof rawFrame)[] = [];
  const keyFrames: (typeof rawFrame)[] = [];
  const renderBody = (context: { node: { frame: typeof rawFrame } }) => {
    bodyFrames.push(context.node.frame);
    return null;
  };
  const contributions = applyEmbeddedDintalDockContributions([
    {
      id: "agent",
      nodes: [
        {
          frame: rawFrame,
          getHeaderFrameRenderKey: (context) => {
            keyFrames.push(context.node.frame);
            return context.node.frame.width >= 630 ? "expanded" : "collapsed";
          },
          renderBody: renderBody as never,
          renderHeader: (context) => {
            headerFrames.push(context.node.frame);
            assert.equal(context.dragHandleProps.onDoubleClick, undefined);
            assert.equal(context.dragHandleProps.onPointerDown, undefined);
            return null;
          },
          title: "Agent",
          typeId: "agent-gui"
        }
      ]
    }
  ]);
  const definition = contributions?.[0]?.nodes?.[0];
  const getHeaderFrameRenderKey = definition?.getHeaderFrameRenderKey;
  const renderHeader = definition?.renderHeader;
  assert.ok(getHeaderFrameRenderKey);
  assert.ok(renderHeader);

  renderToStaticMarkup(
    renderHeader({
      dragHandleProps: {
        "data-workbench-drag-handle": "true",
        onDoubleClick: () => undefined,
        onPointerDown: () => undefined
      },
      node: { frame: rawFrame },
      surfaceSize: { height: 825, width: 759 }
    } as never) as never
  );
  assert.equal(
    getHeaderFrameRenderKey({
      node: { frame: rawFrame },
      surfaceSize: { height: 825, width: 759 }
    } as never),
    "expanded"
  );
  renderEmbeddedDintalDockBody(
    {
      host: {
        getSnapshot: () => ({
          surfaceSize: { height: 825, width: 759 }
        })
      },
      node: { frame: rawFrame }
    } as never,
    renderBody as never
  );

  assert.deepEqual(headerFrames, [projectedFrame]);
  assert.deepEqual(keyFrames, [projectedFrame]);
  assert.deepEqual(bodyFrames, [projectedFrame]);
});

test("shares a session-only narrow rail intent between header and body", () => {
  const frame = { height: 825, width: 600, x: 0, y: 0 };
  let headerExpanded: boolean | undefined;
  let bodyExpanded: boolean | undefined;
  let setHeaderExpanded: ((expanded: boolean) => void) | undefined;
  const controller = {
    getSnapshot: () => ({ surfaceSize: { height: 825, width: 600 } }),
    subscribe: () => () => undefined
  };
  const contribution = applyEmbeddedDintalDockContributions([
    {
      id: "agent",
      nodes: [
        {
          frame,
          renderBody: ((context: {
            conversationRailNarrowExpanded?: boolean;
          }) => {
            bodyExpanded = context.conversationRailNarrowExpanded;
            return null;
          }) as never,
          renderHeader: ((context: {
            conversationRailNarrowExpanded?: boolean;
            onConversationRailNarrowExpandedChange?: (
              expanded: boolean
            ) => void;
          }) => {
            headerExpanded = context.conversationRailNarrowExpanded;
            setHeaderExpanded = context.onConversationRailNarrowExpandedChange;
            return null;
          }) as never,
          title: "Agent",
          typeId: "agent-gui"
        }
      ]
    }
  ])?.[0]?.nodes?.[0];
  assert.ok(contribution?.renderHeader);

  const headerContext = {
    dragHandleProps: {},
    node: { frame, id: "agent-node" },
    surfaceSize: { height: 825, width: 600 }
  } as never;
  const bodyContext = {
    host: {
      controller,
      getSnapshot: controller.getSnapshot
    },
    node: { frame, id: "agent-node" }
  } as never;
  const renderSharedState = (): void => {
    renderToStaticMarkup(contribution.renderHeader?.(headerContext) as never);
    renderToStaticMarkup(contribution.renderBody(bodyContext) as never);
  };

  renderSharedState();
  assert.equal(headerExpanded, false);
  assert.equal(bodyExpanded, false);
  assert.ok(setHeaderExpanded);

  setHeaderExpanded(true);
  renderSharedState();
  assert.equal(headerExpanded, true);
  assert.equal(bodyExpanded, true);
});

test("waits for a positive body surface and keeps narrow auto-collapse", () => {
  const rawFrame = { height: 192, width: 320, x: 13, y: 52 };
  let surfaceSize = { height: 0, width: 0 };
  const bodyFrames: (typeof rawFrame)[] = [];
  const renderBody = (context: { node: { frame: typeof rawFrame } }) => {
    bodyFrames.push(context.node.frame);
    return null;
  };
  const bodyContext = {
    host: { getSnapshot: () => ({ surfaceSize }) },
    node: { frame: rawFrame }
  } as never;

  renderEmbeddedDintalDockBody(bodyContext, renderBody as never);
  surfaceSize = { height: 825, width: Number.NaN };
  renderEmbeddedDintalDockBody(bodyContext, renderBody as never);
  surfaceSize = { height: 825, width: 759 };
  renderEmbeddedDintalDockBody(bodyContext, renderBody as never);
  assert.deepEqual(bodyFrames, [
    rawFrame,
    rawFrame,
    { height: 825, width: 759, x: 0, y: 0 }
  ]);

  const getHeaderFrameRenderKey = applyEmbeddedDintalDockContributions([
    {
      id: "agent",
      nodes: [
        {
          frame: rawFrame,
          getHeaderFrameRenderKey: (context) =>
            context.node.frame.width >= 630 ? "expanded" : "collapsed",
          renderBody: () => null,
          title: "Agent",
          typeId: "agent-gui"
        }
      ]
    }
  ])?.[0]?.nodes?.[0]?.getHeaderFrameRenderKey;
  assert.equal(
    getHeaderFrameRenderKey?.({
      node: { frame: rawFrame },
      surfaceSize: { height: 825, width: 600 }
    } as never),
    "collapsed"
  );
  assert.equal(
    getHeaderFrameRenderKey?.({
      node: { frame: rawFrame },
      surfaceSize: { height: Number.POSITIVE_INFINITY, width: 759 }
    } as never),
    "collapsed"
  );
});

test("leaves non-Agent contributions unchanged", () => {
  const node = {
    frame: { height: 600, width: 800, x: 0, y: 0 },
    renderBody: () => null,
    title: "Files",
    typeId: "files"
  };
  const contributions = applyEmbeddedDintalDockContributions([
    { id: "files", nodes: [node] }
  ]);
  assert.equal(contributions?.[0]?.nodes?.[0], node);
});

// Split view (patch 0108) keeps up to TWO Agent windows — one per pane — and
// hands them to the split controller frontmost-first. Everything else on the
// persisted desktop is still removed, and a third Agent window is still closed.
test("keeps the two frontmost agents and removes the rest of the desktop", async () => {
  const closed: string[] = [];
  const exited: string[] = [];
  const focused: string[] = [];
  const adoptedCalls: string[][] = [];
  const result = await reconcileEmbeddedDintalDock(
    {
      closeNode: (id: string) => closed.push(id),
      exitFullscreenNode: (id: string) => exited.push(id),
      focusNode: (id: string) => focused.push(id),
      getSnapshot: () => ({
        nodeStack: ["agent-stale", "agent-old", "files", "agent-current"],
        nodes: [
          {
            data: { typeId: "agent-gui" },
            displayMode: "floating",
            id: "agent-stale"
          },
          {
            data: { typeId: "agent-gui" },
            displayMode: "floating",
            id: "agent-old"
          },
          {
            data: { typeId: "files" },
            displayMode: "floating",
            id: "files"
          },
          {
            data: { typeId: "agent-gui" },
            displayMode: "fullscreen",
            id: "agent-current"
          }
        ]
      }),
      launchNode: async () => null,
      load: async () => undefined
    } as never,
    {
      adopt: async (ids: readonly string[]) => {
        adoptedCalls.push([...ids]);
      }
    }
  );

  assert.equal(result, "agent-current");
  assert.deepEqual(closed, ["agent-stale", "files"]);
  assert.deepEqual(exited, ["agent-current"]);
  assert.deepEqual(focused, ["agent-current"]);
  assert.deepEqual(adoptedCalls, [["agent-current", "agent-old"]]);
});

// `onHandleReady` 会用**同一个 host** 反复回调（它的 effect 依赖回调身份，而回调是带
// `contributions` 依赖的 useCallback，工作台一有窗口变化就换新身份）。reconcile 现在会
// `split.adopt`，adopt 又会给空着的那一栏起窗口 —— 重入一次就多一个窗口，新窗口再触发
// 下一次重入。真机上是窗口每秒新增一个、WebContent 涨到 33GB 后崩溃，所以同一个 host
// 必须只 reconcile 一次。
test("reconciles a host handle only once even if called again", async () => {
  const closed: string[] = [];
  const launched: unknown[] = [];
  const adoptedCalls: string[][] = [];
  const host = {
    closeNode: (id: string) => closed.push(id),
    exitFullscreenNode: () => undefined,
    focusNode: () => undefined,
    getSnapshot: () => ({
      nodeStack: ["agent-a", "files"],
      nodes: [
        {
          data: { typeId: "agent-gui" },
          displayMode: "floating",
          id: "agent-a"
        },
        { data: { typeId: "files" }, displayMode: "floating", id: "files" }
      ]
    }),
    launchNode: async (input: unknown) => {
      launched.push(input);
      return "agent-new";
    },
    load: async () => undefined
  } as never;
  const split = {
    adopt: async (ids: readonly string[]) => {
      adoptedCalls.push([...ids]);
    }
  };

  const first = await reconcileEmbeddedDintalDock(host, split);
  const second = await reconcileEmbeddedDintalDock(host, split);

  assert.equal(first, "agent-a");
  assert.equal(second, null);
  assert.deepEqual(closed, ["files"]);
  assert.deepEqual(launched, []);
  assert.deepEqual(adoptedCalls, [["agent-a"]]);
});

test("opens an agent when the persisted desktop has none", async () => {
  const launched: { reason: "host"; typeId: string }[] = [];
  const result = await reconcileEmbeddedDintalDock({
    closeNode: () => undefined,
    exitFullscreenNode: () => undefined,
    focusNode: () => undefined,
    getSnapshot: () => ({ nodeStack: [], nodes: [] }),
    launchNode: async (input) => {
      launched.push(input);
      return "agent-new";
    },
    load: async () => undefined
  });

  assert.equal(result, "agent-new");
  assert.deepEqual(launched, [{ reason: "host", typeId: "agent-gui" }]);
});

// Issue 03: the master shell paints the only top bar, so the embedded header
// context has to tell the Agent header contract to render nothing at all.
test("marks the embedded header context as host-owned chrome", () => {
  const frame = { height: 825, width: 900, x: 0, y: 0 };
  const headerContexts: { embedded?: boolean }[] = [];
  const contribution = applyEmbeddedDintalDockContributions([
    {
      id: "agent",
      nodes: [
        {
          frame,
          renderBody: (() => null) as never,
          renderHeader: ((context: { embedded?: boolean }) => {
            headerContexts.push(context);
            return null;
          }) as never,
          title: "Agent",
          typeId: "agent-gui"
        }
      ]
    }
  ])?.[0]?.nodes?.[0];
  assert.ok(contribution?.renderHeader);

  renderToStaticMarkup(
    contribution.renderHeader({
      dragHandleProps: {},
      node: { frame, id: "agent-node" },
      surfaceSize: { height: 825, width: 900 }
    } as never) as never
  );

  assert.equal(headerContexts.length, 1);
  assert.equal(headerContexts[0]?.embedded, true);
});

const embeddedDintalDockCss = readFileSync(
  new URL("./EmbeddedDintalDock.css", import.meta.url),
  "utf8"
);

// With no header row left, the 44px the three columns reserve for it under
// `[data-window-header-layout="overlay"]` would render as an empty band under
// the master toolbar. The embedding stylesheet has to zero that reservation.
test("the embedded surface reserves no height for the removed header row", () => {
  const reservation = embeddedDintalDockCss.match(
    /\.rndmaster-dintaldock-embedded\s*\n?\s*\.workbench-window\[data-window-header-layout="overlay"\] \{([^}]*)\}/
  );
  assert.notEqual(reservation, null);
  assert.match(
    String(reservation?.[1] ?? ""),
    /--agent-gui-workbench-header-height:\s*0px;/
  );
  assert.match(
    embeddedDintalDockCss,
    /\.workbench-window__header \{\n\s*display: none !important;/
  );
});

// 右栏用 `display: none` 藏掉会话栏/provider 栏/拖宽把手，那三个元素于是不再是网格项，
// 详情面板会被**自动放到第 1 条 0 宽轨道**上，1fr 那条空着 —— 右栏内容被压成一条，
// 看着就是空白（票 03 第五轮真机：detail-panel 宽 0、timeline 宽 96）。
// 所以藏栏位和「把详情钉到最后一列」必须成对出现。
test("the right pane pins its detail panel to the last grid column", () => {
  assert.match(
    embeddedDintalDockCss,
    /\[data-rndmaster-pane="right"\]\s*\n\s*\.agent-gui-node__layout \{[^}]*grid-template-columns:\s*0 0 minmax\(0, 1fr\)/
  );
  const placement = embeddedDintalDockCss.match(
    /\[data-rndmaster-pane="right"\]\s*\n\s*\.agent-gui-node__detail-panel \{([^}]*)\}/
  );
  assert.notEqual(placement, null);
  assert.match(String(placement?.[1] ?? ""), /grid-column:\s*3/);
});

// 同一栏还会被工作台的遮挡优化判成「完全被盖住」：`data-agent-gui-visible="false"`
// 带来的 `content-visibility: hidden` 会跳过整棵子树的绘制/文本/命中测试，
// 盒子尺寸全对、屏幕上却是空白（票 03 第六轮真机）。并排的两栏谁也没盖住谁，
// 所以嵌入层必须把这条性能规则解除，动画同理。
test("split panes opt out of the workbench occlusion optimisation", () => {
  const occlusion = embeddedDintalDockCss.match(
    /\[data-rndmaster-pane\]\s*\n\s*\.agent-gui-node__layout\[data-agent-gui-visible="false"\] \{([^}]*)\}/
  );
  assert.notEqual(occlusion, null);
  assert.match(String(occlusion?.[1] ?? ""), /content-visibility:\s*visible/);
  assert.match(
    embeddedDintalDockCss,
    /\[data-agent-gui-visible="false"\]\s*\n\s*:where\(\*, \*::before, \*::after\) \{\s*animation-play-state:\s*running/
  );
});

// 栏头（票 04）压在窗口壳顶上 44px，正文必须让出同样的高度：上游用
// `--agent-gui-workbench-header-height` 给三列一起留空间（补丁 0106 清成了 0），
// 有栏头时把这份预留还回来。少了它，栏头会直接盖住会话栏第一行与详情顶部。
//
// 判据钉在 `data-rndmaster-pane-header` 上，不是 `data-rndmaster-split`：单栏也画
// 栏头（否则嵌入态顶上没有任何地方写当前是哪条会话），而首页态两种都不画。
test("panes give the content back the 44px the pane header takes", () => {
  const reservation = embeddedDintalDockCss.match(
    /\.rndmaster-dintaldock-embedded\[data-rndmaster-pane-header="true"\]\s*\n\s*\.workbench-window-shell\[data-rndmaster-pane\]\s*\n\s*\.workbench-window\[data-window-header-layout="overlay"\] \{([^}]*)\}/
  );
  assert.notEqual(reservation, null);
  assert.match(
    String(reservation?.[1] ?? ""),
    /--agent-gui-workbench-header-height:\s*var\(--rndmaster-split-header-height\)/
  );
  assert.match(
    embeddedDintalDockCss,
    /--rndmaster-split-header-height:\s*44px;/
  );
  assert.match(
    embeddedDintalDockCss,
    /\.rndmaster-split-pane-header \{[^}]*height: var\(--rndmaster-split-header-height\)/
  );
});

// 分隔线也要让出栏头：`top: 0` 时那条 1px 竖线会一直穿到栏头里，看着像连栏头
// 都被切成两半（真机截图，票 04 回归）。命中带同样下移，否则栏头高度内的拖动会
// 和右侧 Pair/⋯/✕ 抢事件。
// 栏头的「这是谁」要对齐正文：左栏前面还有 provider 栏和会话栏，贴壳左边就浮在
// 侧栏上方了。左边界量出来写进 `--rndmaster-split-pane-content-left`（票 04 回归）。
test("the header identity is inset to the pane content, not the shell edge", () => {
  const identity = embeddedDintalDockCss.match(
    /\.rndmaster-split-pane-header__identity \{([^}]*)\}/
  );
  assert.notEqual(identity, null);
  assert.match(
    String(identity?.[1] ?? ""),
    /padding-left:\s*var\(--rndmaster-split-pane-content-left/
  );
});

// 超长标题封顶 + 省略号，全文只在 hover 时用气泡给出；气泡必须长在**不裁剪**的
// 外壳上，长在标题本体上会被它自己的 `overflow: hidden` 裁掉（票 04 回归）。
test("a long pane title is capped and reveals the rest on hover", () => {
  const title = embeddedDintalDockCss.match(
    /\.rndmaster-split-pane-header__title \{([^}]*)\}/
  );
  assert.notEqual(title, null);
  assert.match(String(title?.[1] ?? ""), /max-width:\s*320px/);
  assert.match(String(title?.[1] ?? ""), /text-overflow:\s*ellipsis/);
  const bubble = embeddedDintalDockCss.match(
    /\.rndmaster-split-pane-header__title-wrap\[data-truncated="true"\]:hover::after \{([^}]*)\}/
  );
  assert.notEqual(bubble, null);
  assert.match(String(bubble?.[1] ?? ""), /content:\s*attr\(data-full-title\)/);
  const wrap = embeddedDintalDockCss.match(
    /\.rndmaster-split-pane-header__title-wrap \{([^}]*)\}/
  );
  assert.equal(/overflow:\s*hidden/.test(String(wrap?.[1] ?? "")), false);
});

test("the divider starts below the pane header instead of at the top", () => {
  const divider = embeddedDintalDockCss.match(
    /\.rndmaster-split-divider \{([^}]*)\}/
  );
  assert.notEqual(divider, null);
  assert.match(
    String(divider?.[1] ?? ""),
    /top:\s*var\(--rndmaster-split-header-height\)/
  );
});

// 焦点态（票 04）：不画横线、栏头不着色。焦点只体现在两处 —— 焦点栏标题加重、
// 非焦点栏的输入区/底部控件压暗。原来那条 2px 蓝线连样式都不该留下。
test("focus is expressed by the header title weight and a dimmed composer only", () => {
  assert.equal(embeddedDintalDockCss.includes("rndmaster-split-focus-line"), false);
  const focusedTitle = embeddedDintalDockCss.match(
    /\.rndmaster-split-pane-header\[data-focused="true"\]\s*\n\s*\.rndmaster-split-pane-header__title \{([^}]*)\}/
  );
  assert.notEqual(focusedTitle, null);
  assert.match(String(focusedTitle?.[1] ?? ""), /font-weight:\s*600/);
  const dimmed = embeddedDintalDockCss.match(
    /\[data-rndmaster-pane\]\[data-rndmaster-pane-focus="false"\]\s*\n\s*\.agent-gui-node__composer,[^{]*\{([^}]*)\}/
  );
  assert.notEqual(dimmed, null);
  assert.match(String(dimmed?.[1] ?? ""), /opacity:\s*0\.5/);
});

// ✕ 与链条都进了栏头：正文上不能再叠任何浮动按钮（右栏藏了会话栏之后，
// 原来那个浮动 ✕ 正好压住 agent-gui 自己的设置按钮）。
test("the pane chrome keeps no floating buttons over the content", () => {
  assert.equal(embeddedDintalDockCss.includes(".rndmaster-split-close"), false);
  assert.equal(embeddedDintalDockCss.includes(".rndmaster-split-link"), false);
  assert.match(
    embeddedDintalDockCss,
    /\.rndmaster-split-pane-header__action \{[^}]*background: transparent/
  );
});

// 双栏时点「某会话完成了」提示的「打开」走 requestWorkspaceAgentGuiLaunch({agentSessionId})：
// 工作台按 current-session 复用策略找不到已显示它的窗口就会开**第三个**窗口——分栏层不认领、
// reconcile 只跑一次不会关，它按工作台原始坐标整块画在两栏之上。嵌入态一律进分栏漏斗。
test("a session-scoped launch while embedded lands in a pane instead of a third window", async () => {
  const { routeEmbeddedDintalDockSessionLaunch } = await import(
    "./embeddedDintalDock.ts"
  );
  const activations: unknown[] = [];
  const host = {
    activateNode: (...args: unknown[]) => activations.push(args),
    closeNode() {},
    exitFullscreenNode() {},
    focusNode() {},
    getSnapshot: () => ({
      nodeStack: ["agent-current"],
      nodes: [{ data: { typeId: "agent-gui" }, id: "agent-current" }]
    }),
    launchNode: async () => null,
    load: async () => undefined
  } as never;

  assert.equal(
    routeEmbeddedDintalDockSessionLaunch(host, { agentSessionId: "s-1" }),
    null
  );
  assert.equal(activations.length, 1);
  // 新窗口 / 草稿 / 无会话号：不归漏斗管，照常 launchNode。
  assert.equal(
    routeEmbeddedDintalDockSessionLaunch(host, {
      agentSessionId: "s-1",
      openInNewWindow: true
    }),
    undefined
  );
  assert.equal(
    routeEmbeddedDintalDockSessionLaunch(host, {
      agentSessionId: "s-1",
      forceNewInstance: true
    }),
    undefined
  );
  assert.equal(
    routeEmbeddedDintalDockSessionLaunch(host, {
      agentSessionId: "s-1",
      draftPrompt: "hi"
    }),
    undefined
  );
  assert.equal(routeEmbeddedDintalDockSessionLaunch(host, {}), undefined);
  assert.equal(activations.length, 1);
});

// 票 05：分栏时会话栏展开要把空间从右栏挤出来，而不是浮在左栏正文上。
// 三条几何式必须一起带上推力，少一条就会出现「壳错位 / 分隔线不在缝上」。
test("分栏几何把会话栏的推力算进两栏宽度", () => {
  const left = embeddedDintalDockCss.match(
    /\.workbench-window-shell\[data-rndmaster-pane="left"\] \{([^}]*)\}/
  );
  assert.notEqual(left, null);
  assert.match(
    String(left?.[1] ?? ""),
    /width: calc\(\s*\(var\(--rndmaster-split-ratio\) \+ var\(--rndmaster-split-rail-push\)\)/
  );
  const right = embeddedDintalDockCss.match(
    /\.workbench-window-shell\[data-rndmaster-pane="right"\] \{([^}]*)\}/
  );
  assert.notEqual(right, null);
  assert.match(
    String(right?.[1] ?? ""),
    /left: calc\(\s*\(var\(--rndmaster-split-ratio\) \+ var\(--rndmaster-split-rail-push\)\)/
  );
  assert.match(
    String(right?.[1] ?? ""),
    /width: calc\(\s*\(1 - var\(--rndmaster-split-ratio\) - var\(--rndmaster-split-rail-push\)\)/
  );
  // 缺省值必须是 0：没分栏 / 会话栏收起时，几何要原样退回纯比例式。
  assert.match(embeddedDintalDockCss, /--rndmaster-split-rail-push: 0;/);
});

// 补丁 0073 的窄容器抽屉在分栏左栏里必然生效（半个主区 < 上游 630px 阈值），
// 一展开就盖住左栏正文和覆盖层栏头的标题。分栏下要把它按回网格里当真列。
test("分栏左栏把会话栏抽屉按回网格里的真列", () => {
  const grid = embeddedDintalDockCss.match(
    /\[data-rndmaster-pane="left"\]\s*\n\s*\.agent-gui-node__layout\[data-agent-gui-conversation-rail-overlay="true"\] \{([^}]*)\}/
  );
  assert.notEqual(grid, null);
  assert.match(
    String(grid?.[1] ?? ""),
    /var\(--agent-gui-conversation-rail-width\)/
  );
  const panels = embeddedDintalDockCss.match(
    /\.agent-gui-node__rail-panel \{\n\s*position: relative !important;([^}]*)\}/
  );
  assert.notEqual(panels, null);
  assert.match(String(panels?.[1] ?? ""), /left: auto !important;/);
  assert.match(String(panels?.[1] ?? ""), /box-shadow: none !important;/);
});
