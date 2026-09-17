import type { ClaudeSDKSidecarEventEmitter } from "./protocol.ts";

export type AssistantSegmentKind = "assistant" | "thinking";

type AssistantSegmentState = {
  readonly messageId: string;
  readonly messageBase: string;
  readonly kind: AssistantSegmentKind;
  readonly source: "live" | "fallback";
  snapshot: string;
  completed: boolean;
  usageEmitted: boolean;
};

export class AssistantStreamProjector {
  private readonly activeTurnId: () => string;
  private readonly emit: ClaudeSDKSidecarEventEmitter;
  private currentMessageBase = "";
  private sequence = 0;
  private pendingUsage: Record<string, unknown> | undefined;
  private readonly segmentsByKey = new Map<string, AssistantSegmentState>();
  private readonly segmentKeyByIndex = new Map<string, string>();

  constructor(activeTurnId: () => string, emit: ClaudeSDKSidecarEventEmitter) {
    this.activeTurnId = activeTurnId;
    this.emit = emit;
  }

  reset(): void {
    this.currentMessageBase = "";
    this.sequence = 0;
    this.pendingUsage = undefined;
    this.segmentsByKey.clear();
    this.segmentKeyByIndex.clear();
  }

  setPendingUsage(usage: Record<string, unknown> | undefined): void {
    this.pendingUsage = usage;
  }

  // Re-emit already-completed assistant rows when transcript usage arrives
  // after content_block_stop, so payload.usage is not dropped.
  flushCompletedAssistantUsage(): void {
    if (!this.pendingUsage) {
      return;
    }
    for (const segment of this.segmentsByKey.values()) {
      if (
        segment.kind !== "assistant" ||
        !segment.completed ||
        !segment.snapshot ||
        segment.usageEmitted
      ) {
        continue;
      }
      this.emit({
        type: "assistant_completed",
        payload: {
          turnId: this.activeTurnId(),
          messageId: segment.messageId,
          content: segment.snapshot,
          usage: this.pendingUsage
        }
      });
      segment.usageEmitted = true;
    }
  }

  setMessageBase(messageId: string): void {
    this.currentMessageBase = messageId;
  }

  start(index: number | undefined, kind: AssistantSegmentKind): void {
    this.ensureLiveSegment(index, kind);
  }

  appendDelta(
    index: number | undefined,
    kind: AssistantSegmentKind,
    delta: string
  ): void {
    this.emitDelta(this.ensureLiveSegment(index, kind), delta);
  }

  completeIndex(index: number | undefined): boolean {
    const segment = this.segmentForIndex(index);
    if (!segment) {
      return false;
    }
    this.completeSegment(index, segment);
    return true;
  }

  completeContent(
    kind: AssistantSegmentKind,
    messageBase: string,
    content: string,
    usedSegmentIds: Set<string>
  ): void {
    if (!content) {
      return;
    }
    const existing = this.segmentWithContent(
      kind,
      messageBase,
      content,
      usedSegmentIds
    );
    if (existing) {
      this.completeSegment(undefined, existing, content);
      usedSegmentIds.add(existing.messageId);
      return;
    }
    const segment = this.createSegment(
      kind,
      "fallback",
      messageBase || this.activeTurnId()
    );
    this.completeSegment(undefined, segment, content);
    usedSegmentIds.add(segment.messageId);
  }

  failContent(
    kind: AssistantSegmentKind,
    messageBase: string,
    content: string,
    usedSegmentIds: Set<string>
  ): void {
    if (!content) {
      return;
    }
    const existing = this.segmentWithContent(
      kind,
      messageBase,
      content,
      usedSegmentIds
    );
    const segment =
      existing ??
      this.createSegment(kind, "fallback", messageBase || this.activeTurnId());
    this.completeSegment(undefined, segment, content, true);
    usedSegmentIds.add(segment.messageId);
  }

  private emitDelta(segment: AssistantSegmentState, delta: string): void {
    if (!delta) {
      return;
    }
    segment.snapshot += delta;
    this.emit({
      type: segment.kind === "assistant" ? "assistant_delta" : "thinking_delta",
      payload: {
        turnId: this.activeTurnId(),
        messageId: segment.messageId,
        content: delta,
        snapshot: segment.snapshot
      }
    });
  }

