import assert from "node:assert/strict";
import test from "node:test";
import { ToolActivityProjector } from "./toolActivity.ts";
import { SessionRuntime } from "./sessionRuntime.ts";
import { sidecarClaudeOptionsFromPayload } from "./options.ts";
import { withSidecarEventSinkForTest } from "./eventSink.ts";
import { testCanUseToolOptions } from "./sessionRuntimeTestCommon.ts";
import { waitForEvent } from "./sessionRuntimeTestQueries.nested.ts";

test("background results keep their launch identity across new turns and late progress", () => {
  let turnId = "first-turn";
  const events: Array<{ type: string; payload?: Record<string, unknown> }> = [];
  const activity = new ToolActivityProjector(
    () => turnId,
    (event) => events.push(event)
  );
  activity.handleTaskSystemMessage("task_started", {
    tool_use_id: "bash-1",
    task_id: "process-1",
    description: "Compile project"
  });
  turnId = "second-turn";
  activity.handleTaskSystemMessage("task_progress", {
    task_id: "process-1",
    summary: "Compiling"
  });
  activity.handleTaskSystemMessage("task_updated", {
    task_id: "process-1",
    status: "failed",
    error: "compiler error"
  });
  activity.handleTaskSystemMessage("task_progress", {
    task_id: "process-1",
    summary: "late progress"
  });
  activity.handleTaskSystemMessage("task_notification", {
    task_id: "process-1",
    status: "failed",
    summary: "Build failed",
    exit_code: 2,
    output_file: "/tmp/build.output"
  });
  assert.equal(events.length, 4);
  assert.ok(
    events.every(
      (event) =>
        event.type === "background_process_updated" &&
        event.payload?.turnId === "first-turn" &&
        event.payload?.toolCallId === "background:process-1"
    )
  );
  assert.deepEqual(
    events.map((event) => event.payload?.status),
    ["running", "running", "failed", "failed"]
  );
  assert.deepEqual(events.at(-1)?.payload?.output, {
    status: "failed",
    text: "Build failed",
    exitCode: 2,
    outputFile: "/tmp/build.output"
  });
  assert.equal(activity.resolveDelegatedTaskIdForStop("process-1", ""), "");
  activity.close();
});

test("unidentified task completion cannot settle the only running subagent", () => {
  const events: Array<{ type: string; payload?: Record<string, unknown> }> = [];
  const activity = new ToolActivityProjector(
    () => "root-turn",
    (event) => events.push(event)
  );
  activity.upsertToolUse(
    { id: "agent-1", name: "Agent", input: { prompt: "Investigate" } },
    undefined,
    "tool_started"
  );
  activity.handleUserContentBlock({
    type: "tool_result",
    tool_use_id: "agent-1",
    content: [{ type: "text", text: "Agent launched" }]
  });
  events.length = 0;
  activity.handleTaskSystemMessage("task_notification", {
    task_id: "unrelated-process",
    status: "completed"
  });
  assert.equal(events.length, 0);
  activity.close();
});

test("provider plan entry revokes stale bypass and plan exit follows observed mode", async () => {
  const events: Array<{ type: string; payload?: Record<string, unknown> }> = [];
  let session: SessionRuntime;
  let permission: unknown;
  const settings = {
    model: "",
    permissionModeId: "bypassPermissions",
    planMode: false,
    effort: "",
    speed: ""
  };
  const restore = withSidecarEventSinkForTest((event) => {
    events.push(event);
    if (event.type === "approval_requested") {
      session.submitInteractive(
        String(event.payload?.turnId),
        String(event.payload?.requestId),
        "submit",
        "deny_once",
        {}
      );
    }
  });
  session = new SessionRuntime(
    "provider-mode",
    "/repo",
    {},
    false,
    false,
    settings,
    sidecarClaudeOptionsFromPayload({}),
    undefined,
    ({ prompt, options }) => ({
      async *[Symbol.asyncIterator]() {
        const outbound = await prompt[Symbol.asyncIterator]().next();
        yield {
          ...outbound.value,
          uuid: "provider-turn",
          type: "user",
          parent_tool_use_id: null,
          session_id: "provider-mode"
        } as never;
        yield {
          type: "system",
          subtype: "status",
          status: null,
          permissionMode: "plan"
        } as never;
        permission = await options.canUseTool?.(
          "Write",
          { file_path: "/repo/example", content: "change" },
          testCanUseToolOptions({
            requestId: "write-request",
            toolUseID: "write-1"
          })
        );
        yield {
          type: "system",
          subtype: "status",
          status: null,
          permissionMode: "acceptEdits"
        } as never;
        yield { type: "result", subtype: "success" } as never;
      },
      close() {}
    })
  );
  try {
    await session.start();
    session.exec(
      "mode-turn",
      "Plan the change",
      undefined,
      undefined,
      undefined,
      "",
      "mode-correlation"
    );
    await waitForEvent(events, "turn_completed");
    assert.equal((permission as { behavior: string }).behavior, "deny");
    assert.deepEqual(
      events
        .filter((event) => event.type === "permission_mode_updated")
        .map((event) => event.payload?.permissionMode),
      ["plan", "acceptEdits"]
    );
    assert.equal(settings.planMode, false);
    assert.equal(settings.permissionModeId, "acceptEdits");
  } finally {
    await session.close();
    restore();
  }
});
