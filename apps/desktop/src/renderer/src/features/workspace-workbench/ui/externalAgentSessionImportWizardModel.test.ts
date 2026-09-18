import assert from "node:assert/strict";
import test from "node:test";
import {
  externalImportGroupsFromScan,
  externalImportRequestSource,
  externalImportScanDecision,
  externalImportScanRequest,
  externalImportScanSource,
  externalImportScanStateReducer,
  externalImportSelectionProjects,
  externalImportUsableScan,
  filterExternalImportGroups,
  isExternalImportArchiveMode,
  isExternalImportWizardBusy,
  pruneExternalImportDeselections,
  shouldAllowExternalImportDialogOpenChange
} from "./externalAgentSessionImportWizardModel.ts";

test("archive source keeps the same path and kind for scan and disables project registration", () => {
  assert.equal(isExternalImportArchiveMode(" /tmp/claude-export.zip "), true);
  const source = externalImportScanSource({
    archiveKind: "claude",
    archivePath: " /tmp/claude-export.zip ",
    days: -1,
    providers: ["codex", "claude-code"]
  });
  assert.deepEqual(externalImportScanRequest(source), {
    archivePath: "/tmp/claude-export.zip",
    archiveKind: "claude",
    days: -1
  });
  assert.deepEqual(
    externalImportRequestSource(" /tmp/claude-export.zip ", "claude", true),
    {
      archivePath: "/tmp/claude-export.zip",
      archiveKind: "claude",
      registerUserProjects: false
    }
  );
});

test("chatgpt archive source threads its kind through scan and import requests", () => {
  const source = externalImportScanSource({
    archiveKind: "chatgpt",
    archivePath: "/tmp/chatgpt-export.zip",
    days: -1,
    providers: []
  });
  assert.deepEqual(externalImportScanRequest(source), {
    archivePath: "/tmp/chatgpt-export.zip",
    archiveKind: "chatgpt",
    days: -1
  });
  assert.deepEqual(
    externalImportRequestSource("/tmp/chatgpt-export.zip", "chatgpt", true),
    {
      archivePath: "/tmp/chatgpt-export.zip",
      archiveKind: "chatgpt",
      registerUserProjects: false
    }
  );
});

test("local source keeps providers and the project registration preference", () => {
  assert.equal(isExternalImportArchiveMode(null), false);
  assert.deepEqual(
    externalImportScanRequest(
      externalImportScanSource({
        archiveKind: "claude",
        archivePath: null,
        days: 30,
        providers: ["codex"]
      })
    ),
    { days: 30, providers: ["codex"] }
  );
  assert.deepEqual(externalImportRequestSource(null, null, false), {
    registerUserProjects: false
  });
});

test("completed scan is usable only for its exact source identity", () => {
  const response = {
    errors: [],
    projects: [],
    providers: [],
    scannedMessages: 0,
    scannedSessions: 0,
    sessions: [],
    skippedSessions: 0
  };
  const source = externalImportScanSource({
    archiveKind: "claude",
    archivePath: "/tmp/claude-export-a.zip",
    days: -1,
    providers: ["codex", "claude-code"]
  });
  const state = externalImportScanStateReducer(null, {
    type: "scan-succeeded",
    response,
    source
  });

  assert.deepEqual(externalImportUsableScan(state, source)?.sessions, []);
  assert.equal(
    externalImportUsableScan(
      state,
      externalImportScanSource({
        archiveKind: "claude",
        archivePath: "/tmp/claude-export-b.zip",
        days: -1,
        providers: ["codex", "claude-code"]
      })
    ),
    null
  );
  assert.equal(
    externalImportUsableScan(
      state,
      externalImportScanSource({
        archiveKind: "chatgpt",
        archivePath: "/tmp/claude-export-a.zip",
        days: -1,
        providers: ["codex", "claude-code"]
      })
    ),
    null,
    "same path but a different archive kind must not reuse the scan"
  );
  assert.equal(
    externalImportUsableScan(
      state,
      externalImportScanSource({
        archiveKind: "claude",
        archivePath: null,
        days: -1,
        providers: ["codex", "claude-code"]
      })
    ),
    null
  );
});

test("starting or failing a scan keeps the completed snapshot; changing source clears it", () => {
  const response = {
    errors: [],
    projects: [],
    providers: [],
    scannedMessages: 0,
    scannedSessions: 0,
    sessions: [],
    skippedSessions: 0
  };
  const source = externalImportScanSource({
    archiveKind: "claude",
    archivePath: "/tmp/claude-export.zip",
    days: -1,
    providers: []
  });
  const completed = externalImportScanStateReducer(null, {
    type: "scan-succeeded",
    response,
    source
  });

  assert.equal(
    externalImportScanStateReducer(completed, { type: "scan-started" })
      ?.snapshot,
    completed?.snapshot
  );
  assert.equal(
    externalImportScanStateReducer(completed, { type: "scan-failed" })
      ?.snapshot,
    completed?.snapshot
  );
  assert.equal(
    externalImportScanStateReducer(completed, { type: "source-changed" }),
    null
  );
});