  private ensureLiveSegment(
    index: number | undefined,
    kind: AssistantSegmentKind
  ): AssistantSegmentState {
    const existing = this.segmentForIndex(index, kind);
    if (existing && !existing.completed) {
      return existing;
    }
    return this.createSegment(kind, "live", "", index);
  }

  private createSegment(
    kind: AssistantSegmentKind,
    source: "live" | "fallback",
    messageBase = "",
    liveIndex?: number
  ): AssistantSegmentState {
    const base = messageBase || this.currentMessageBase || this.activeTurnId();
    const messageId = `claude-sdk:${kind}:${base}:${source}:${this.sequence++}`;
    const key = `${kind}:${messageId}`;
    const segment: AssistantSegmentState = {
      messageId,
      messageBase: base,
      kind,
      source,
      snapshot: "",
      completed: false,
      usageEmitted: false
    };
    this.segmentsByKey.set(key, segment);
    if (typeof liveIndex === "number") {
      this.segmentKeyByIndex.set(indexKey(kind, liveIndex), key);
    }
    return segment;
  }

  private segmentForIndex(
    index: number | undefined,
    kind?: AssistantSegmentKind
  ): AssistantSegmentState | undefined {
    if (typeof index !== "number") {
      return undefined;
    }
    if (kind) {
      const key = this.segmentKeyByIndex.get(indexKey(kind, index));
      return key ? this.segmentsByKey.get(key) : undefined;
    }
    const key =
      this.segmentKeyByIndex.get(indexKey("assistant", index)) ??
      this.segmentKeyByIndex.get(indexKey("thinking", index));
    return key ? this.segmentsByKey.get(key) : undefined;
  }

  private completeSegment(
    index: number | undefined,
    segment: AssistantSegmentState,
    fallbackText = "",
    failed = false
  ): void {
    if (typeof index === "number") {
      this.segmentKeyByIndex.delete(indexKey(segment.kind, index));
    }
    if (segment.completed && !failed) {
      return;
    }
    const tail =
      fallbackText && fallbackText.startsWith(segment.snapshot)
        ? fallbackText.slice(segment.snapshot.length)
        : "";
    if (tail && (segment.source !== "fallback" || segment.snapshot)) {
      this.emitDelta(segment, tail);
    } else if (!segment.snapshot && fallbackText) {
      segment.snapshot = fallbackText;
    }
    segment.completed = true;
    if (!segment.snapshot) {
      return;
    }
    const payload: Record<string, unknown> = {
      turnId: this.activeTurnId(),
      messageId: segment.messageId,
      content: segment.snapshot
    };
    if (segment.kind === "assistant" && this.pendingUsage) {
      payload.usage = this.pendingUsage;
      segment.usageEmitted = true;
    }
    this.emit({
      type:
        segment.kind === "assistant"
          ? failed
            ? "assistant_failed"
            : "assistant_completed"
          : "thinking_completed",
      payload
    });
  }

  private segmentWithContent(
    kind: AssistantSegmentKind,
    messageBase: string,
    content: string,
    usedSegmentIds: Set<string>
  ): AssistantSegmentState | undefined {
    const messageBases = new Set(
      [messageBase, this.currentMessageBase, this.activeTurnId()].filter(
        Boolean
      )
    );
    let tailCandidate: AssistantSegmentState | undefined;
    for (const segment of this.segmentsByKey.values()) {
      if (
        segment.kind === kind &&
        !usedSegmentIds.has(segment.messageId) &&
        messageBases.has(segment.messageBase) &&
        segment.snapshot === content
      ) {
        return segment;
      }
      if (
        !tailCandidate &&
        segment.kind === kind &&
        !segment.completed &&
        !usedSegmentIds.has(segment.messageId) &&
        messageBases.has(segment.messageBase) &&
        content.startsWith(segment.snapshot)
      ) {
        tailCandidate = segment;
      }
    }
    return tailCandidate;
  }
}

function indexKey(kind: AssistantSegmentKind, index: number): string {
  return `${kind}:${index}`;
}
