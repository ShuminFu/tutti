import { useExternalStoreSnapshot } from "@tutti-os/ui-react-hooks";
import { Button, LoadingIcon } from "@tutti-os/ui-system";
import { useTranslation } from "../../../i18n/index.ts";
import { useAgentTargetSetupController } from "../../../shared/agentEnv/agentTargetSetupController.tsx";
import { AgentComposer, type AgentComposerProps } from "../AgentComposer.tsx";

// Setup gates submission, never the lifetime of the draft editor. Keeping the
// editor mounted also preserves IME composition, selection and attachment work.
export function AgentHomeSetupComposer(
  props: AgentComposerProps
): React.JSX.Element {
  const controller = useAgentTargetSetupController();
  const { enabled, setup } = useExternalStoreSnapshot(controller);
  const { t } = useTranslation();
  const blocked =
    enabled && (setup.failed || setup.snapshot?.status !== "ready");
  const pending =
    blocked &&
    (setup.loading ||
      (!setup.snapshot && !setup.failed) ||
      setup.snapshot?.status === "installing" ||
      setup.snapshot?.status === "authenticating");
  const message = blocked
    ? t(
        pending
          ? "agentHost.agentGui.targetSetupDraftPreparing"
          : "agentHost.agentGui.targetSetupDraftNeedsAttention"
      )
    : null;

  return (
    <>
      <AgentComposer
        {...props}
        presentationSubmitDisabled={props.presentationSubmitDisabled || blocked}
        disabledReason={message ?? props.disabledReason}
      />
      <div
        className="flex min-h-8 items-center justify-center gap-2 text-xs text-[var(--text-secondary)]"
        role="status"
        aria-live="polite"
      >
        {blocked ? (
          <>
            {pending ? (
              <LoadingIcon className="size-3 animate-spin motion-reduce:animate-none" />
            ) : null}
            <span>{message}</span>
            {!pending ? (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => {
                  if (setup.failed) void controller.refresh();
                  else controller.setDialogOpen(true);
                }}
              >
                {t(
                  setup.failed
                    ? "agentHost.agentGui.targetSetupRetry"
                    : "agentHost.agentGui.targetSetupOpen"
                )}
              </Button>
            ) : null}
          </>
        ) : null}
      </div>
    </>
  );
}
