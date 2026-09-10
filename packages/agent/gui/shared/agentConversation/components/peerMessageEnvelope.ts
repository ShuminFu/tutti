// RnDMaster 会话互通信封（RNDMASTER_PEER_MESSAGE_CARD）：后端把同伴会话的消息
// 包成一段给模型看的纯文本投进目标会话——一行 "Another <provider> session sent a
// message:" 抬头、N 个 <peer-message …> 块、一段固定的信任脚注。这里只负责把它
// 拆开给人看；模型收到的原文不变。

export interface PeerMessageEnvelopeBlock {
  from: string;
  fromName: string;
  fromProvider: string;
  fromMode: string;
  kind: string;
  body: string;
}

const ENVELOPE_HEAD = /^Another [^\n]* session sent a message:\n/;
const BLOCK_RE =
  /<peer-message\b([^>]*)>\n?([\s\S]*?)\n?<\/peer-message>/g;
const ATTR_RE = /([a-z-]+)="([^"]*)"/g;

export function isPeerMessageEnvelope(text: string): boolean {
  return ENVELOPE_HEAD.test(text) && text.includes("<peer-message");
}

export function parsePeerMessageEnvelope(
  text: string
): PeerMessageEnvelopeBlock[] {
  if (!isPeerMessageEnvelope(text)) return [];
  const blocks: PeerMessageEnvelopeBlock[] = [];
  for (const match of text.matchAll(BLOCK_RE)) {
    const attrs: Record<string, string> = {};
    for (const attr of (match[1] ?? "").matchAll(ATTR_RE)) {
      attrs[attr[1] ?? ""] = attr[2] ?? "";
    }
    blocks.push({
      from: attrs.from ?? "",
      fromName: attrs["from-name"] ?? "",
      fromProvider: attrs["from-provider"] ?? "",
      fromMode: attrs["from-mode"] ?? "",
      kind: attrs.kind || "info",
      body: (match[2] ?? "").trim()
    });
  }
  return blocks;
}

// 卡片抬头用的名字：配对别名 > provider · task 前 8 位。
export function peerMessageSenderLabel(
  block: PeerMessageEnvelopeBlock
): string {
  if (block.fromName.trim()) return block.fromName.trim();
  const short = block.from.slice(0, 8);
  return [block.fromProvider, short].filter(Boolean).join(" · ") || "peer";
}
