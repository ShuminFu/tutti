import assert from "node:assert/strict";
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

test("keeps the frontmost agent and removes the rest of the desktop", async () => {
  const closed: string[] = [];
  const exited: string[] = [];
  const focused: string[] = [];
  const result = await reconcileEmbeddedDintalDock({
    closeNode: (id) => closed.push(id),
    exitFullscreenNode: (id) => exited.push(id),
    focusNode: (id) => focused.push(id),
    getSnapshot: () => ({
      nodeStack: ["agent-old", "files", "agent-current"],
      nodes: [
        {
          data: { typeId: "agent-gui" },
          displayMode: "floating",
          id: "agent-old"
        },
        { data: { typeId: "files" }, displayMode: "floating", id: "files" },
        {
          data: { typeId: "agent-gui" },
          displayMode: "fullscreen",
          id: "agent-current"
        }
      ]
    }),
    launchNode: async () => null,
    load: async () => undefined
  });

  assert.equal(result, "agent-current");
  assert.deepEqual(closed, ["agent-old", "files"]);
  assert.deepEqual(exited, ["agent-current"]);
  assert.deepEqual(focused, ["agent-current"]);
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
