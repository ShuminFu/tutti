import assert from "node:assert/strict";
import test from "node:test";
import {
  createReactRootErrorLogger,
  createRenderStormTracker,
  installBrowserCrashLogging
} from "./reactDiagnostics.ts";

test("react root error logger prints crash diagnostics without filtering by message", () => {
  const calls: Array<{ method: string; args: unknown[] }> = [];
  const diagnostics: unknown[] = [];
  const logger = createReactRootErrorLogger({
    captureOwnerStack: () => "\n    at Owner",
    console: {
      error: (...args: unknown[]) => calls.push({ method: "error", args }),
      groupCollapsed: (...args: unknown[]) =>
        calls.push({ method: "groupCollapsed", args }),
      groupEnd: () => calls.push({ method: "groupEnd", args: [] }),
      info: (...args: unknown[]) => calls.push({ method: "info", args }),
      warn: (...args: unknown[]) => calls.push({ method: "warn", args })
    },
    logRendererDiagnostic(input) {
      diagnostics.push(input);
    }
  });

  const error = new Error("ordinary crash");
  logger("uncaught", error, { componentStack: "\n    at Problem" });

  assert.deepEqual(
    calls.map((call) => call.method),
    ["groupCollapsed", "error", "info", "info", "groupEnd"]
  );
  assert.match(String(calls[0]?.args[0]), /\[tutti:react:uncaught\]/u);
  assert.equal(calls[1]?.args[0], error);
  assert.match(String(calls[2]?.args[0]), /componentStack/u);
  assert.match(String(calls[3]?.args[0]), /ownerStack/u);
  assert.deepEqual(diagnostics, [
    {
      details: {
        componentStack: "at Problem",
        message: "ordinary crash",
        name: "Error",
        ownerStack: "at Owner",
        stack: error.stack
      },
      event: "react.uncaught",
      level: "error",
      source: "react-diagnostics"
    }
  ]);
});

test("render storm tracker logs recent commits when a subtree exceeds the threshold", () => {
  const calls: Array<{ method: string; args: unknown[] }> = [];
  const diagnostics: unknown[] = [];
  const tracker = createRenderStormTracker({
    captureStack: () => "Error\n    at render storm",
    console: {
      error: (...args: unknown[]) => calls.push({ method: "error", args }),
      groupCollapsed: (...args: unknown[]) =>
        calls.push({ method: "groupCollapsed", args }),
      groupEnd: () => calls.push({ method: "groupEnd", args: [] }),
      info: (...args: unknown[]) => calls.push({ method: "info", args }),
      warn: (...args: unknown[]) => calls.push({ method: "warn", args })
    },
    logRendererDiagnostic(input) {
      diagnostics.push(input);
    },
    threshold: 3,
    windowMs: 1000
  });

  tracker.record({
    actualDuration: 1,
    baseDuration: 2,
    commitTime: 10,
    id: "RendererApp",
    phase: "update",
    startTime: 9
  });
  tracker.record({
    actualDuration: 1,
    baseDuration: 2,
    commitTime: 20,
    id: "RendererApp",
    phase: "update",
    startTime: 19
  });
  tracker.record({
    actualDuration: 1,
    baseDuration: 2,
    commitTime: 30,
    id: "RendererApp",
    phase: "update",
    startTime: 29
  });

  assert.equal(calls[0]?.method, "groupCollapsed");
  assert.match(String(calls[0]?.args[0]), /render storm/u);
  assert.equal(
    calls.some((call) => call.args.includes("recent commits")),
    true
  );
  assert.equal(
    calls.some((call) => String(call.args[0]).includes("stack")),
    true
  );
  assert.equal(diagnostics.length, 1);
  assert.deepEqual(diagnostics[0], {
    details: {
      commitCount: 3,
      latestCommit: {
        actualDuration: 1,
        baseDuration: 2,
        commitTime: 30,
        phase: "update",
        startTime: 29
      },
      recentCommits: [
        {
          actualDuration: 1,
          baseDuration: 2,
          commitTime: 10,
          phase: "update",
          startTime: 9
        },
        {
          actualDuration: 1,
          baseDuration: 2,
          commitTime: 20,
          phase: "update",
          startTime: 19
        },
        {
          actualDuration: 1,
          baseDuration: 2,
          commitTime: 30,
          phase: "update",
          startTime: 29
        }
      ],
      stack: "Error\n    at render storm",
      windowMs: 1000
    },
    event: "react.render_storm",
    level: "warn",
    source: "react-diagnostics"
  });
});

// DinTalDock: WebKit reports the benign ResizeObserver warning via window "error";
// it must not become a red renderer diagnostic, but real errors still must.
test("browser crash logging drops the benign ResizeObserver loop warning only", () => {
  const listeners = new Map<string, (event: unknown) => void>();
  const fakeWindow = {
    addEventListener: (type: string, fn: (event: unknown) => void) =>
      listeners.set(type, fn),
    removeEventListener: () => {}
  } as unknown as Window;
  const diagnostics: Array<{ event: string }> = [];
  const noop = () => {};
  installBrowserCrashLogging({
    console: { error: noop, groupCollapsed: noop, groupEnd: noop, info: noop, warn: noop },
    logRendererDiagnostic(input) {
      diagnostics.push(input as { event: string });
    },
    window: fakeWindow
  });
  const fire = (init: { error?: unknown; message: string }) => {
    let prevented = false;
    listeners.get("error")?.({
      colno: 0,
      filename: "",
      lineno: 0,
      ...init,
      preventDefault: () => {
        prevented = true;
      }
    });
    return prevented;
  };

  assert.equal(
    fire({ error: null, message: "ResizeObserver loop completed with undelivered notifications." }),
    true
  );
  assert.equal(fire({ error: undefined, message: "ResizeObserver loop limit exceeded" }), true);
  assert.equal(diagnostics.length, 0);

  // A real crash, and a thrown Error that merely quotes the text, are still logged.
  assert.equal(fire({ error: new Error("boom"), message: "boom" }), false);
  assert.equal(
    fire({
      error: new Error("ResizeObserver loop completed with undelivered notifications"),
      message: "ResizeObserver loop completed with undelivered notifications"
    }),
    false
  );
  assert.deepEqual(
    diagnostics.map((entry) => entry.event),
    ["runtime.error", "runtime.error"]
  );
});
