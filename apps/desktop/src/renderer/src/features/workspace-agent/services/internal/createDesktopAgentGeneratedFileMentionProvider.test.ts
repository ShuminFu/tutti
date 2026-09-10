import assert from "node:assert/strict";
import test from "node:test";
import {
  workspaceFileMentionIconUrl,
  workspaceFolderMentionIconUrl
} from "../../../../assets/desktopMentionIconAssets.ts";
import { createDesktopAgentGeneratedFileMentionProvider } from "./createDesktopAgentGeneratedFileMentionProvider.ts";

test("generated file mentions use renderer assets in web-compatible URLs", async () => {
  const provider = createDesktopAgentGeneratedFileMentionProvider({
    agentActivityRuntime: {
      async listAgentGeneratedFiles() {
        return {
          entries: [
            { label: "report.md", path: "/workspace/report.md" },
            { label: "output", path: "/workspace/output/" }
          ],
          hasMore: false,
          workspaceId: "workspace-1"
        };
      }
    },
    workspaceId: "workspace-1"
  });

  const items = await provider.query({
    context: { metadata: { sectionKey: "project:/workspace" } },
    keyword: "",
    trigger: "@"
  });
  assert.equal(
    provider.getItemIconUrl?.(items[0]!),
    workspaceFileMentionIconUrl
  );
  assert.equal(
    provider.getItemIconUrl?.(items[1]!),
    workspaceFolderMentionIconUrl
  );
  assert.equal(workspaceFileMentionIconUrl.startsWith("tutti-asset:"), false);
  assert.equal(workspaceFolderMentionIconUrl.startsWith("tutti-asset:"), false);
});
