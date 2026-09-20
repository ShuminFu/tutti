import type { RefObject } from "react";
import type { AgentTranscriptVirtualScrollController } from "../../../shared/agentConversation/components/AgentTranscriptView";

export interface TimelineGeometry {
  clientHeight: number;
  maxScrollTop: number;
  scrollHeight: number;
}

export function readTimelineGeometry(timeline: HTMLElement): TimelineGeometry {
  const scrollHeight = timeline.scrollHeight;
  const clientHeight = timeline.clientHeight;
  return {
    clientHeight,
    maxScrollTop: Math.max(0, scrollHeight - clientHeight),
    scrollHeight
  };
}

export function matchingVirtualScrollController(
  controllerRef: RefObject<AgentTranscriptVirtualScrollController | null>,
  agentSessionId: string
): AgentTranscriptVirtualScrollController | null {
  const controller = controllerRef.current;
  return controller?.enabled && controller.agentSessionId === agentSessionId
    ? controller
    : null;
}

export function hasStaleVirtualScrollController(
  controllerRef: RefObject<AgentTranscriptVirtualScrollController | null>,
  agentSessionId: string
): boolean {
  const controller = controllerRef.current;
  return (
    controller?.enabled === true && controller.agentSessionId !== agentSessionId
  );
}

export function userScrollBehavior(): ScrollBehavior {
  return typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
    ? "auto"
    : "smooth";
}
