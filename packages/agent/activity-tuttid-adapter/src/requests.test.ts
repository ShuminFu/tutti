import assert from "node:assert/strict";
import test from "node:test";
import type {
  AgentPromptContentBlock,
  AgentSessionActivateEffectInput
} from "@tutti-os/agent-activity-core";
import {
  tuttiCreateWorkspaceAgentSessionRequestFromActivation,
  tuttiCreateWorkspaceAgentSessionRequestFromActivity,
  tuttiSendWorkspaceAgentSessionInputRequestFromActivity
} from "./index.ts";

test("create and send projections share one prompt allowlist", () => {
  const content: AgentPromptContentBlock[] = [
    activityTextBlock(),
    {
      type: "file",
      name: "report.pdf",
      path: "/runtime/report.pdf",
      sizeBytes: 0,
      hostPath: "/tmp/report.pdf",
      mimeType: "application/pdf",
      uploadStatus: "uploaded"
    }
  ];
  const activityCreate = tuttiCreateWorkspaceAgentSessionRequestFromActivity({
    agentSessionId: "session-1",
    agentTargetId: "target-1",
    clientSubmitId: "submit-1",
    initialContent: content,
    workspaceId: "workspace-1"
  });
  const activationCreate =
    tuttiCreateWorkspaceAgentSessionRequestFromActivation({
      agentSessionId: "session-1",
      agentTargetId: "target-1",
      clientSubmitId: "submit-1",
      initialContent: content,
      isolation: "worktree",
      mode: "new",
      settings: {
        browserUse: true,
        codexSaverMode: true,
        computerUse: true
      },
      workspaceId: "workspace-1"
    } satisfies AgentSessionActivateEffectInput);
  const send = tuttiSendWorkspaceAgentSessionInputRequestFromActivity({
    agentSessionId: "session-1",
    capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
    clientSubmitId: "submit-1",
    content,
    workspaceId: "workspace-1"
  });

  for (const projected of [
    activityCreate.initialContent,
    activationCreate.initialContent,
    send.content
  ]) {
    assert.deepEqual(projected, [
      { text: "hello", type: "text" },
      {
        type: "file",
        name: "report.pdf",
        path: "/runtime/report.pdf",
        sizeBytes: 0
      }
    ]);
  }
  assert.equal(activationCreate.browserUse, true);
  assert.equal(activationCreate.codexSaverMode, true);
  assert.equal(activationCreate.isolation, "worktree");
  assert.equal("computerUse" in activationCreate, false);
  assert.deepEqual(send.capabilityRefs, [
    { capability: "tutti", source: "slash_command" }
  ]);
});

test("request projection rejects non-canonical file blocks", () => {
  for (const block of [
    { hostPath: "/tmp/file.txt", type: "file" as const },
    { data: "ZmlsZQ==", name: "file.txt", type: "file" as const },
    { name: "empty.txt", path: "  ", type: "file" as const },
    { name: "relative.txt", path: "relative.txt", type: "file" as const },
    {
      name: "negative.txt",
      path: "/runtime/negative.txt",
      sizeBytes: -1,
      type: "file" as const
    }
  ]) {
    assert.throws(
      () =>
        tuttiSendWorkspaceAgentSessionInputRequestFromActivity({
          agentSessionId: "session-1",
          clientSubmitId: "submit-1",
          content: [block],
          workspaceId: "workspace-1"
        }),
      /File prompt blocks must be uploaded before submission/
    );
  }
});

test("request projection carries the exact target only for guidance", () => {
  const guidance = tuttiSendWorkspaceAgentSessionInputRequestFromActivity({
    agentSessionId: "session-1",
    clientSubmitId: "submit-guidance",
    content: [activityTextBlock()],
    guidance: true,
    targetTurnId: "  turn-target  ",
    workspaceId: "workspace-1"
  });
  assert.equal(guidance.guidance, true);
  assert.equal(guidance.turnId, "turn-target");

  const ordinary = tuttiSendWorkspaceAgentSessionInputRequestFromActivity({
    agentSessionId: "session-1",
    clientSubmitId: "submit-ordinary",
    content: [activityTextBlock()],
    targetTurnId: "turn-ignored",
    workspaceId: "workspace-1"
  });
  assert.equal("guidance" in ordinary, false);
  assert.equal("turnId" in ordinary, false);
});

test("request projection preserves a structured local connector selection", () => {
  const projected = tuttiSendWorkspaceAgentSessionInputRequestFromActivity({
    agentSessionId: "session-1",
    clientSubmitId: "submit-connector",
    content: [
      { text: "list my calendar events", type: "text" },
      { connectorKey: "lark-cli", type: "connector" }
    ],
    displayPrompt: "/lark-cli list my calendar events",
    workspaceId: "workspace-1"
  });
  assert.deepEqual(projected.content, [
    { text: "list my calendar events", type: "text" },
    { connectorKey: "lark-cli", type: "connector" }
  ]);
  assert.equal(projected.displayPrompt, "/lark-cli list my calendar events");
});

function activityTextBlock(): AgentPromptContentBlock {
  return {
    assetId: "asset-1",
    hostPath: "/tmp/local-only.txt",
    kind: "local-text",
    sizeBytes: 5,
    text: "hello",
    type: "text",
    uploadStatus: "uploaded",
    uri: "file:///tmp/local-only.txt"
  };
}
