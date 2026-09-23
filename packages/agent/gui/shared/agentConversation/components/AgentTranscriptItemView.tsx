import { memo, useCallback, type JSX, type ReactNode } from "react";
import type { WorkspaceLinkAction } from "../../../contexts/workspace/presentation/renderer/actions/workspaceLinkActions";
import type { AgentMessageMarkdownWorkspaceAppIcon } from "../../AgentMessageMarkdown";
import type { AgentGUIProviderSkillOption } from "../../../agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import { resolveAgentConversationLinkAction } from "../actions/agentConversationLinkActions";
import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";
import type { AgentConversationParticipantPresentation } from "../contracts/agentConversationParticipantPresentation";
import { AgentGeneratedImageRow } from "./AgentGeneratedImageRow";
import { AgentGoalControlRow } from "./AgentGoalControlRow";
import { AgentMcpAppRow } from "./AgentMcpAppRow";
import { AgentMessageBlock } from "./AgentMessageBlock";
import { AgentProcessingRow } from "./AgentProcessingRow";
import { AgentToolGroupRow } from "./AgentToolGroupRow";
import { AgentTurnSummaryRow } from "./AgentTurnSummaryRow";
import type { AgentUserMessageEditRetryControl } from "./AgentUserMessageEditRetry";

interface AgentTranscriptItemViewProps {
  sessionId?: string;
  workspaceRoot: string | null;
  basePath: string;
  row: AgentTranscriptRowVM;
  editRetry?: AgentUserMessageEditRetryControl;
  labels: {
    toolCallsLabel: (count: number) => string;
    thinkingLabel: string;
    processing: string;
    turnSummary: string;
    rawTimelineJson?: string;
  };
  onLinkAction?: (action: WorkspaceLinkAction) => void;
  onAuthLogin?: (provider?: string | null) => void;
  provider?: string | null;
  availableSkills?: readonly AgentGUIProviderSkillOption[];
  workspaceAppIcons?: readonly AgentMessageMarkdownWorkspaceAppIcon[];
  showRawTimelineJson?: boolean;
  participantPresentation?: AgentConversationParticipantPresentation;
  showParticipantHeader?: boolean;
  isActiveTurn?: boolean;
  turnSettled?: boolean;
  processingPaused?: boolean;
  toolGroupExpanded?: boolean;
  toolGroupExpansionKey?: string;
  onToolGroupExpandedChange?: (key: string, expanded: boolean) => void;
  footerAction?: ReactNode;
}

export const AgentTranscriptItemView = memo(function AgentTranscriptItemView({
  sessionId,
  workspaceRoot,
  basePath,
  row,
  editRetry,
  labels,
  onLinkAction,
  onAuthLogin,
  provider,
  availableSkills,
  workspaceAppIcons,
  showRawTimelineJson = false,
  participantPresentation,
  showParticipantHeader,
  isActiveTurn = false,
  turnSettled = false,
  processingPaused = false,
  toolGroupExpanded,
  toolGroupExpansionKey,
  onToolGroupExpandedChange,
  footerAction
}: AgentTranscriptItemViewProps): JSX.Element {
  "use memo";

  const handleLinkClick = useCallback(
    (href: string) => {
      const action = resolveAgentConversationLinkAction({
        workspaceRoot,
        basePath,
        href,
        source: "agent-markdown"
      });
      if (action) {
        onLinkAction?.(action);
      }
    },
    [basePath, onLinkAction, workspaceRoot]
  );
  switch (row.kind) {
    case "generated-image":
      return <AgentGeneratedImageRow row={row} />;
    case "mcp-app":
      return <AgentMcpAppRow row={row} />;
    case "goal-control":
      return (
        <AgentGoalControlRow
          row={row}
          availableSkills={availableSkills}
          workspaceAppIcons={workspaceAppIcons}
        />
      );
    case "message":
      return (
        <AgentMessageBlock
          sessionId={sessionId}
          workspaceRoot={workspaceRoot}
          basePath={basePath}
          row={row}
          editRetry={editRetry}
          onLinkAction={onLinkAction}
          onAuthLogin={onAuthLogin}
          provider={provider}
          availableSkills={availableSkills}
          workspaceAppIcons={workspaceAppIcons}
          thinkingLabel={labels.thinkingLabel}
          toolCallsLabel={labels.toolCallsLabel}
          showRawTimelineJson={showRawTimelineJson}
          rawTimelineJsonLabel={labels.rawTimelineJson}
          participantPresentation={participantPresentation}
          showParticipantHeader={showParticipantHeader}
          isActiveTurn={isActiveTurn}
          turnSettled={turnSettled}
          footerAction={footerAction}
        />
      );
    case "tool-group":
      return (
        <AgentToolGroupRow
          sessionId={sessionId}
          row={row}
          label={labels.toolCallsLabel}
          thinkingLabel={labels.thinkingLabel}
          onLinkClick={handleLinkClick}
          showRawTimelineJson={showRawTimelineJson}
          rawTimelineJsonLabel={labels.rawTimelineJson}
          expanded={row.grouped ? toolGroupExpanded : undefined}
          onExpandedChange={row.grouped ? onToolGroupExpandedChange : undefined}
          expansionKey={toolGroupExpansionKey}
        />
      );
    case "turn-summary":
      return (
        <AgentTurnSummaryRow
          row={row}
          workspaceRoot={workspaceRoot}
          basePath={basePath}
          label={labels.turnSummary}
          onLinkAction={onLinkAction}
        />
      );
    case "processing":
      return (
        <AgentProcessingRow
          row={row}
          label={labels.processing}
          paused={processingPaused}
        />
      );
  }
});
