import { useState, type JSX } from "react";
import { ChevronRight } from "lucide-react";
import { translate } from "../../../i18n/index";
import { CollapsibleReveal } from "./CollapsibleReveal";

// "neutral" is for informational notices (e.g. the skills-budget note) that
// must not read as an error; "danger" stays the default for failure cards.
const DISCLOSURE_TONE_CLASS_NAMES = {
  danger: {
    root: "text-[var(--state-danger)]",
    hover: "hover:text-[var(--state-danger-hover)]",
    panel: "bg-[var(--on-danger)] text-[var(--state-danger)]"
  },
  neutral: {
    root: "text-[var(--text-secondary)]",
    hover: "hover:text-[var(--text-primary)]",
    panel: "bg-[var(--transparency-block)] text-[var(--text-secondary)]"
  }
} as const;

export function AgentMessageDetailsDisclosure({
  detail,
  className = "",
  label,
  tone = "danger"
}: {
  detail: string;
  className?: string;
  label?: string;
  tone?: keyof typeof DISCLOSURE_TONE_CLASS_NAMES;
}): JSX.Element {
  "use memo";
  const [expanded, setExpanded] = useState(false);
  const toneClassNames = DISCLOSURE_TONE_CLASS_NAMES[tone];
  return (
    <div className={`${className} text-[11px] ${toneClassNames.root}`}>
      <button
        type="button"
        className={`inline-flex w-fit max-w-full min-w-0 cursor-pointer select-none items-center gap-1.5 border-0 bg-transparent p-0 text-left font-[inherit] text-[inherit] transition-colors duration-150 ${toneClassNames.hover}`}
        aria-expanded={expanded}
        onClick={() => setExpanded((value) => !value)}
      >
        {label ?? translate("agentHost.agentGui.visibleErrorDetails")}
        <ChevronRight
          size={12}
          strokeWidth={2.2}
          aria-hidden="true"
          className="shrink-0 text-[inherit]"
          style={{
            transform: expanded ? "rotate(90deg)" : "rotate(0deg)",
            transformOrigin: "center",
            transition: "transform 200ms cubic-bezier(0.22, 1.18, 0.36, 1)",
            willChange: "transform"
          }}
        />
      </button>
      <CollapsibleReveal expanded={expanded} preMountOnIdle>
        <pre
          className={`mt-2 max-h-[220px] overflow-auto whitespace-pre-wrap break-words rounded-[6px] px-3 py-2 font-[var(--tsh-font-mono)] text-[11px] leading-5 ${toneClassNames.panel}`}
        >
          {detail}
        </pre>
      </CollapsibleReveal>
    </div>
  );
}
