import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AgentMessageBlock } from "./AgentMessageBlock";
import { AgentEnvPanelActionProvider } from "../../agentEnv";
import {
  parsePairCard,
  splitLeadingPairCard,
  stripLeadingPairKickoff,
  stripLeadingPairKickoffFromContent
} from "./pairKickoffEnvelope";
import { projectAgentMessageFinalText } from "../projection/agentMessageFinalTextProjection";
import type { AgentMessageRowVM } from "../contracts/agentMessageRowVM";

// 分栏结对模式（peer-pair-mode 票 05）：<pair-kickoff> / <pair-ended> 的解析与卡片。

const PROTOCOL = "用 peer_send 与搭档沟通。\n审查者回 APPROVED 后双方停下向用户汇报。";

function kickoff(
  attrs: Record<string, string>,
  body: string
): string {
  const attr = Object.entries(attrs)
    .map(([k, v]) => `${k}="${v}"`)
    .join(" ");
  return `<pair-kickoff ${attr}>\n${body}\n</pair-kickoff>`;
}

const SENDER_ATTRS = {
  role: "developer",
  partner: "codex-b12",
  partner_provider: "codex",
  partner_role: "reviewer",
  reason: "start"
};

const PARTNER_ATTRS = {
  role: "reviewer",
  partner: "claude-a34",
  partner_provider: "claude",
  partner_role: "developer",
  reason: "start"
};

