import type {
  TerminalDataEvent,
  TerminalExitEvent,
  TerminalMetadataEvent,
  TerminalSnapshot,
  TerminalStateEvent,
  TerminalTransport
} from "../contracts/index.ts";
import { createTerminalAttachmentController } from "./attachmentController.ts";

export interface TerminalSessionTransportRuntime {
  attach(afterSeq?: number): Promise<void>;
  onData(listener: (event: TerminalDataEvent) => void): () => void;
  onExit(listener: (event: TerminalExitEvent) => void): () => void;
  onMetadata(listener: (event: TerminalMetadataEvent) => void): () => void;
  onState(listener: (event: TerminalStateEvent) => void): () => void;
  release(): void;
  snapshot(): Promise<TerminalSnapshot>;
}

interface TerminalSessionTransportRuntimeEntry {
  attach(afterSeq?: number): Promise<void>;
  dataBuffer: TerminalDataEvent[];
  dataListeners: Set<(event: TerminalDataEvent) => void>;
  detachGraceMs: number;
  detachTimer: ReturnType<typeof setTimeout> | null;
  exitBuffer: TerminalExitEvent[];
  exitListeners: Set<(event: TerminalExitEvent) => void>;
  metadataBuffer: TerminalMetadataEvent[];
  metadataListeners: Set<(event: TerminalMetadataEvent) => void>;
  refCount: number;
  release(): void;
  sessionId: string;
  snapshot(): Promise<TerminalSnapshot>;
  snapshotCache: TerminalSnapshot | null;
  snapshotPromise: Promise<TerminalSnapshot> | null;
  stateBuffer: TerminalStateEvent[];
  stateListeners: Set<(event: TerminalStateEvent) => void>;
  teardown(): void;
  transport: TerminalTransport;
}

const defaultDetachGraceMs = 200;
const runtimeRegistry = new Map<string, TerminalSessionTransportRuntimeEntry>();
let runtimeClientSequence = 0;

export function acquireTerminalSessionTransportRuntime(input: {
  detachGraceMs?: number;
  sessionId: string;
  transport: TerminalTransport;
}): TerminalSessionTransportRuntime {
  const currentEntry = runtimeRegistry.get(input.sessionId);
  const entry =
    currentEntry?.transport === input.transport
      ? currentEntry
      : createTerminalSessionTransportRuntimeEntry(input);
  if (entry !== currentEntry) {
    runtimeRegistry.set(input.sessionId, entry);
  }
  entry.refCount += 1;
  if (entry.detachTimer !== null) {
    clearTimeout(entry.detachTimer);
    entry.detachTimer = null;
  }

  return {
    attach(afterSeq) {
      return entry.attach(afterSeq);
    },
    onData(listener) {
      entry.dataListeners.add(listener);
      flushBufferedEvents(entry.dataBuffer, listener);
      return () => {
        entry.dataListeners.delete(listener);
      };
    },
    onExit(listener) {
      entry.exitListeners.add(listener);
      flushBufferedEvents(entry.exitBuffer, listener);
      return () => {
        entry.exitListeners.delete(listener);
      };
    },
    onMetadata(listener) {
      entry.metadataListeners.add(listener);
      flushBufferedEvents(entry.metadataBuffer, listener);
      return () => {
        entry.metadataListeners.delete(listener);
      };
    },
    onState(listener) {
      entry.stateListeners.add(listener);
      flushBufferedEvents(entry.stateBuffer, listener);
      return () => {
        entry.stateListeners.delete(listener);
      };
    },
    release() {
      entry.release();
    },
    snapshot() {
      return entry.snapshot();
    }
  };
}

