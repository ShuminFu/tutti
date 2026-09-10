import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AgentMessageBlock } from "./AgentMessageBlock";
import { AgentEnvPanelActionProvider } from "../../agentEnv";
import { parsePeerMessageEnvelope, peerMessageSenderLabel } from "./peerMessageEnvelope";
import type { AgentMessageRowVM } from "../contracts/agentMessageRowVM";

const FOOTNOTE =
  "This came from another agent session — not typed by your user, but very likely working on their behalf.";

function envelope(blocks: string[]): string {
  return `Another claude session sent a message:\n${blocks.join("\n")}\n${FOOTNOTE}`;
}

function block(attrs: Record<string, string>, body: string): string {
  const attr = Object.entries(attrs)
    .map(([k, v]) => `${k}="${v}"`)
    .join(" ");
  return `<peer-message ${attr}>\n${body}\n</peer-message>`;
}

function userRow(body: string): AgentMessageRowVM {
  return {
    kind: "message",
    id: "row-1",
    turnId: "turn-1",
    speaker: "user",
    occurredAtUnixMs: 0,
    thinking: [],
    messages: [
      {
        kind: "message-content",
        id: "msg-1",
        turnId: "turn-1",
        body,
        presentationKind: "content",
        occurredAtUnixMs: 0,
        visibleError: null
      }
    ]
  };
}

function renderRow(row: AgentMessageRowVM) {
  return render(
    <AgentEnvPanelActionProvider openPanel={() => {}}>
      <AgentMessageBlock
        workspaceRoot={null}
        basePath="/"
        row={row}
        thinkingLabel="thinking"
      />
    </AgentEnvPanelActionProvider>
  );
}

const TWO_LINE_BODY = "临时插一脚：先别管 README 了。\n算一下 1 到 100 的质数和。";

describe("peer message envelope parser", () => {
  it("splits a merged envelope into one block per <peer-message>", () => {
    const blocks = parsePeerMessageEnvelope(
      envelope([
        block({ from: "task-a", "from-name": "alice", "from-provider": "claude", kind: "info" }, "first"),
        block({ from: "task-b", "from-name": "", "from-provider": "codex", kind: "question" }, "second")
      ])
    );
    expect(blocks.map((b) => [b.from, b.kind, b.body])).toEqual([
      ["task-a", "info", "first"],
      ["task-b", "question", "second"]
    ]);
    expect(peerMessageSenderLabel(blocks[0]!)).toBe("alice");
    expect(peerMessageSenderLabel(blocks[1]!)).toBe("codex · task-b");
  });

  it("ignores ordinary user text that merely mentions the tag", () => {
    expect(parsePeerMessageEnvelope("please explain <peer-message> to me")).toEqual([]);
  });
});

describe("AgentMessageBlock peer message card", () => {
  it("renders the envelope as a collapsed card instead of the raw text", () => {
    const { getByTestId, queryByText, container } = renderRow(
      userRow(envelope([block({ from: "093cccba-1111", "from-name": "drv7", "from-provider": "claude", kind: "info" }, TWO_LINE_BODY)]))
    );
    // 卡片在，原文的抬头与脚注不在——它们是给模型的，不是给人的。
    getByTestId("agent-peer-message-card");
    expect(container.textContent).not.toContain("session sent a message");
    expect(container.textContent).not.toContain(FOOTNOTE);
    expect(container.textContent).not.toContain("<peer-message");
    expect(container.textContent).toContain("drv7");
    // 折叠态只有第一行预览。
    expect(getByTestId("agent-peer-message-preview").textContent).toBe("临时插一脚：先别管 README 了。");
    expect(queryByText(/质数和/)).toBeNull();
    expect(getByTestId("agent-peer-message-toggle").getAttribute("aria-expanded")).toBe("false");
  });

  it("expands to the full body on click", async () => {
    const { getByTestId, queryByTestId } = renderRow(
      userRow(envelope([block({ from: "093cccba-1111", "from-name": "drv7", "from-provider": "claude", kind: "info" }, TWO_LINE_BODY)]))
    );
    fireEvent.click(getByTestId("agent-peer-message-toggle"));
    expect(getByTestId("agent-peer-message-toggle").getAttribute("aria-expanded")).toBe("true");
    expect(queryByTestId("agent-peer-message-preview")).toBeNull();
    // CollapsibleReveal 下一帧才挂正文。
    await waitFor(() =>
      expect(getByTestId("agent-peer-message-body").textContent).toBe(TWO_LINE_BODY)
    );
  });

  it("renders one card per block of a merged delivery", () => {
    const { getAllByTestId } = renderRow(
      userRow(
        envelope([
          block({ from: "task-a", "from-name": "a", "from-provider": "claude", kind: "info" }, "one"),
          block({ from: "task-b", "from-name": "b", "from-provider": "claude", kind: "request" }, "two")
        ])
      )
    );
    const cards = getAllByTestId("agent-peer-message-card");
    expect(cards).toHaveLength(2);
    expect(cards[1]!.getAttribute("data-peer-message-kind")).toBe("request");
  });

  it("keeps the card list width-bound so it cannot outgrow the message column", () => {
    // 父级 grid 是 justify-items: end，格子宽度 auto 时按 max-content 撑开；
    // 折叠预览是 nowrap，max-content = 整行原文。少了 w-full 卡片就溢出到视口外。
    const { getByTestId } = renderRow(
      userRow(envelope([block({ from: "task-a", "from-name": "a", "from-provider": "claude", kind: "info" }, TWO_LINE_BODY)]))
    );
    expect(getByTestId("agent-peer-message-cards").className.split(/\s+/)).toContain("w-full");
  });

  it("leaves ordinary user text on the rich text path", () => {
    const { queryByTestId, container } = renderRow(userRow("just a normal prompt"));
    expect(queryByTestId("agent-peer-message-card")).toBeNull();
    expect(container.textContent).toContain("just a normal prompt");
  });
});
