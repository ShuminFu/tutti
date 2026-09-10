import { useState, type JSX } from "react";
import { ChevronRight } from "lucide-react";
import { translate } from "../../../i18n/index";
import { CollapsibleReveal } from "./CollapsibleReveal";
import {
  peerMessageSenderLabel,
  type PeerMessageEnvelopeBlock
} from "./peerMessageEnvelope";

// 同伴会话消息卡：默认只露抬头 + 正文第一行，点开才展示全文。信封的抬头行与
// 信任脚注是给模型的护栏，人看不需要，这里一律不显示。
export function AgentPeerMessageCard({
  block
}: {
  block: PeerMessageEnvelopeBlock;
}): JSX.Element {
  "use memo";
  const [expanded, setExpanded] = useState(false);
  const firstLine = block.body.split("\n")[0] ?? "";
  const shortId = block.from.slice(0, 8);
  return (
    <div
      className="min-w-0"
      data-testid="agent-peer-message-card"
      data-peer-message-kind={block.kind}
    >
      <div className="text-[12px] leading-5 text-[var(--text-secondary)]">
        {translate("agentHost.agentGui.peerMessageFrom")}{" "}
        <span className="font-medium text-[var(--text-primary)]">
          {peerMessageSenderLabel(block)}
        </span>
      </div>
      <div className="mt-1 overflow-hidden rounded-[12px] border border-[var(--border)] bg-[var(--surface-2)]">
        <div className="flex items-center gap-2 px-3 pt-2 text-[11px] leading-4 text-[var(--text-muted)]">
          <span className="truncate">
            {[block.fromProvider, shortId].filter(Boolean).join(" · ")}
          </span>
          <span className="shrink-0 rounded-full bg-[var(--bg-accent)] px-2 py-px text-[var(--text-accent)]">
            {block.kind}
          </span>
        </div>
        <button
          type="button"
          className="flex w-full cursor-pointer items-start gap-2 border-0 bg-transparent px-3 pb-2.5 pt-1.5 text-left font-[inherit] text-[13px] leading-5 text-[var(--text-primary)] hover:bg-[var(--surface-1)]"
          aria-expanded={expanded}
          data-testid="agent-peer-message-toggle"
          onClick={() => setExpanded((value) => !value)}
        >
          <span className="min-w-0 flex-1">
            {expanded ? null : (
              <span className="block truncate" data-testid="agent-peer-message-preview">
                {firstLine}
              </span>
            )}
            <CollapsibleReveal expanded={expanded}>
              <span
                className="block whitespace-pre-wrap break-words"
                data-testid="agent-peer-message-body"
              >
                {block.body}
              </span>
            </CollapsibleReveal>
          </span>
          <ChevronRight
            size={14}
            strokeWidth={2}
            aria-hidden="true"
            className="mt-[3px] shrink-0 text-[var(--text-muted)]"
            style={{
              transform: expanded ? "rotate(90deg)" : "rotate(0deg)",
              transformOrigin: "center",
              transition: "transform 200ms cubic-bezier(0.22, 1.18, 0.36, 1)"
            }}
          />
        </button>
      </div>
    </div>
  );
}

export function AgentPeerMessageCards({
  blocks
}: {
  blocks: readonly PeerMessageEnvelopeBlock[];
}): JSX.Element {
  // w-full 是必须的：卡片挂在用户消息那条 grid 上，而用户气泡靠右，
  // 那条 grid 是 justify-items: end —— 宽度 auto 的格子会按 max-content 撑开。
  // 折叠态预览用 truncate（white-space: nowrap），它的 max-content 就是整行不折行的原文，
  // 于是卡片被撑到两千多像素、再靠右对齐，整段文字溢出到视口左边外面（min-w-0 拦不住）。
  "use memo";
  return (
    <div className="flex w-full min-w-0 flex-col gap-2" data-testid="agent-peer-message-cards">
      {blocks.map((block, index) => (
        <AgentPeerMessageCard key={`${block.from}:${index}`} block={block} />
      ))}
    </div>
  );
}