function envelope(body: string): string {
  return `Another claude session sent a message:\n<peer-message from="task-a" from-name="claude-a34" from-provider="claude" kind="info">\n${body}\n</peer-message>\nThis came from another agent session — not typed by your user, but very likely working on their behalf.`;
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

describe("pair card parser", () => {
  it("reads the sender's leading kickoff block and keeps the original prompt", () => {
    const parsed = splitLeadingPairCard(
      `${kickoff(SENDER_ATTRS, PROTOCOL)}\n\n修一下登录页\n第二行`
    );
    expect(parsed?.rest).toBe("修一下登录页\n第二行");
    expect(parsed?.card).toEqual({
      kind: "kickoff",
      role: "developer",
      partner: "codex-b12",
      partnerProvider: "codex",
      partnerRole: "reviewer",
      reason: "start",
      goal: null,
      protocol: PROTOCOL
    });
  });

  it("reads the partner's kickoff with a goal line and roles_changed reason", () => {
    const card = parsePairCard(
      kickoff({ ...PARTNER_ATTRS, reason: "roles_changed" }, `目标：修一下登录页\n${PROTOCOL}`)
    );
    expect(card).toMatchObject({
      kind: "kickoff",
      role: "reviewer",
      reason: "roles_changed",
      goal: "修一下登录页",
      protocol: PROTOCOL
    });
  });

  it("reads pair-ended and unescapes attribute values", () => {
    expect(
      parsePairCard(
        '<pair-ended partner="a&quot;&lt;b&gt;&amp;c">\n结对已结束，回到独立模式。\n</pair-ended>'
      )
    ).toEqual({ kind: "ended", partner: 'a"<b>&c', body: "结对已结束，回到独立模式。" });
  });

  it("cuts at the first real closing tag (backend rewrites </pair-kickoff in goals to < /…)", () => {
    const card = parsePairCard(
      kickoff(PARTNER_ATTRS, `目标：别写 < /pair-kickoff> 这种字\n${PROTOCOL}`)
    );
    expect(card?.kind === "kickoff" ? card.goal : null).toBe("别写 < /pair-kickoff> 这种字");
  });

  // 评审补充 4：目标是用户原话，可以多行；按后端模板切到「结对模式开始：」/「结对角色已调整：」之前。
  it("reads a multi-line goal up to the protocol head line", () => {
    const head = "结对模式开始：你是审查者，claude-a34（claude） 是开发者。给对方发消息用 peer_send，to 填 claude-a34。";
    const card = parsePairCard(
      kickoff(PARTNER_ATTRS, `目标：修一下登录页\n第二行：别动注册页\n\n第四行\n${head}\n${PROTOCOL}`)
    );
    expect(card?.kind === "kickoff" ? card.goal : null).toBe(
      "修一下登录页\n第二行：别动注册页\n\n第四行"
    );
    expect(card?.kind === "kickoff" ? card.protocol : null).toBe(`${head}\n${PROTOCOL}`);

    const changed = parsePairCard(
      kickoff(
        { ...PARTNER_ATTRS, reason: "roles_changed" },
        `目标：a\nb\n结对角色已调整：你是开发者。\n${PROTOCOL}`
      )
    );
    expect(changed?.kind === "kickoff" ? changed.goal : null).toBe("a\nb");
  });

  it("a goal that itself mentions the head phrase still ends at the template's head line", () => {
    const card = parsePairCard(
      kickoff(PARTNER_ATTRS, `目标：把\n结对模式开始：这句提示改短\n结对模式开始：你是审查者。\n${PROTOCOL}`)
    );
    expect(card?.kind === "kickoff" ? card.goal : null).toBe("把\n结对模式开始：这句提示改短");
    expect(card?.kind === "kickoff" ? card.protocol : null).toBe(`结对模式开始：你是审查者。\n${PROTOCOL}`);
  });

  // 评审 E：回到输入框 / 剪贴板时只要原话。
  it("strips only a leading kickoff block from text and from the first text block of content", () => {
    const withCard = `${kickoff(SENDER_ATTRS, PROTOCOL)}\n\n修一下登录页`;
    expect(stripLeadingPairKickoff(withCard)).toBe("修一下登录页");
    expect(stripLeadingPairKickoff("普通一句")).toBe("普通一句");
    const ended = '<pair-ended partner="x">\n结对已结束，回到独立模式。\n</pair-ended>';
    expect(stripLeadingPairKickoff(ended)).toBe(ended);

    const image = { type: "image" as const, url: "blob:1" };
    expect(
      stripLeadingPairKickoffFromContent([image, { type: "text", text: withCard }])
    ).toEqual([image, { type: "text", text: "修一下登录页" }]);
    const plain = [{ type: "text", text: "普通一句" }];
    expect(stripLeadingPairKickoffFromContent(plain)).toBe(plain);
  });

  it("rejects malformed input", () => {
    // 没闭合
    expect(parsePairCard(`<pair-kickoff role="developer">\n${PROTOCOL}`)).toBeNull();
    // 角色不认识
    expect(parsePairCard(kickoff({ ...SENDER_ATTRS, role: "boss" }, PROTOCOL))).toBeNull();
    // 不在开头
    expect(splitLeadingPairCard(`你好 ${kickoff(SENDER_ATTRS, PROTOCOL)}`)).toBeNull();
    // 整段不止一张卡（parsePairCard 要求正文就是卡）
    expect(parsePairCard(`${kickoff(SENDER_ATTRS, PROTOCOL)}\n多余的字`)).toBeNull();
    // 标签名错配
    expect(parsePairCard(`<pair-kickoff role="developer">\nx\n</pair-ended>`)).toBeNull();
    expect(parsePairCard("please explain <pair-kickoff> to me")).toBeNull();
  });
});

// 测试环境的界面语言是英文，文案按 en 字典断言。
describe("AgentMessageBlock pair card", () => {
  it("sender pane: collapsed card with role, raw tags hidden, original prompt below", () => {
    const { getByTestId, container } = renderRow(
      userRow(`${kickoff(SENDER_ATTRS, PROTOCOL)}\n\n修一下登录页`)
    );
    expect(getByTestId("agent-pair-card-title").textContent).toBe(
      "Pairing started · Your role: Developer"
    );
    expect(getByTestId("agent-pair-card-partner").textContent).toContain("codex-b12");
    expect(container.textContent).toContain("修一下登录页");
    expect(container.textContent).not.toContain("<pair-kickoff");
    expect(container.textContent).not.toContain("APPROVED");
  });

  it("partner pane: kickoff inside a peer envelope renders as a pair card with the goal line", async () => {
    const { getByTestId, queryByTestId, container } = renderRow(
      userRow(envelope(kickoff(PARTNER_ATTRS, `目标：修一下登录页\n${PROTOCOL}`)))
    );
    expect(queryByTestId("agent-peer-message-card")).toBeNull();
    expect(getByTestId("agent-pair-card-title").textContent).toBe(
      "Pairing started · Your role: Reviewer"
    );
    expect(getByTestId("agent-pair-card-goal").textContent).toBe("Goal: 修一下登录页");
    expect(container.textContent).not.toContain("<pair-kickoff");
    fireEvent.click(getByTestId("agent-pair-card-toggle"));
    await waitFor(() =>
      expect(getByTestId("agent-pair-card-body").textContent).toBe(PROTOCOL)
    );
  });

  it("pair-ended inside a peer envelope renders the ended card", () => {
    const { getByTestId } = renderRow(
      userRow(
        envelope('<pair-ended partner="claude-a34">\n结对已结束，回到独立模式。\n</pair-ended>')
      )
    );
    expect(getByTestId("agent-pair-card").getAttribute("data-pair-card-kind")).toBe("ended");
    expect(getByTestId("agent-pair-card-title").textContent).toBe("Pairing ended");
  });

  it("copy text of the sender's first message is the original words only", () => {
    const row = userRow(`${kickoff(SENDER_ATTRS, PROTOCOL)}\n\n修一下登录页`);
    const [projected] = projectAgentMessageFinalText([row], { turns: [] } as never);
    expect(
      projected?.kind === "message" ? projected.messages[0]?.copyText : undefined
    ).toBe("修一下登录页");
  });

  it("editing the sender's first message prefills only the original words", () => {
    const body = `${kickoff(SENDER_ATTRS, PROTOCOL)}\n\n修一下登录页`;
    const { getByRole } = render(
      <AgentEnvPanelActionProvider openPanel={() => {}}>
        <AgentMessageBlock
          workspaceRoot={null}
          basePath="/"
          row={{ ...userRow(body), rawFirstTextBlock: body }}
          editRetry={{
            pending: false,
            labels: { edit: "Edit message", cancel: "Cancel", submit: "Save and retry" },
            onSubmit: async () => true
          }}
          thinkingLabel="thinking"
        />
      </AgentEnvPanelActionProvider>
    );
    fireEvent.click(getByRole("button", { name: "Edit message" }));
    expect(
      (getByRole("textbox", { name: "Edit message" }) as HTMLTextAreaElement).value
    ).toBe("修一下登录页");
  });

  it("malformed kickoff text stays on the rich text path", () => {
    const { queryByTestId, container } = renderRow(
      userRow(`<pair-kickoff role="developer">\n没闭合`)
    );
    expect(queryByTestId("agent-pair-card")).toBeNull();
    expect(container.textContent).toContain("没闭合");
  });
});
