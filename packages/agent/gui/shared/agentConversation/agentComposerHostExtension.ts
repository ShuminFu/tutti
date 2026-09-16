// 作曲区的宿主扩展口（分栏结对模式，peer-pair-mode 票 04/05）。
//
// 为什么要这个口：结对模式的单选要画在**每栏作曲区正上方**，第一句还要在发出去之前
// 拼上一张开工卡——这两件事的判据（本栏是哪一栏、这对是不是 pair 模式、开工卡
// pending 没有）全在分栏层（apps/desktop）手里，而 agent-gui 包**不能反向 import**
// apps/desktop。所以照 0103/0116/0122 那几个注册口的形状，在 gui 包里只声明一个
// 与分栏无关的扩展点：
//
//   - `renderAboveComposer`：作曲区上方一条附加行，返回 null 就不占位；
//   - `wantsSubmitPreparation` + `prepareSubmit`：提交前给宿主一次「在正文前面拼一段」
//     的机会，并在提交被引擎接受 / 拒绝后回调宿主。
//
// 普通 web / 老宿主不注册，作曲区与提交链路逐字节不变。
//
// 这个文件刻意**没有运行时 import**（React 只用类型），因为 apps/desktop 的
// `node --test --experimental-strip-types` 单测会经 conversation-rail-projection
// 子路径直接加载它，不带扩展名的运行时 import 在那条路上解析不了。
import type { ReactNode } from "react";
import type { AgentPromptContentBlock } from "../contracts/dto";

/** 宿主对一次提交的「准备结果」：拼在正文前面的一段 + 提交结局回调。 */
export interface AgentPromptSubmitPreparation {
  /** 拼在第一个文字块最前面的一段（不含分隔用的空行，由本模块补 `\n\n`）。 */
  prefix: string;
  /** 引擎确认接受了这次提交（accepted / confirmed）之后调一次。 */
  onAccepted(): Promise<void> | void;
  /** 提交没发出去、被拒或被撤回时调一次，宿主据此清掉「在途」标记。 */
  onRejected(): void;
}

export interface AgentComposerSubmitPreparationInput {
  agentSessionId: string;
  /** 这次提交的纯文字部分（各文字块按原顺序拼起来）；宿主拿它当「目标」。 */
  text: string;
}

export interface AgentComposerHostExtension {
  renderAboveComposer?(input: { agentSessionId: string | null }): ReactNode;
  /**
   * 同步预判：这次提交宿主要不要插手。返回 false 时提交链路**不产生任何异步跳转**
   * ——绝大多数提交（非结对 / 已开工）必须和没注册时一模一样。
   */
  wantsSubmitPreparation?(input: AgentComposerSubmitPreparationInput): boolean;
  prepareSubmit?(
    input: AgentComposerSubmitPreparationInput
  ): Promise<AgentPromptSubmitPreparation | null>;
}

let registeredExtension: AgentComposerHostExtension | null = null;
const extensionListeners = new Set<() => void>();

function notifyExtensionListeners(): void {
  for (const listener of [...extensionListeners]) listener();
}

export function registerAgentComposerHostExtension(
  extension: AgentComposerHostExtension
): () => void {
  registeredExtension = extension;
  notifyExtensionListeners();
  return () => {
    // 只摘自己那一份，避免两次安装时后者被前者的 dispose 误摘。
    if (registeredExtension !== extension) return;
    registeredExtension = null;
    notifyExtensionListeners();
  };
}

export function agentComposerHostExtension(): AgentComposerHostExtension | null {
  return registeredExtension;
}

/** 作曲区的附加行要在「注册晚于首次渲染」时也能出现，所以给个订阅口。 */
export function subscribeAgentComposerHostExtension(
  listener: () => void
): () => void {
  extensionListeners.add(listener);
  return () => {
    extensionListeners.delete(listener);
  };
}

/** 一次提交里用户打的字：只取文字块，图片 / 文件 / 技能块不算。 */
export function agentPromptSubmitText(
  content: readonly AgentPromptContentBlock[]
): string {
  return content
    .filter((block) => block.type === "text")
    .map((block) => block.text ?? "")
    .join("")
    .trim();
}

/** 用户原话：各文字块去空白后按行拼，与 agentPromptContentDisplayText 同口径（本文件不做运行时 import）。 */
function agentPromptOriginalDisplayText(
  content: readonly AgentPromptContentBlock[]
): string | undefined {
  const text = content
    .filter((block) => block.type === "text")
    .map((block) => block.text?.trim() ?? "")
    .filter(Boolean)
    .join("\n");
  return text || undefined;
}

/**
 * 斜杠命令（`/compact`、`/review` …）永远不交给宿主：它们不是「一句话」，拼卡会把
 * 命令变成普通正文、provider 就不认了（票 05）。空文字（纯贴图）同样不拦——没有
 * 目标可写。
 */