test("a 30-day snapshot covers a 7-day window without another request", () => {
  const now = 1_800_000_000_000;
  const source30 = externalImportScanSource({
    archiveKind: "claude",
    archivePath: null,
    days: 30,
    providers: ["codex"]
  });
  const oldSession = {
    id: "old",
    lastUpdatedAtUnixMs: now - 20 * 24 * 60 * 60 * 1000,
    messageCount: 2,
    projectPath: "/tmp/p",
    provider: "codex" as const,
    sourcePath: "/tmp/old.jsonl",
    title: "old"
  };
  const newSession = {
    id: "new",
    lastUpdatedAtUnixMs: now - 2 * 24 * 60 * 60 * 1000,
    messageCount: 1,
    projectPath: "/tmp/p",
    provider: "codex" as const,
    sourcePath: "/tmp/new.jsonl",
    title: "new"
  };
  const state = externalImportScanStateReducer(null, {
    type: "scan-succeeded",
    response: {
      complete: true,
      cutoffUnixMs: now - 30 * 24 * 60 * 60 * 1000,
      scannedAtUnixMs: now,
      errors: [],
      projects: [],
      providers: [
        {
          available: true,
          messageCount: 3,
          provider: "codex",
          root: "/tmp",
          sessionCount: 2
        }
      ],
      scannedMessages: 3,
      scannedSessions: 2,
      sessions: [oldSession, newSession],
      skippedSessions: 1
    },
    source: source30
  });
  const source7 = externalImportScanSource({
    archiveKind: "claude",
    archivePath: null,
    days: 7,
    providers: ["codex"]
  });
  assert.equal(
    externalImportScanDecision(state, source7, now).type,
    "reuse"
  );
  const projected = externalImportUsableScan(state, source7, now);
  assert.deepEqual(
    projected?.sessions.map((session) => session.id),
    ["new"]
  );
  assert.equal(projected?.scannedSessions, 1);
  assert.equal(projected?.skippedSessions, 1);
  assert.equal(
    externalImportScanDecision(state, source30, now).type,
    "reuse"
  );
  const source90 = externalImportScanSource({
    archiveKind: "claude",
    archivePath: null,
    days: 90,
    providers: ["codex"]
  });
  assert.equal(
    externalImportScanDecision(state, source90, now).type,
    "request"
  );
  assert.deepEqual(
    pruneExternalImportDeselections(new Set(["old", "gone"]), [
      "old",
      "new"
    ]),
    new Set(["old"])
  );
});

test("archive groups use their source label and preserve selected session ids", () => {
  const scan = {
    errors: [],
    projects: [
      {
        label: "demo",
        messageCount: 3,
        path: "/Users/demo",
        providers: ["claude-code" as const],
        sessionCount: 2
      }
    ],
    providers: [],
    scannedMessages: 3,
    scannedSessions: 2,
    sessions: [
      {
        ...{
          activeTurnId: null,
          latestTurnInteractions: [],
          pendingInteractions: []
        },
        id: "session-new",
        lastUpdatedAtUnixMs: 200,
        messageCount: 2,
        projectPath: "/Users/demo",
        provider: "claude-code" as const,
        sourcePath: "/tmp/claude-export.zip",
        title: "New conversation"
      },
      {
        id: "session-old",
        lastUpdatedAtUnixMs: 100,
        messageCount: 1,
        projectPath: "/Users/demo",
        provider: "claude-code" as const,
        sourcePath: "/tmp/claude-export.zip",
        title: "Old conversation"
      }
    ],
    skippedSessions: 0
  };
  const groups = externalImportGroupsFromScan(
    scan,
    () => "fallback",
    "Claude chats"
  );
  assert.equal(groups[0]?.label, "Claude chats");
  assert.deepEqual(
    filterExternalImportGroups(groups, "new")[0]?.sessions.map(
      (session) => session.id
    ),
    ["session-new"]
  );
  assert.deepEqual(
    externalImportSelectionProjects(scan.sessions, new Set(["session-old"])),
    [
      {
        path: "/Users/demo",
        providers: ["claude-code"],
        sessionIds: ["session-new"]
      }
    ]
  );
});

test("wizard is not busy when idle", () => {
  assert.equal(
    isExternalImportWizardBusy({ importing: false, loading: false }),
    false
  );
});

test("wizard is busy while scanning", () => {
  assert.equal(
    isExternalImportWizardBusy({ importing: false, loading: true }),
    true
  );
});

test("wizard is busy while importing", () => {
  assert.equal(
    isExternalImportWizardBusy({ importing: true, loading: false }),
    true
  );
});

test("blocks the X close button (onOpenChange(false)) while importing", () => {
  assert.equal(
    shouldAllowExternalImportDialogOpenChange({
      importing: true,
      loading: false,
      nextOpen: false
    }),
    false
  );
});

test("blocks the X close button (onOpenChange(false)) while scanning", () => {
  assert.equal(
    shouldAllowExternalImportDialogOpenChange({
      importing: false,
      loading: true,
      nextOpen: false
    }),
    false
  );
});

test("allows the X close button (onOpenChange(false)) when idle", () => {
  assert.equal(
    shouldAllowExternalImportDialogOpenChange({
      importing: false,
      loading: false,
      nextOpen: false
    }),
    true
  );
});

test("allows the X close button once importing finishes and a result is shown", () => {
  // handleImport's finally block clears `importing` before/along with
  // setResult, so by the time the result screen is visible the wizard is
  // idle again and dismissal must not be trapped.
  assert.equal(
    shouldAllowExternalImportDialogOpenChange({
      importing: false,
      loading: false,
      nextOpen: false
    }),
    true
  );
});

test("never blocks opening the dialog, even if somehow called while busy", () => {
  assert.equal(
    shouldAllowExternalImportDialogOpenChange({
      importing: true,
      loading: true,
      nextOpen: true
    }),
    true
  );
});
