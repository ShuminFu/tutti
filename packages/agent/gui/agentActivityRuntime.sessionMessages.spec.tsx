import {
  act,
  fireEvent,
  render,
  renderHook,
  screen
} from "@testing-library/react";
import type {
  AgentActivityMessage,
  AgentActivitySnapshot
} from "@tutti-os/agent-activity-core";
import {
  Suspense,
  useLayoutEffect,
  useState,
  type PropsWithChildren
} from "react";
import { describe, expect, it } from "vitest";
import {
  AgentGUIRuntimeProvider,
  useAgentActivitySessionMessages,
  useAgentActivitySnapshot,
  type AgentGUIRuntime
} from "./agentActivityRuntime";

describe("useAgentActivitySessionMessages", () => {
  it("keeps typing and committed history available while a stream projection is pending", async () => {
    const store = createRuntimeStore(
      snapshot({ "session-a": [message("session-a", "before")] })
    );
    let release!: () => void;
    let ready = false;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    function CanonicalObservation() {
      const current = useAgentActivitySnapshot("workspace-1");
      return (
        <output data-testid="canonical-message">
          {String(current.sessionMessagesById["session-a"]?.[0]?.payload.text)}
        </output>
      );
    }
    function Conversation() {
      const messages = useAgentActivitySessionMessages("workspace-1", [
        "session-a"
      ]);
      const [draft, setDraft] = useState("");
      const body = messages["session-a"]?.[0]?.payload.text;
      if (body === "after" && !ready) throw pending;
      return (
        <>
          <input
            aria-label="draft"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
          />
          <p>{String(body)}</p>
        </>
      );
    }
    render(
      <AgentGUIRuntimeProvider runtime={store.runtime}>
        <CanonicalObservation />
        <Suspense fallback={<p>loading</p>}>
          <Conversation />
        </Suspense>
      </AgentGUIRuntimeProvider>
    );
    act(() =>
      store.publish(snapshot({ "session-a": [message("session-a", "after")] }))
    );
    expect(screen.queryByText("loading")).toBeNull();
    expect(screen.getByTestId("canonical-message")).toHaveTextContent("after");
    expect(screen.getByText("before")).toBeVisible();
    fireEvent.change(screen.getByRole("textbox", { name: "draft" }), {
      target: { value: "kept while streaming" }
    });
    expect(screen.getByRole("textbox")).toHaveValue("kept while streaming");
    await act(async () => {
      ready = true;
      release();
      await pending;
    });
    expect(screen.getByText("after", { selector: "p" })).toBeVisible();
    expect(screen.getByRole("textbox")).toHaveValue("kept while streaming");
  });

  it.each(["session", "workspace", "runtime"] as const)(
    "never commits old history after a %s identity change",
    (scope) => {
      const store = createRuntimeStore(
        snapshot({
          "session-a": [message("session-a", "A")],
          "session-b": [message("session-b", "B")]
        })
      );
      const otherStore = createRuntimeStore(
        snapshot({ "session-a": [message("session-a", "B")] })
      );
      const runtime = {
        ...store.runtime,
        getSnapshot: (workspace: string) =>
          workspace === "workspace-2"
            ? otherStore.runtime.getSnapshot(workspace)
            : store.runtime.getSnapshot(workspace)
      };
      const committed: Array<{ expected: string; actual: unknown }> = [];
      function Conversation({
        workspace,
        session,
        expected
      }: {
        workspace: string;
        session: string;
        expected: string;
      }) {
        const messages = useAgentActivitySessionMessages(workspace, [session]);
        useLayoutEffect(() => {
          committed.push({
            expected,
            actual: messages[session]?.[0]?.payload.text
          });
        });
        return null;
      }
      const rendered = render(
        <AgentGUIRuntimeProvider runtime={runtime}>
          <Conversation
            workspace="workspace-1"
            session="session-a"
            expected="A"
          />
        </AgentGUIRuntimeProvider>
      );
      rendered.rerender(
        <AgentGUIRuntimeProvider
          runtime={scope === "runtime" ? otherStore.runtime : runtime}
        >
          <Conversation
            workspace={scope === "workspace" ? "workspace-2" : "workspace-1"}
            session={scope === "session" ? "session-b" : "session-a"}
            expected="B"
          />
        </AgentGUIRuntimeProvider>
      );
      expect(committed.length).toBeGreaterThanOrEqual(2);
      for (const commit of committed)
        expect(commit.actual).toBe(commit.expected);
    }
  );

  it("delivers projected streaming text without rendering an unrelated Session", () => {
    const messagesA = [message("session-a", "partial")];
    const messagesB = [message("session-b", "settled")];
    const store = createRuntimeStore(
      snapshot({ "session-a": messagesA, "session-b": messagesB })
    );
    const wrapper = ({ children }: PropsWithChildren) => (
      <AgentGUIRuntimeProvider runtime={store.runtime}>
        {children}
      </AgentGUIRuntimeProvider>
    );
    let rendersA = 0;
    let rendersB = 0;
    const renderedA = renderHook(
      () => {
        rendersA += 1;
        return useAgentActivitySessionMessages("workspace-1", ["session-a"]);
      },
      { wrapper }
    );
    renderHook(
      () => {
        rendersB += 1;
        return useAgentActivitySessionMessages("workspace-1", ["session-b"]);
      },
      { wrapper }
    );
    const initialRendersA = rendersA;
    const initialRendersB = rendersB;

    act(() => {
      store.publish(
        snapshot({
          "session-a": [message("session-a", "partial text")],
          "session-b": messagesB
        })
      );
    });

    expect(renderedA.result.current["session-a"]?.[0]?.payload.text).toBe(
      "partial text"
    );
    expect(rendersA).toBeGreaterThan(initialRendersA);
    expect(rendersB).toBe(initialRendersB);
  });

  it("subscribes to projected messages for child Sessions in the family", () => {
    const rootMessages = [message("root", "root text")];
    const store = createRuntimeStore(
      snapshot({
        root: rootMessages,
        child: [message("child", "child partial")]
      })
    );
    const wrapper = ({ children }: PropsWithChildren) => (
      <AgentGUIRuntimeProvider runtime={store.runtime}>
        {children}
      </AgentGUIRuntimeProvider>
    );
    const rendered = renderHook(
      () => useAgentActivitySessionMessages("workspace-1", ["root", "child"]),
      { wrapper }
    );

    act(() => {
      store.publish(
        snapshot({
          root: rootMessages,
          child: [message("child", "child partial text")]
        })
      );
    });

    expect(rendered.result.current.child?.[0]?.payload.text).toBe(
      "child partial text"
    );
  });
});

function createRuntimeStore(initial: AgentActivitySnapshot) {
  let current = initial;
  const listeners = new Set<() => void>();
  const runtime = {
    getSnapshot: () => current,
    origin: "test",
    subscribe(_workspaceId: string, listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    }
  } as unknown as AgentGUIRuntime;
  return {
    publish(next: AgentActivitySnapshot) {
      current = next;
      for (const listener of listeners) listener();
    },
    runtime
  };
}

function snapshot(
  sessionMessagesById: Record<string, readonly AgentActivityMessage[]>
): AgentActivitySnapshot {
  return { sessionMessagesById } as AgentActivitySnapshot;
}

function message(agentSessionId: string, text: string): AgentActivityMessage {
  return {
    agentSessionId,
    kind: "text",
    messageId: `message-${agentSessionId}`,
    occurredAtUnixMs: 1,
    payload: { text },
    role: "assistant",
    sequence: 1,
    status: "streaming",
    turnId: "turn-1",
    version: 1,
    workspaceId: "workspace-1"
  };
}
