import { resolveAgentGUIProviderCatalogIdentity } from "@tutti-os/agent-gui/provider-catalog";
import type { AgentActivityCreateSessionInput } from "@tutti-os/agent-activity-core";
import {
  requestHostCreateAgentSession,
  type HostAgentPromptContentBlock,
  type HostAgentRailPlacement,
  type HostCreateAgentSessionArgs,
  type HostCreateAgentSessionResult
} from "../../../../platform/desktop/web/webHostBridgeClient.ts";
import { isEmbeddedDintalDock } from "../../../workspace-workbench/ui/embeddedDintalDock.ts";
import { embeddedHostCreatedSessionId } from "./embeddedHostCreatedSessionId.ts";

// 回退看红保留这个符号：adapter 用 `if (shouldAskHostCreateAgentSession())`
// 包住前置钩子；改成 `if (false && shouldAskHostCreateAgentSession())` 后
// 「宿主成功不调用原 create」必须转红。
export function shouldAskHostCreateAgentSession(): boolean {
  return isEmbeddedDintalDock();
}

export function resolveEmbeddedHostCreatedSessionId(
  clientSubmitId: string
): string | null {
  return isEmbeddedDintalDock() && clientSubmitId.trim()
    ? embeddedHostCreatedSessionId(clientSubmitId)
    : null;
}

const SUPPORTED_IMAGE_MIME_TYPES = new Set([
  "image/jpeg",
  "image/png",
  "image/webp"
]);
const CONNECTOR_KEY_PATTERN = /^[a-z][a-z0-9._-]{0,127}$/;

