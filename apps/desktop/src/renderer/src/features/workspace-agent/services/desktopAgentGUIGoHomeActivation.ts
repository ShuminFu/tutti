import type { WorkbenchHostActivation } from "@tutti-os/workbench-surface";
import { desktopAgentGUIGoHomeActivationType } from "../desktopAgentGUINodeState.ts";

export interface DesktopAgentGUIGoHomeRequest {
  sequence: number;
}

export interface ConsumeDesktopAgentGUIGoHomeActivationInput {
  activation: WorkbenchHostActivation | null;
  clearNodeActivation?: (this: void, nodeId: string, sequence: number) => void;
  handledSequence: number | null;
  markHandled(this: void, sequence: number): void;
  nodeId: string;
}

/**
 * 「回首页」激活的消费端。形状照 prefill-prompt 那条：按 sequence 去重、消费完
 * 立刻 clearNodeActivation，免得同一条激活在重渲染里被再放一次。
 * 真正的「回首页」由调用方把节点状态里的 lastActiveAgentSessionId 清空完成——
 * agent-gui 的选择控制器盯着这个字段，读到空就退回落地页（external_last_active_empty）。
 */
export function consumeDesktopAgentGUIGoHomeActivation({
  activation,
  clearNodeActivation,
  handledSequence,
  markHandled,
  nodeId
}: ConsumeDesktopAgentGUIGoHomeActivationInput): DesktopAgentGUIGoHomeRequest | null {
  const request = resolveDesktopAgentGUIGoHomeActivation(activation);
  if (!request || handledSequence === request.sequence) {
    return null;
  }
  markHandled(request.sequence);
  clearNodeActivation?.(nodeId, request.sequence);
  return request;
}

export function resolveDesktopAgentGUIGoHomeActivation(
  activation: WorkbenchHostActivation | null
): DesktopAgentGUIGoHomeRequest | null {
  if (
    !activation ||
    activation.type !== desktopAgentGUIGoHomeActivationType
  ) {
    return null;
  }
  return { sequence: activation.sequence };
}
