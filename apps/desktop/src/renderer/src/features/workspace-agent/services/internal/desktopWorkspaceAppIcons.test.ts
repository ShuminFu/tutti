import assert from "node:assert/strict";
import test from "node:test";
import { resolveRendererWorkspaceAppDefaultIconUrl } from "../../../../assets/desktopMentionIconAssets.ts";
import { resolveDesktopWorkspaceAppIconEntries } from "./desktopWorkspaceAppIcons.ts";

test("desktop workspace app icon entries use App Center icon fields", () => {
  const entries = resolveDesktopWorkspaceAppIconEntries({
    apps: [
      {
        appId: "automation",
        availableIconUrl: "available-automation.png",
        iconUrl: "stored-automation.png"
      },
      {
        appId: "notes",
        availableIconUrl: "available-notes.png",
        iconUrl: null
      }
    ],
    workspaceId: "workspace-1"
  });

  assert.deepEqual(entries, [
    {
      appId: "automation",
      iconUrl: "stored-automation.png",
      workspaceId: "workspace-1"
    },
    {
      appId: "notes",
      iconUrl: "available-notes.png",
      workspaceId: "workspace-1"
    },
    {
      appId: "agent-codex",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("agent-codex"),
      workspaceId: "workspace-1"
    },
    {
      appId: "agent-claude-code",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("agent-claude-code"),
      workspaceId: "workspace-1"
    },
    {
      appId: "agent-tutti-agent",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("agent-tutti-agent"),
      workspaceId: "workspace-1"
    },
    {
      appId: "issue-manager",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("issue-manager"),
      workspaceId: "workspace-1"
    }
  ]);
});

test("desktop workspace app icon entries seed built-in agent app icons", () => {
  const entries = resolveDesktopWorkspaceAppIconEntries({
    apps: [],
    workspaceId: "workspace-1"
  });

  assert.deepEqual(entries, [
    {
      appId: "agent-codex",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("agent-codex"),
      workspaceId: "workspace-1"
    },
    {
      appId: "agent-claude-code",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("agent-claude-code"),
      workspaceId: "workspace-1"
    },
    {
      appId: "agent-tutti-agent",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("agent-tutti-agent"),
      workspaceId: "workspace-1"
    },
    {
      appId: "issue-manager",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("issue-manager"),
      workspaceId: "workspace-1"
    }
  ]);
});

test("desktop workspace app icon entries keep App Center agent icons", () => {
  const entries = resolveDesktopWorkspaceAppIconEntries({
    apps: [
      {
        appId: "agent-codex",
        availableIconUrl: "tutti-asset://agent/codex.png",
        iconUrl: "stored-agent-codex.png"
      }
    ],
    workspaceId: "workspace-1"
  });

  assert.deepEqual(entries, [
    {
      appId: "agent-codex",
      iconUrl: "stored-agent-codex.png",
      workspaceId: "workspace-1"
    },
    {
      appId: "agent-claude-code",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("agent-claude-code"),
      workspaceId: "workspace-1"
    },
    {
      appId: "agent-tutti-agent",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("agent-tutti-agent"),
      workspaceId: "workspace-1"
    },
    {
      appId: "issue-manager",
      iconUrl: resolveRendererWorkspaceAppDefaultIconUrl("issue-manager"),
      workspaceId: "workspace-1"
    }
  ]);
});
