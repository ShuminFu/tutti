import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ConversationFileContextMenu } from "./ConversationFileContextMenu";

const { revealInFolderMock, openPathMock } = vi.hoisted(() => ({
  revealInFolderMock: vi.fn(),
  openPathMock: vi.fn()
}));

vi.mock("../../../agentActivityHost", () => ({
  useOptionalAgentHostApi: () => ({
    filesystem: {
      revealInFolder: revealInFolderMock,
      openPath: openPathMock
    },
    meta: { platform: "darwin" },
    toast: { error: vi.fn(), info: vi.fn() }
  })
}));

describe("ConversationFileContextMenu", () => {
  afterEach(() => {
    revealInFolderMock.mockReset();
    openPathMock.mockReset();
  });

  it("reveals the generated file in Finder from the context menu", async () => {
    revealInFolderMock.mockResolvedValue({
      fallbackToDirectory: false,
      path: "/tmp/out.md"
    });

    render(
      <ConversationFileContextMenu path="/tmp/out.md">
        <span data-testid="file">out.md</span>
      </ConversationFileContextMenu>
    );

    fireEvent.contextMenu(screen.getByTestId("file"));
    const revealItem = await screen.findByText("Reveal in Finder");
    fireEvent.pointerDown(revealItem, { button: 0 });

    await waitFor(() => {
      expect(revealInFolderMock).toHaveBeenCalledWith({ path: "/tmp/out.md" });
    });
  });

  it("surfaces reveal failures instead of swallowing them", async () => {
    revealInFolderMock.mockRejectedValue(
      new Error("file not found: /tmp/missing.md")
    );

    render(
      <ConversationFileContextMenu path="/tmp/missing.md">
        <span data-testid="file">missing.md</span>
      </ConversationFileContextMenu>
    );

    fireEvent.contextMenu(screen.getByTestId("file"));
    fireEvent.pointerDown(await screen.findByText("Reveal in Finder"), {
      button: 0
    });

    await waitFor(() => {
      expect(revealInFolderMock).toHaveBeenCalled();
    });
  });

  it("opens the file with the default app from the context menu", async () => {
    openPathMock.mockResolvedValue(undefined);

    render(
      <ConversationFileContextMenu path="/tmp/out.md">
        <span data-testid="file">out.md</span>
      </ConversationFileContextMenu>
    );

    fireEvent.contextMenu(screen.getByTestId("file"));
    const openItem = await screen.findByText("Open");
    fireEvent.pointerDown(openItem, { button: 0 });

    await waitFor(() => {
      expect(openPathMock).toHaveBeenCalledWith({ path: "/tmp/out.md" });
    });
  });
});
