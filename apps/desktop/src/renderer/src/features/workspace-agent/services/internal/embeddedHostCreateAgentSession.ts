import { resolveAgentGUIProviderCatalogIdentity } from "@tutti-os/agent-gui/provider-catalog";
import type { AgentActivityCreateSessionInput } from "@tutti-os/agent-activity-core";
import {
  requestHostCreateAgentSession,
  type HostCreateAgentSessionArgs,
  type HostCreateAgentSessionResult
} from "../../../../platform/desktop/web/webHostBridgeClient.ts";
import { isEmbeddedDintalDock } from "../../../workspace-workbench/ui/embeddedDintalDock.ts";

// 回退看红保留这个符号：adapter 用 `if (shouldAskHostCreateAgentSession())`
// 包住前置钩子；改成 `if (false && shouldAskHostCreateAgentSession())` 后
// 「宿主成功不调用原 create」必须转红。
export function shouldAskHostCreateAgentSession(): boolean {
  return isEmbeddedDintalDock();
}

export function hostCreateAgentSessionArgsFromCreateInput(
  input: AgentActivityCreateSessionInput
): HostCreateAgentSessionArgs {
  const provider =
    resolveAgentGUIProviderCatalogIdentity(input.agentTargetId)?.providerId ??
    providerFromAgentTargetId(input.agentTargetId);
  const args: HostCreateAgentSessionArgs = {
    provider,
    cwd: input.cwd?.trim() ?? "",
    prompt: promptFromCreateInput(input)
  };
  const model = input.model?.trim();
  if (model) {
    args.model = model;
  }
  const thinkingLevel = input.reasoningEffort?.trim();
  if (thinkingLevel) {
    args.thinkingLevel = thinkingLevel;
  }
  return args;
}

export async function requestEmbeddedHostCreateAgentSession(
  input: AgentActivityCreateSessionInput
): Promise<HostCreateAgentSessionResult> {
  // 宿主请求发出后结果可能尚未返回，不得另建一条绕过任务队列的会话。
  return requestHostCreateAgentSession(
    hostCreateAgentSessionArgsFromCreateInput(input)
  );
}

function promptFromCreateInput(input: AgentActivityCreateSessionInput): string {
  const fromBlocks = (input.initialContent ?? [])
    .filter((block) => block.type === "text")
    .map((block) => block.text?.trim() ?? "")
    .filter((text) => text.length > 0)
    .join("\n");
  if (fromBlocks) {
    return fromBlocks;
  }
  return input.initialDisplayPrompt?.trim() ?? "";
}

function providerFromAgentTargetId(agentTargetId: string): string {
  const trimmed = agentTargetId.trim();
  const separator = trimmed.indexOf(":");
  return separator >= 0 ? trimmed.slice(separator + 1) : trimmed;
}