export function hostCreateAgentSessionArgsFromCreateInput(
  input: AgentActivityCreateSessionInput
): HostCreateAgentSessionArgs {
  const provider =
    resolveAgentGUIProviderCatalogIdentity(input.agentTargetId)?.providerId ??
    providerFromAgentTargetId(input.agentTargetId);
  const args: HostCreateAgentSessionArgs = {
    ...(input.clientSubmitId?.trim()
      ? { clientSubmitId: input.clientSubmitId.trim() }
      : {}),
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
  // 以下都是「用户显式表态才带」的字段：缺省时请求体与老版本逐字节相同，老调用方
  // （以及不含这些字段的自动化调用）的执行语义一个字都不变。
  const permissionModeId = input.permissionModeId?.trim();
  if (permissionModeId) {
    args.permissionModeId = permissionModeId;
  }
  // planMode 用 typeof 判断：`false` 是「用户明确没开计划模式」，与 undefined（没表态）
  // 必须分开——写成 `if (input.planMode)` 就把显式 false 丢了。
  if (typeof input.planMode === "boolean") {
    args.planMode = input.planMode;
  }
  const isolation = input.isolation?.trim();
  if (isolation === "worktree") {
    args.isolation = "worktree";
    // worktree 是 tuttid 建 session 的前置校验项：没有 project 归属它会直接拒收。
    // 拿不到项目归属就把选择报成不可用，而不是让它静默降级成在原目录里跑。
    const railPlacement = railPlacementFromCreateInput(input);
    if (
      !railPlacement ||
      railPlacement.kind !== "project" ||
      !railPlacement.projectPath
    ) {
      throw hostCreateError(
        "worktree isolation requires a project selection",
        "host_create_worktree_project_required"
      );
    }
    args.railPlacement = railPlacement;
  }
  const initialContent = initialContentFromCreateInput(input);
  if (initialContent.length > 0) {
    args.initialContent = initialContent;
  }
  const initialDisplayPrompt = input.initialDisplayPrompt?.trim();
  if (initialDisplayPrompt) {
    args.initialDisplayPrompt = initialDisplayPrompt;
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

/**
 * 首轮文本：只取文字块。纯图片首轮返回空串——宿主侧的非空校验由结构化内容放行，
 * 这里不编一句假文字冒充用户说过的话。
 */
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

/**
 * 结构化首轮内容 → 宿主可转发的块。
 *
 * 只挑文字块会让用户发的截图在首轮静默消失（Agent 只收到文字），所以这里按 tuttid 的
 * AgentPromptContentBlock 形状整块带过去（字段白名单：多一个键会被它的
 * additionalProperties:false 拒收），交给它既有的附件准备流程处理。
 *
 * attachmentId 刻意不转发：tuttid 的附件 id 按 workspace + agentSession 定位，宿主新建的
 * 是另一条会话号，跨会话原样转过去只会指向查无此物的附件。可搬运的是块的耐久载体：
 * path（已归档进 tuttid 唯一接受的图片来源根）、url、data。
 *
 * 搬不动的非空块一律报错，不静默丢掉：少一半内容的一轮比一次明确的失败更难查。
 */
export function initialContentFromCreateInput(
  input: AgentActivityCreateSessionInput
): HostAgentPromptContentBlock[] {
  const blocks: HostAgentPromptContentBlock[] = [];
  (input.initialContent ?? []).forEach((block, index) => {
    const next = hostPromptContentBlock(block, index);
    if (next) {
      blocks.push(next);
    }
  });
  return blocks;
}

function hostPromptContentBlock(
  block: NonNullable<AgentActivityCreateSessionInput["initialContent"]>[number],
  index: number
): HostAgentPromptContentBlock | null {
  if (block.type === "text") {
    const text = block.text ?? "";
    if (!text.trim()) {
      return null;
    }
    return { type: "text", text };
  }
  if (block.type === "image") {
    const mimeType = block.mimeType?.trim() ?? "";
    if (!SUPPORTED_IMAGE_MIME_TYPES.has(mimeType)) {
      throw hostCreateError(
        `initial content image ${index} has an unsupported MIME type`,
        "host_create_image_mime_unsupported"
      );
    }
    const url = block.url?.trim() ?? "";
    const data = block.data?.trim() ?? "";
    const path = block.path?.trim() ?? "";
    if (url && data) {
      throw hostCreateError(
        `initial content image ${index} carries both url and data`,
        "host_create_image_reference_ambiguous"
      );
    }
    if (url && !isHostSafeImageUrl(url)) {
      // tuttid 只取 https 且不带凭据的图片来源（它的 safePromptImageURL 同口径），
      // 送过去必被拒收，不如在这里说清楚。
      throw hostCreateError(
        `initial content image ${index} has an unusable url`,
        "host_create_image_url_unsafe"
      );
    }
    if (!url && !data && !path) {
      // 只剩 attachmentId 的图片没有可搬运的载体：它属于另一条会话，转过去必失效。
      throw hostCreateError(
        `initial content image ${index} has no durable reference (url, data or path)`,
        "host_create_image_reference_missing"
      );
    }
    const next: HostAgentPromptContentBlock = {
      type: "image",
      mimeType,
      // 挂载点优先顺序：url（provider 直取）> data（字节）> path（宿主归档的可读路径）。
      ...(url ? { url } : data ? { data } : { path })
    };
    const name = block.name?.trim();
    if (name) {
      next.name = name;
    }
    return next;
  }
  if (block.type === "file") {
    const path = block.path?.trim() ?? "";
    const name = block.name?.trim() ?? "";
    if (!isAbsoluteHostPromptPath(path)) {
      throw hostCreateError(
        `initial content file ${index} needs an absolute path`,
        "host_create_file_path_not_absolute"
      );
    }
    if (!name) {
      throw hostCreateError(
        `initial content file ${index} needs a name`,
        "host_create_file_name_missing"
      );
    }
    const next: HostAgentPromptContentBlock = { type: "file", path, name };
    const mimeType = block.mimeType?.trim();
    if (mimeType) {
      next.mimeType = mimeType;
    }
    // sizeBytes 过桥要落成 Go 的 int64：只有安全整数能无损转换。
    if (
      typeof block.sizeBytes === "number" &&
      Number.isSafeInteger(block.sizeBytes) &&
      block.sizeBytes >= 0
    ) {
      next.sizeBytes = block.sizeBytes;
    }
    return next;
  }
  if (block.type === "skill" || block.type === "mention") {
    const name = block.name?.trim() ?? "";
    const path = block.path?.trim() ?? "";
    if (!name || !path) {
      throw hostCreateError(
        `initial content ${block.type} ${index} needs a name and a path`,
        "host_create_block_incomplete"
      );
    }
    return { type: block.type, name, path };
  }
  if (block.type === "connector") {
    const connectorKey = block.connectorKey?.trim() ?? "";
    if (!CONNECTOR_KEY_PATTERN.test(connectorKey)) {
      throw hostCreateError(
        `initial content connector ${index} has an invalid key`,
        "host_create_block_incomplete"
      );
    }
    return { type: "connector", connectorKey };
  }
  throw hostCreateError(
    `initial content block ${index} has an unsupported type`,
    "host_create_block_unsupported"
  );
}

/**
 * 会话栏归属：worktree 隔离的前置条件（tuttid 要求 project 归属，且不接受从 cwd 反推）。
 */
function railPlacementFromCreateInput(
  input: AgentActivityCreateSessionInput
): HostAgentRailPlacement | null {
  const placement = input.railPlacement;
  if (
    !placement ||
    (placement.kind !== "project" && placement.kind !== "conversations")
  ) {
    return null;
  }
  return {
    kind: placement.kind,
    ...(placement.projectPath?.trim()
      ? { projectPath: placement.projectPath.trim() }
      : {}),
    ...(placement.sectionKey?.trim()
      ? { sectionKey: placement.sectionKey.trim() }
      : {}),
    ...(typeof placement.version === "number"
      ? { version: placement.version }
      : {})
  };
}

function isAbsoluteHostPromptPath(path: string): boolean {
  return (
    path.startsWith("/") ||
    /^[A-Za-z]:[\\/]/.test(path) ||
    /^\\\\[^\\/]+[\\/][^\\/]+/.test(path)
  );
}

// 与 tuttid 的 safePromptImageURL 同口径：https、有 host、不带凭据。
function isHostSafeImageUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return (
      url.protocol === "https:" &&
      Boolean(url.hostname) &&
      !url.username &&
      !url.password
    );
  } catch {
    return false;
  }
}

function hostCreateError(message: string, code: string): Error {
  return Object.assign(new Error(message), { code });
}

function providerFromAgentTargetId(agentTargetId: string): string {
  const trimmed = agentTargetId.trim();
  const separator = trimmed.indexOf(":");
  return separator >= 0 ? trimmed.slice(separator + 1) : trimmed;
}