export function isAgentPromptSubmitPreparable(text: string): boolean {
  const trimmed = text.trim();
  return trimmed.length > 0 && !trimmed.startsWith("/");
}

/**
 * 把宿主给的前缀拼进**第一个文字块**的最前面，其余块（图片、文件、技能、提及）一个
 * 不动、顺序不变、块数不变——坑151 就是「文字＋贴图」的块结构被动过之后整条请求
 * 被 tuttid 拒收。没有文字块时原样返回（调用方已经用 isAgentPromptSubmitPreparable 挡掉）。
 */
export function prefixAgentPromptContent(
  content: readonly AgentPromptContentBlock[],
  prefix: string
): AgentPromptContentBlock[] {
  const index = content.findIndex((block) => block.type === "text");
  if (index < 0 || !prefix) return content.map((block) => ({ ...block }));
  return content.map((block, blockIndex) =>
    blockIndex === index
      ? { ...block, text: `${prefix}\n\n${block.text ?? ""}` }
      : { ...block }
  );
}

/**
 * 一次真正发出去的提交的结局。`settle()` 是函数不是现成的 Promise：
 * 只有真要等结局（拼了卡）时才去订阅引擎，普通提交不多挂一个订阅。
 * resolve 为 true 表示引擎确认接受。
 */
export interface AgentPromptSubmitReceipt {
  settle(): Promise<boolean>;
}

export interface RunPreparedAgentPromptSubmitInput {
  agentSessionId: string;
  content: readonly AgentPromptContentBlock[];
  displayPrompt?: string;
  extension: AgentComposerHostExtension;
  /** 拼好（或没拼）之后真正发送；同步返回，没发出去给 null。 */
  send(
    content: AgentPromptContentBlock[],
    displayPrompt: string | undefined
  ): AgentPromptSubmitReceipt | null;
  /** send 已经调过（不论拼没拼）：调用方据此解除「准备中」的重复提交闸。 */
  onDispatched?(): void;
}

/**
 * 「先问宿主 → 拼 → 发 → 等引擎结局 → 回调宿主」这一整段编排（PRD D4：发送被接受
 * 之后才算开工）。抽成纯函数是为了单测不必搭 React 与引擎：
 *
 *   - 宿主准备失败（抛错 / 返回 null）→ **原样发送**，绝不因为开工卡把用户那句吞掉；
 *   - 发出去了但没被接受（send 返回 null / settle() 给 false 或抛错）→ onRejected，
 *     **不**调 onAccepted（「提交失败不 commit」）；
 *   - 被接受 → onAccepted（宿主在这里投卡；它自己的失败自己兜，不影响已发出的消息）。
 */
export async function runPreparedAgentPromptSubmit(
  input: RunPreparedAgentPromptSubmitInput
): Promise<void> {
  const text = agentPromptSubmitText(input.content);
  let preparation: AgentPromptSubmitPreparation | null = null;
  if (isAgentPromptSubmitPreparable(text) && input.extension.prepareSubmit) {
    try {
      preparation = await input.extension.prepareSubmit({
        agentSessionId: input.agentSessionId,
        text
      });
    } catch {
      preparation = null;
    }
  }
  if (!preparation || !preparation.prefix.trim()) {
    // 宿主给了个空前缀：当作不拦截，但它可能已经记了「在途」，要让它清掉。
    preparation?.onRejected();
    input.send(input.content.map((block) => ({ ...block })), input.displayPrompt);
    input.onDispatched?.();
    return;
  }
  // 回显一律用用户原话（评审 E）：displayPrompt 会进排队面板、引擎的待发记录和 tuttid
  // 的消息负载，带上开工卡的话，排队项的文字 / 编辑回填都会露出一大段协议。
  // 模型收到的仍是拼了卡的 content。
  const displayPrompt = input.displayPrompt?.trim()
    ? input.displayPrompt
    : agentPromptOriginalDisplayText(input.content);
  let receipt: AgentPromptSubmitReceipt | null = null;
  let sent = false;
  try {
    receipt = input.send(
      prefixAgentPromptContent(input.content, preparation.prefix),
      displayPrompt
    );
    sent = true;
  } finally {
    input.onDispatched?.();
    // send 抛错（引擎 / 诊断上报炸了）时宿主记的「在途」必须清掉，否则这对的开工卡
    // 再也不会被拦截（评审补充 3）。错误照常往上抛给调用方显示。
    if (!sent) preparation.onRejected();
  }
  let accepted = false;
  if (receipt) {
    try {
      accepted = await receipt.settle();
    } catch {
      accepted = false;
    }
  }
  if (!accepted) {
    preparation.onRejected();
    return;
  }
  await preparation.onAccepted();
}