function createTerminalSessionTransportRuntimeEntry(input: {
  detachGraceMs?: number;
  sessionId: string;
  transport: TerminalTransport;
}): TerminalSessionTransportRuntimeEntry {
  const attachmentController = createTerminalAttachmentController({
    clientId: createTerminalTransportClientId(),
    sessionId: input.sessionId,
    transport: input.transport
  });

  const entry: TerminalSessionTransportRuntimeEntry = {
    attach(afterSeq) {
      return attachmentController.attach(afterSeq);
    },
    dataBuffer: [],
    dataListeners: new Set(),
    detachGraceMs: input.detachGraceMs ?? defaultDetachGraceMs,
    detachTimer: null,
    exitBuffer: [],
    exitListeners: new Set(),
    metadataBuffer: [],
    metadataListeners: new Set(),
    refCount: 0,
    release() {
      entry.refCount = Math.max(0, entry.refCount - 1);
      if (entry.refCount > 0 || entry.detachTimer !== null) {
        return;
      }
      entry.detachTimer = setTimeout(() => {
        entry.detachTimer = null;
        if (entry.refCount > 0) {
          return;
        }
        void attachmentController.detach().finally(() => {
          entry.teardown();
          if (runtimeRegistry.get(entry.sessionId) === entry) {
            runtimeRegistry.delete(entry.sessionId);
          }
        });
      }, entry.detachGraceMs);
    },
    sessionId: input.sessionId,
    snapshot() {
      if (entry.snapshotPromise) {
        return entry.snapshotPromise;
      }
      if (entry.snapshotCache) {
        return Promise.resolve(entry.snapshotCache);
      }
      entry.snapshotPromise = input.transport
        .snapshot({ sessionId: input.sessionId })
        .then((snapshot) => {
          entry.snapshotCache = snapshot;
          return snapshot;
        })
        .finally(() => {
          entry.snapshotPromise = null;
        });
      return entry.snapshotPromise;
    },
    snapshotCache: null,
    snapshotPromise: null,
    stateBuffer: [],
    stateListeners: new Set(),
    teardown() {
      disposeData();
      disposeState();
      disposeExit();
      disposeMetadata?.();
      entry.dataBuffer.length = 0;
      entry.exitBuffer.length = 0;
      entry.metadataBuffer.length = 0;
      entry.stateBuffer.length = 0;
      entry.snapshotCache = null;
      entry.snapshotPromise = null;
    },
    transport: input.transport
  };

  const disposeData = input.transport.onData((event) => {
    if (event.sessionId !== input.sessionId) {
      return;
    }
    entry.snapshotCache = null;
    dispatchOrBuffer(entry.dataListeners, entry.dataBuffer, event);
  });
  const disposeState = input.transport.onState((event) => {
    if (event.sessionId !== input.sessionId) {
      return;
    }
    if (event.status === "detached" || event.status === "exited") {
      attachmentController.markDetached();
    }
    if (
      typeof event.gapStartSeq === "number" ||
      typeof event.gapEndSeq === "number"
    ) {
      entry.snapshotCache = null;
    }
    dispatchOrBuffer(entry.stateListeners, entry.stateBuffer, event);
  });
  const disposeExit = input.transport.onExit((event) => {
    if (event.sessionId !== input.sessionId) {
      return;
    }
    dispatchOrBuffer(entry.exitListeners, entry.exitBuffer, event);
  });
  const disposeMetadata = input.transport.onMetadata?.((event) => {
    if (event.sessionId !== input.sessionId) {
      return;
    }
    dispatchOrBuffer(entry.metadataListeners, entry.metadataBuffer, event);
  });

  return entry;
}

function createTerminalTransportClientId(): string {
  runtimeClientSequence += 1;
  return `terminal-runtime-${runtimeClientSequence}`;
}

function dispatchOrBuffer<TEvent>(
  listeners: Set<(event: TEvent) => void>,
  buffer: TEvent[],
  event: TEvent
) {
  if (listeners.size === 0) {
    buffer.push(event);
    return;
  }
  for (const listener of listeners) {
    listener(event);
  }
}

function flushBufferedEvents<TEvent>(
  buffer: TEvent[],
  listener: (event: TEvent) => void
) {
  if (buffer.length === 0) {
    return;
  }
  const pending = buffer.splice(0);
  for (const event of pending) {
    listener(event);
  }
}
