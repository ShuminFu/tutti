import type {
  TerminalNodeLimits,
  TerminalWriteEncoding
} from "../contracts/index.ts";
import type { TerminalNodeFeature } from "./feature.ts";
import {
  createBufferedTerminalInputQueue,
  type BufferedTerminalInputQueue
} from "./inputQueue.ts";
import {
  createTerminalSessionRecovery,
  type TerminalSessionRecoveryHandle
} from "./sessionControllerRecovery.ts";
import { createTerminalSessionRecoveryDiagnostics } from "./sessionDiagnostics.ts";
import {
  createTerminalSessionControllerStore,
  type TerminalSessionControllerState,
  type TerminalSessionControllerStore
} from "./sessionControllerStore.ts";
import {
  acquireTerminalSessionTransportRuntime,
  type TerminalSessionTransportRuntime
} from "./sessionTransportRuntime.ts";

export type { TerminalSessionControllerState } from "./sessionControllerStore.ts";

export interface TerminalSessionController {
  getState(): TerminalSessionControllerState;
  retain(): void;
  release(): void;
  resize(input: { cols: number; rows: number }): Promise<void>;
  subscribe(listener: () => void): () => void;
  write(data: string, encoding?: TerminalWriteEncoding): void;
}

interface TerminalSessionControllerEntry {
  context: TerminalSessionControllerContext;
  disposed: boolean;
  inputQueue: BufferedTerminalInputQueue;
  recovery: TerminalSessionRecoveryHandle;
  refCount: number;
  release(): void;
  retainTimer: ReturnType<typeof setTimeout> | null;
  runtimeWriteReady: boolean;
  startPromise: Promise<void> | null;
  store: TerminalSessionControllerStore;
  teardown(): void;
  transportRuntime: TerminalSessionTransportRuntime;
}

interface TerminalSessionControllerContext {
  feature: TerminalNodeFeature;
  nodeId: string;
  sessionId: string;
}

const defaultControllerRetainMs = 5_000;
const controllerRegistry = new Map<string, TerminalSessionControllerEntry>();

export function acquireTerminalSessionController(input: {
  feature: TerminalNodeFeature;
  nodeId: string;
  retainMs?: number;
  sessionId: string;
}): TerminalSessionController {
  const currentEntry = controllerRegistry.get(input.sessionId);
  // Host contribution refreshes can replace the terminal feature and its
  // transport while keeping the same persisted session id. Reusing the old
  // controller would keep recovery bound to the stale transport but route
  // writes through the new one, leaving the terminal permanently detached.
  const entry =
    currentEntry?.context.feature === input.feature
      ? currentEntry
      : createTerminalSessionControllerEntry({
          feature: input.feature,
          nodeId: input.nodeId,
          retainMs: input.retainMs ?? defaultControllerRetainMs,
          sessionId: input.sessionId
        });
  if (entry !== currentEntry) {
    controllerRegistry.set(input.sessionId, entry);
  }

  entry.context = {
    feature: input.feature,
    nodeId: input.nodeId,
    sessionId: input.sessionId
  };

  return {
    getState() {
      return entry.store.getState();
    },
    retain() {
      entry.refCount += 1;
      if (entry.retainTimer !== null) {
        clearTimeout(entry.retainTimer);
        entry.retainTimer = null;
      }
      ensureRecoveryStarted(entry);
    },
    release() {
      entry.release();
    },
    resize({ cols, rows }) {
      return entry.context.feature.transport.resize({
        cols,
        rows,
        sessionId: entry.context.sessionId
      });
    },
    subscribe(listener) {
      return entry.store.subscribe(listener);
    },
    write(data, encoding = "utf8") {
      if (!entry.runtimeWriteReady) {
        entry.inputQueue.enqueue(data, encoding);
        ensureRecoveryStarted(entry);
        return;
      }
      void writeTerminalInput(entry, data, encoding);
    }
  };
}

