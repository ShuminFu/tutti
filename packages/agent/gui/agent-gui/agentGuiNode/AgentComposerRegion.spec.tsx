import { createRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentComposerRegion } from "./AgentComposerRegion";

describe("AgentComposerRegion", () => {
  it("keeps floating controls outside one persistent viewport and preserves draft/actions across prompts", () => {
    const approve = vi.fn();
    const ref = createRef<HTMLDivElement>();
    const content = (attentionKey?: string) => (
      <AgentComposerRegion
        regionRef={ref}
        attentionKey={attentionKey}
        floating={<button>Latest reply</button>}
        lifted={
          attentionKey ? (
            <button data-agent-composer-attention onClick={approve}>
              Approve
            </button>
          ) : null
        }
        accessories={<button>Expand workflow</button>}
        primary={<textarea aria-label="Draft" defaultValue="Keep my draft" />}
      />
    );
    const view = render(content());
    const viewport = ref.current!.querySelector<HTMLElement>(
      ".agent-gui-node__bottom-dock-viewport"
    )!;
    const draft = screen.getByLabelText("Draft");
    draft.focus();
    fireEvent.change(draft, { target: { value: "Edited draft" } });
    viewport.scrollTop = 180;
    view.rerender(content("session:turn:approval"));
    expect(screen.getByLabelText("Draft")).toBe(draft);
    expect(draft).toHaveValue("Edited draft");
    expect(document.activeElement).toBe(draft);
    expect(viewport.contains(screen.getByText("Approve"))).toBe(true);
    expect(viewport.contains(screen.getByText("Expand workflow"))).toBe(true);
    expect(viewport.contains(screen.getByText("Latest reply"))).toBe(false);
    fireEvent.click(screen.getByText("Approve"));
    expect(approve).toHaveBeenCalledOnce();
    view.rerender(content());
    expect(
      ref.current!.querySelector(".agent-gui-node__bottom-dock-viewport")
    ).toBe(viewport);
    expect(draft).toHaveValue("Edited draft");
  });

  it("reveals a new exact prompt with dock-local scrolling, not ancestor scrolling or focus", () => {
    const ref = createRef<HTMLDivElement>();
    const content = (attentionKey?: string) => (
      <AgentComposerRegion
        regionRef={ref}
        attentionKey={attentionKey}
        primary={<div data-agent-composer-attention>Question</div>}
      />
    );
    const view = render(content());
    const viewport = ref.current!.firstElementChild as HTMLElement;
    const question = screen.getByText("Question");
    viewport.getBoundingClientRect = () => ({ top: 400 }) as DOMRect;
    question.getBoundingClientRect = () => ({ top: 360 }) as DOMRect;
    Object.defineProperty(viewport, "scrollHeight", {
      configurable: true,
      value: 900
    });
    viewport.scrollTop = 120;
    view.rerender(content("session:turn:question-1"));
    expect(viewport.scrollTop).toBe(80);
    viewport.scrollTop = 150;
    view.rerender(content("session:turn:question-1"));
    expect(viewport.scrollTop).toBe(150);
    view.rerender(content("session:turn:question-2"));
    expect(viewport.scrollTop).toBe(110);
    view.rerender(content());
    expect(viewport.scrollTop).toBe(900);
  });
});
