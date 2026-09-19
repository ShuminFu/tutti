import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentMessageMarkdown } from "./AgentMessageMarkdown";
import { formatAttachmentSize } from "./AgentMessageFileCard";
const { reveal, info, read, download } = vi.hoisted(() => ({
  reveal: vi.fn().mockResolvedValue({ fallbackToDirectory: false }),
  info: vi.fn().mockResolvedValue({ sizeBytes: 6585057 }),
  read: vi.fn(),
  download: vi.fn().mockResolvedValue(undefined)
}));
vi.mock("../agentActivityHost", () => ({
  useOptionalAgentHostApi: () => ({
    filesystem: {
      revealInFolder: reveal,
      getFileInfo: info,
      downloadFile: download
    },
    workspace: { readFile: read },
    meta: { platform: "darwin" }
  }),
  getOptionalAgentHostApi: () => null
}));
describe("assistant file delivery", () => {
  it("retains links, deduplicates files, reveals the resolved path without changing headings or lists", async () => {
    const { container } = render(
      <AgentMessageMarkdown
        fileCards
        content={
          "[Download](./out.zip)\n\n[Again][zip]\n\n### Packaging scope\n\n- Full log\n\n[zip]: ./out.zip"
        }
        workspaceLinkContext={{
          basePath: "/tmp",
          workspaceRoot: "/tmp",
          source: "agent-markdown"
        }}
      />
    );
    expect(container.querySelectorAll("[data-agent-file-card]")).toHaveLength(
      1
    );
    expect(screen.getByRole("link", { name: "Download" })).toBeInTheDocument();
    expect(container.querySelector("details")).toBeNull();
    expect(
      screen.getByRole("heading", { name: "Packaging scope" })
    ).toBeInTheDocument();
    await screen.findByText("6.28 MB");
    expect(read).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Reveal in Finder" }));
    await waitFor(() =>
      expect(reveal).toHaveBeenCalledWith({ path: "/tmp/out.zip" })
    );
  });
  it("uses the native download capability without reading preview bytes", async () => {
    render(
      <AgentMessageMarkdown fileCards content="[Download](/tmp/save.zip)" />
    );
    fireEvent.click(screen.getByRole("button", { name: "Download file" }));
    await waitFor(() =>
      expect(download).toHaveBeenCalledWith({ path: "/tmp/save.zip" })
    );
    expect(read).not.toHaveBeenCalled();
  });
  it("does not make cards from remote links, source paths, mentions or code samples", () => {
    const { container } = render(
      <AgentMessageMarkdown
        fileCards
        content={
          "[Remote](https://example.org/a.zip) [Code](/tmp/a.ts) [@f.zip](/tmp/f.zip)\n\n```md\n[x](/tmp/example.zip)\n```"
        }
      />
    );
    expect(container.querySelector("[data-agent-file-card]")).toBeNull();
  });
  it("formats zero and varied file sizes without changing units prematurely", () => {
    expect([0, 1023, 1024, 1048576].map(formatAttachmentSize)).toEqual([
      "0 B",
      "1023 B",
      "1 KB",
      "1 MB"
    ]);
  });
});