function createTerminalSessionControllerEntry(input: {
  feature: TerminalNodeFeature;
  nodeId: string;
  retainMs: number;
  sessionId: string;
}) {
  const transportRuntime = acquireTerminalSessionTransportRuntime({
    sessionId: input.sessionId,
    transport: input.feature.transport
  });
  const inputQueue = createBufferedTerminalInputQueue();
  const store = createTerminalSessionControllerStore({
    maxScrollbackChars: resolveMaxScrollbackChars(input.feature.limits)
  });

  const entry = {
    context: {
      feature: input.feature,
      nodeId: input.nodeId,
      sessionId: input.sessionId
    },
    disposed: false,
    inputQueue,
    recovery: null as unknown as TerminalSessionRecoveryHandle,
    refCount: 0,
    release: () => undefined,
    retainTimer: null,
    runtimeWriteReady: false,
    startPromise: null,
    store,
    teardown: () => undefined,
    transportRuntime
  } as TerminalSessionControllerEntry;

  entry.release = () => {
    entry.refCount = Math.max(0, entry.refCount - 1);
    if (entry.refCount > 0 || entry.retainTimer !== null) {
      return;
    }
    entry.retainTimer = setTimeout(() => {
      entry.retainTimer = null;
      if (entry.refCount > 0) {
        return;
      }
      entry.teardown();
      if (controllerRegistry.get(entry.context.sessionId) === entry) {
        controllerRegistry.delete(entry.context.sessionId);
      }
    }, input.retainMs);
  };

  entry.teardown = () => {
    if (entry.disposed) {
      return;
    }
    entry.disposed = true;
    inputQueue.reset();
    entry.runtimeWriteReady = false;
    entry.recovery.dispose();
    transportRuntime.release();
    store.clearListeners();
  };

  entry.recovery = createTerminalSessionRecovery({
    diagnostics: createTerminalSessionRecoveryDiagnostics({
      diagnostics: input.feature.diagnostics,
      nodeId: input.nodeId,
      sessionId: input.sessionId
    }),
    inputQueue,
    onRecoverableDetach() {
      if (entry.disposed || entry.refCount === 0) {
        return;
      }
      ensureRecoveryStarted(entry);
    },
    onWriteReadyChange(ready) {
      if (entry.disposed) {
        return;
      }
      entry.runtimeWriteReady = ready;
    },
    replayGapMessage: input.feature.i18n.t("recovery.replayGap"),
    sessionId: input.sessionId,
    snapshotTruncatedMessage: input.feature.i18n.t(
      "recovery.snapshotTruncated"
    ),
    store,
    transport: input.feature.transport,
    transportRuntime
  });

  return entry;
}

function ensureRecoveryStarted(entry: TerminalSessionControllerEntry) {
  if (
    entry.disposed ||
    entry.startPromise !== null ||
    entry.runtimeWriteReady
  ) {
    return;
  }
  entry.startPromise = entry.recovery.start().finally(() => {
    entry.startPromise = null;
  });
}

function resolveMaxScrollbackChars(limits: TerminalNodeLimits) {
  return Math.max(limits.maxScrollbackLines * 160, 400_000);
}

function writeTerminalInput(
  entry: TerminalSessionControllerEntry,
  data: string,
  encoding: TerminalWriteEncoding
) {
  return entry.context.feature.transport
    .write({
      data,
      encoding,
      provenance: "user",
      sessionId: entry.context.sessionId
    })
    .catch((error: unknown) => {
      entry.runtimeWriteReady = false;
      entry.store.setInputReady(false);
      entry.inputQueue.enqueue(data, encoding);
      ensureRecoveryStarted(entry);
      entry.context.feature.diagnostics.log("write-error", {
        nodeId: entry.context.nodeId,
        sessionId: entry.context.sessionId
      });
      entry.store.setSurfaceError(errorMessage(error));
    });
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}
