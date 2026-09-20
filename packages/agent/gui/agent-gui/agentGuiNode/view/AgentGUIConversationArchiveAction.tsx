import { useState } from "react";
import { Archive, ArchiveRestore } from "lucide-react";
import { BareIconButton, toast } from "@tutti-os/ui-system";
import { useOptionalAgentGUIRuntime } from "../../../agentActivityRuntime";
import { useOptionalAgentHostApi } from "../../../agentActivityHost";
import { useTranslation } from "../../../i18n";
import { getAgentGUIErrorMessage } from "../controller/agentGuiController.errors";

export function AgentGUIConversationArchiveAction(props: {
  agentSessionId: string;
  archived: boolean;
  workspaceId: string;
  disabled?: boolean;
  isInteractionLocked: () => boolean;
}) {
  const runtime = useOptionalAgentGUIRuntime();
  const host = useOptionalAgentHostApi();
  const { t } = useTranslation();
  const [pending, setPending] = useState(false);
  if (!runtime?.setSessionArchived) return null;
  const label = t(
    props.archived
      ? "agentHost.agentGui.restoreArchive"
      : "agentHost.agentGui.archiveSession"
  );
  return (
    <BareIconButton
      aria-label={label}
      title={label}
      size="md"
      disabled={pending || props.disabled}
      onPointerDown={(event) => event.stopPropagation()}
      onClick={(event) => {
        event.stopPropagation();
        if (props.isInteractionLocked() || pending) return;
        setPending(true);
        void runtime.setSessionArchived!({
          workspaceId: props.workspaceId,
          agentSessionId: props.agentSessionId,
          archived: !props.archived
        })
          .catch((error) =>
            (host?.toast?.error ?? toast.error)(getAgentGUIErrorMessage(error))
          )
          .finally(() => setPending(false));
      }}
    >
      {props.archived ? (
        <ArchiveRestore aria-hidden="true" />
      ) : (
        <Archive aria-hidden="true" />
      )}
    </BareIconButton>
  );
}
