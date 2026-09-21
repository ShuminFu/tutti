import { act, render, screen } from "@testing-library/react";
import type { JSX } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentGUIRuntime } from "../../../agentActivityRuntime";
import { AgentMessageBlock } from "./AgentMessageBlock";
import type {
  AgentMessageContentVM,
  AgentMessageRowVM
} from "../contracts/agentMessageRowVM";

vi.mock("../../../i18n/index", () => ({
  getActiveUiLanguage: () => "en",
  useTranslation: () => ({
    t: (key: string) => key
  }),
  translate: (key: string) => key
}));

vi.mock("../../AgentRichTextReadonly", () => ({
  AgentRichTextReadonly: ({ value }: { value?: string }) => (
    <div>{value ?? "User message"}</div>
  )
}));

afterEach(() => {
  delete (window as { agentGUIRuntime?: unknown }).agentGUIRuntime;
});

const wait = (ms: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms));

// 独立挂载组件：行数据从响应式的 revision 派生，每次 revision 变化都以全新对象
// 身份重建同一张图片——模拟流式回合期间会话快照不断落地、父级对同一批图片反复
// 重投影（DINTAL-5328 的发送→回复窗口）。注意每次重渲染要独立 act 提交：同一
// tick 内的连续 root.render 会被 React 19 合并成一次提交，中间渲染不会落地。
function PendingTurnUserImageRow({
  revision
}: {
  revision: number;
}): JSX.Element {
  const imageMessage: AgentMessageContentVM = {
    kind: "message-content",
    id: "user-images-1",
    turnId: "turn-1",
    body: "",
    presentationKind: "content",
    contentKind: "image-grid",
    images: [
      {
        id: "attachment-1",
        workspaceId: "room-1",
        agentSessionId: "session-1",
        attachmentId: "attachment-1",
        mimeType: "image/png",
        name: "screen.png"
      }
    ],
    // 时间戳随快照变化：仅让对象身份轮转，图片定位内容保持不变。
    occurredAtUnixMs: revision
  };
  const row: AgentMessageRowVM = {
    kind: "message",
    id: "message-row-user-1",
    turnId: "turn-1",
    speaker: "user",
    messages: [imageMessage],
    thinking: [],
    occurredAtUnixMs: 1
  };
  return (
    <AgentMessageBlock
      workspaceRoot="/workspace/demo"
      basePath="/workspace/demo"
      row={row}
      thinkingLabel="Thought process"
    />
  );
}

describe("AgentMessageImages pending-turn convergence", () => {
  it("keeps one attachment read alive across identity-only re-renders and converges the placeholder", async () => {
    let resolveRead!: (attachment: {
      attachmentId: string;
      data: string;
      mimeType: "image/png";
      name: string;
    }) => void;
    const readSessionAttachment = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveRead = resolve;
        })
    );
    Object.defineProperty(window, "agentGUIRuntime", {
      configurable: true,
      value: { readSessionAttachment } as Partial<AgentGUIRuntime>
    });

    const { rerender } = render(<PendingTurnUserImageRow revision={0} />);

    for (let revision = 1; revision <= 6; revision += 1) {
      await act(async () => {
        rerender(<PendingTurnUserImageRow revision={revision} />);
        await wait(2);
      });
    }
    await act(async () => {
      resolveRead({
        attachmentId: "attachment-1",
        data: "aW1hZ2U=",
        mimeType: "image/png",
        name: "screen.png"
      });
      await wait(2);
    });

    const image = await screen.findByRole("img", { name: "screen.png" });
    expect(image).toHaveAttribute("src", "data:image/png;base64,aW1hZ2U=");
    // 消融断言：图片集合内容未变的重渲染，不得取消并重启在途附件读取。
    expect(readSessionAttachment).toHaveBeenCalledTimes(1);
  });
});
