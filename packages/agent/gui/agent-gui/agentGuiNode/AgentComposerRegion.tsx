import { useCallback, type ReactNode, type RefObject } from "react";
import styles from "./AgentGUINode.styles";

/**
 * Stable layout boundary for the conversation composer area.
 *
 * Floating controls, lifted interaction prompts, session accessories, and the
 * primary composer each have an explicit slot. Domain workflows compose into
 * `accessories`; this primitive owns no workflow or lifecycle policy.
 */
export function AgentComposerRegion({
  accessories,
  attentionKey,
  floating,
  lifted,
  primary,
  regionRef
}: {
  accessories?: ReactNode;
  attentionKey?: string;
  floating?: ReactNode;
  lifted?: ReactNode;
  primary: ReactNode;
  regionRef: RefObject<HTMLDivElement | null>;
}): React.JSX.Element {
  const revealAttention = useCallback(
    (viewport: HTMLDivElement | null) => {
      if (!viewport) return;
      const attention = attentionKey
        ? viewport.querySelector<HTMLElement>("[data-agent-composer-attention]")
        : null;
      // Only scroll the dock: scrollIntoView can move the transcript or host.
      // Do not focus a newly arriving prompt while the user is typing.
      viewport.scrollTop = attention
        ? viewport.scrollTop +
          attention.getBoundingClientRect().top -
          viewport.getBoundingClientRect().top
        : viewport.scrollHeight;
    },
    [attentionKey]
  );

  return (
    <div
      ref={regionRef}
      className={styles.bottomDock}
      data-testid="agent-gui-bottom-dock"
    >
      {floating}
      <div ref={revealAttention} className={styles.bottomDockViewport}>
        <div className={styles.bottomDockContent}>
          {lifted}
          {accessories}
          {primary}
        </div>
      </div>
    </div>
  );
}
