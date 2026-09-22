import {
  act,
  cleanup,
  fireEvent,
  render,
  screen
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentComposerProps } from "../AgentComposer.tsx";
import {
  AgentTargetSetupControllerProvider,
  type AgentTargetSetupController,
  type AgentTargetSetupControllerState
} from "../../../shared/agentEnv/agentTargetSetupController.tsx";
import { AgentHomeSetupComposer } from "./AgentHomeSetupComposer.tsx";

vi.mock("../AgentComposer.tsx", () => ({
  AgentComposer: (props: AgentComposerProps) => (
    <>
      <textarea
        aria-label="Draft"
        disabled={props.presentationEditorDisabled}
      />
      <button
        disabled={props.presentationSubmitDisabled}
        onClick={() => props.onSubmit([])}
      >
        Send
      </button>
    </>
  )
}));
afterEach(cleanup);

describe("home setup composer", () => {
  it("keeps the editor, draft and focus through checking, failure and retry", () => {
    let state: AgentTargetSetupControllerState = {
      enabled: true,
      agentTargetId: "extension:example",
      agentTarget: null,
      setup: { snapshot: null, loading: true, failed: false },
      authenticatePending: false,
      installPending: false,
      dialogOpen: false,
      selectedAuthMethodId: null,
      terminalLoginAvailable: false,
      terminalLoginError: null,
      terminalLoginPhase: "idle"
    };
    const listeners = new Set<() => void>();
    const refresh = vi.fn(async () => {});
    const controller = {
      getSnapshot: () => state,
      subscribe: (listener: () => void) => {
        listeners.add(listener);
        return () => {
          listeners.delete(listener);
        };
      },
      refresh,
      setDialogOpen: vi.fn()
    } as unknown as AgentTargetSetupController;
    const onSubmit = vi.fn();
    render(
      <AgentTargetSetupControllerProvider controller={controller}>
        <AgentHomeSetupComposer
          {...({
            presentationEditorDisabled: false,
            presentationSubmitDisabled: false,
            onSubmit
          } as unknown as AgentComposerProps)}
        />
      </AgentTargetSetupControllerProvider>
    );
    const editor = screen.getByRole("textbox") as HTMLTextAreaElement;
    editor.focus();
    fireEvent.change(editor, { target: { value: "Continue my draft" } });
    fireEvent.click(screen.getByText("Send"));
    expect(onSubmit).not.toHaveBeenCalled();
    const publish = (setup: AgentTargetSetupControllerState["setup"]) =>
      act(() => {
        state = { ...state, setup };
        listeners.forEach((listener) => listener());
      });
    publish({ snapshot: null, loading: false, failed: true });
    expect(screen.getByRole("textbox")).toBe(editor);
    expect(editor.value).toBe("Continue my draft");
    expect(document.activeElement).toBe(editor);
    fireEvent.click(screen.getByText("Check again"));
    expect(refresh).toHaveBeenCalledOnce();
    publish({ snapshot: null, loading: true, failed: false });
    publish({
      snapshot: {
        agentTargetId: "extension:example",
        status: "ready",
        runtimeSource: "managed",
        runtimeVersion: "1",
        reason: null,
        authMethods: [],
        account: null,
        plan: null,
        action: null
      },
      loading: false,
      failed: false
    });
    expect(screen.getByRole("textbox")).toBe(editor);
    expect(editor.value).toBe("Continue my draft");
    fireEvent.click(screen.getByText("Send"));
    expect(onSubmit).toHaveBeenCalledOnce();
  });
});
