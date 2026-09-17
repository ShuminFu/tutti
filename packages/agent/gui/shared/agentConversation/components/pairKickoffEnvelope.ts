// 分栏结对模式（peer-pair-mode 票 05）的卡片文本解析：`<pair-kickoff>` / `<pair-ended>`。
//
// 两处出现（格式由后端生成，见 PRD「卡片文本格式」）：
//   - 发送栏：用户消息 = `<pair-kickoff …>…</pair-kickoff>` + `\n\n` + 用户原文。
//     → splitLeadingPairCard 把前缀块剥成卡片，剩下的原文照常显示。
//   - 搭档栏：块是 `<peer-message>` 信封的正文（正文以 `<pair-kickoff` / `<pair-ended` 开头）。
//     → parsePairCard 把整段正文认成结对卡，而不是普通 peer 卡。
// 只负责拆给人看；模型收到的原文不变。认不出（畸形）一律返回 null，调用方退回原样显示，
// 宁可露标签也不吞字。

export type PairCardRole = "developer" | "reviewer";

export type PairCard =
  | {
      kind: "kickoff";
      /** 收卡这一栏的角色。 */
      role: PairCardRole;
      partner: string;
      partnerProvider: string;
      partnerRole: PairCardRole | null;
      reason: "start" | "roles_changed";
      /** 「目标：」之后的用户原话，可多行（只有投给搭档的块里有）；发送栏的块为 null。 */
      goal: string | null;
      /** 去掉目标段之后的协议正文（从「结对模式开始：」/「结对角色已调整：」那行起）。 */
      protocol: string;
    }
  | {
      kind: "ended";
      partner: string;
      body: string;
    };

const OPEN_TAG_RE = /^<(pair-kickoff|pair-ended)((?:\s+[a-z_-]+="[^"]*")*)\s*>/;
const ATTR_RE = /([a-z_-]+)="([^"]*)"/g;
const GOAL_PREFIX_RE = /^目标[：:]\s*/;
// 后端模板（cliagent-backend/internal/peer/pair_kickoff.go FormatPairKickoffBlock）里
// 协议正文的首行：「结对模式开始：…」或「结对角色已调整：…」，紧跟在（可能多行的）目标之后。
const PROTOCOL_HEAD_RE = /^(结对模式开始|结对角色已调整)[：:]/;

// 属性值按 XML 转义（PRD）：先换具名实体，`&amp;` 放最后，免得 `&amp;lt;` 被解两次。
function unescapeAttr(value: string): string {
  return value
    .replace(/&quot;/g, '"')
    .replace(/&apos;|&#39;/g, "'")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&amp;/g, "&");
}

function parseRole(value: string | undefined): PairCardRole | null {
  return value === "developer" || value === "reviewer" ? value : null;
}

/**
 * 从 text 开头读一个完整的结对块。按**首个**真正的闭合标签切：后端会把 goal 里的
 * `</pair-kickoff` / `</pair-ended` 改写成 `< /…`，所以第一个闭合标签一定是块自己的。
 */
function readLeadingPairCard(
  text: string
): { card: PairCard; end: number } | null {
  const open = OPEN_TAG_RE.exec(text);
  if (!open) return null;
  const tag = open[1] as "pair-kickoff" | "pair-ended";
  const closing = `</${tag}>`;
  const bodyStart = open[0].length;
  const closeAt = text.indexOf(closing, bodyStart);
  if (closeAt < 0) return null;
  const attrs: Record<string, string> = {};
  for (const attr of (open[2] ?? "").matchAll(ATTR_RE)) {
    attrs[attr[1] ?? ""] = unescapeAttr(attr[2] ?? "");
  }
  const body = text.slice(bodyStart, closeAt).replace(/^\n/, "").replace(/\n$/, "");
  const end = closeAt + closing.length;
  if (tag === "pair-ended") {
    return {
      card: { body: body.trim(), kind: "ended", partner: attrs.partner ?? "" },
      end
    };
  }
  const role = parseRole(attrs.role);
  if (!role) return null;
  const lines = body.split("\n");
  const hasGoal = GOAL_PREFIX_RE.test(lines[0] ?? "");
  // 目标是用户原话，可以多行：从「目标：」一直到协议首行之前。协议首行取**最后一个**
  // 匹配行——用户原话里恰好也写了「结对模式开始：」时，模板那行一定排在它后面；
  // 协议固定行不以这两个词开头。找不到协议首行（老格式 / 手搓的块）时退回只认第一行。
  let goalEnd = hasGoal ? 1 : 0;
  if (hasGoal) {
    for (let index = lines.length - 1; index >= 1; index -= 1) {
      if (PROTOCOL_HEAD_RE.test(lines[index] ?? "")) {
        goalEnd = index;
        break;
      }
    }
  }
  const goal = hasGoal
    ? lines
        .slice(0, goalEnd)
        .join("\n")
        .replace(GOAL_PREFIX_RE, "")
        .trim()
    : null;
  return {
    card: {
      goal,
      kind: "kickoff",
      partner: attrs.partner ?? "",
      partnerProvider: attrs.partner_provider ?? "",
      partnerRole: parseRole(attrs.partner_role),
      protocol: lines.slice(goalEnd).join("\n").trim(),
      reason: attrs.reason === "roles_changed" ? "roles_changed" : "start",
      role
    },
    end
  };
}

/** 整段文本就是一张结对卡（搭档栏 peer 信封的正文）；后面还有别的字就不算。 */
export function parsePairCard(text: string): PairCard | null {
  const trimmed = text.trim();
  const read = readLeadingPairCard(trimmed);
  if (!read) return null;
  return trimmed.slice(read.end).trim() ? null : read.card;
}

/** 发送栏：开头一张卡 + 用户原文。原文可以为空（纯卡）。 */
export function splitLeadingPairCard(
  text: string
): { card: PairCard; rest: string } | null {
  const read = readLeadingPairCard(text.replace(/^\s+/, ""));
  if (!read) return null;
  const leading = text.length - text.replace(/^\s+/, "").length;
  return {
    card: read.card,
    rest: text.slice(leading + read.end).replace(/^\n+/, "")
  };
}

/**
 * 发送栏那句「开工卡 + 原话」回到输入框 / 剪贴板时只要原话（票 05 评审 E）：
 * 排队项的「编辑」、已发消息的「编辑重发」「复制」、上箭头历史都走这里，
 * 与 splitLeadingPairCard 同一套解析，只剥开工卡；认不出就原样返回。
 */
export function stripLeadingPairKickoff(text: string): string {
  const split = splitLeadingPairCard(text);
  return split && split.card.kind === "kickoff" ? split.rest : text;
}

/**
 * 同上，作用在提交内容上：只动**第一个文字块**（开工卡就拼在那里，见
 * agentComposerHostExtension.prefixAgentPromptContent），其余块原样。没有卡时返回原数组。
 */
export function stripLeadingPairKickoffFromContent<
  Block extends { type: string; text?: string }
>(content: readonly Block[]): readonly Block[] {
  const index = content.findIndex((block) => block.type === "text");
  const text = index < 0 ? undefined : content[index]?.text;
  if (text === undefined) return content;
  const stripped = stripLeadingPairKickoff(text);
  if (stripped === text) return content;
  return content.map((block, blockIndex) =>
    blockIndex === index ? { ...block, text: stripped } : block
  );
}
