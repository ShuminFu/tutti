import type { AgentGUIProvider } from "../../../types";
import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import type { WorkspaceLinkAction } from "../../../actions/workspaceLinkActions";
import type { WorkspaceUserProjectI18nRuntime } from "@tutti-os/workspace-user-project/i18n";
import type { AgentGUINodeViewModel } from "../model/agentGuiNodeTypes";
import type { useAgentGUIConversationRailQuery } from "../controller/useAgentGUIConversationRailQuery";
import type { AgentGUIConversationRailLabels } from "./agentGUIConversationRailLabels";

export interface AgentGUIConversationRailControllerProps {
  activityContextKey?: string;
  conversations: AgentGUINodeViewModel["rail"]["conversations"];
  currentUserId?: string | null;
  nodeId?: string | null;
  footer?: React.ReactNode;
  /** Leading cell for hosts that supply their own top bar. */
  railToolbarLeadingAccessory?: React.ReactNode;
  workspaceId: string;
  userProjects: AgentGUINodeViewModel["rail"]["userProjects"];
  activeConversation: AgentGUINodeViewModel["rail"]["activeConversation"];
  activeConversationId: string | null;
  revealRequest: AgentGUINodeViewModel["rail"]["revealRequest"];
  pendingDeleteConversationId: string | null;
  isLoadingConversations: boolean;
  isDeletingConversation: boolean;
  isDeletingProjectConversations: boolean;
  isUserProjectMutationPending?: boolean;
  labels: AgentGUIConversationRailLabels;
  workspaceUserProjectI18n: WorkspaceUserProjectI18nRuntime;
  uiLanguage: UiLanguage;
  createConversationDisabled: boolean;
  isCollapsed: boolean;
  agentTargets: AgentGUINodeViewModel["rail"]["agentTargets"];
  agentTargetsLoading: AgentGUINodeViewModel["rail"]["agentTargetsLoading"];
  conversationFilter: AgentGUINodeViewModel["rail"]["conversationFilter"];
  /** Shares the query lock with host-dispatched session actions. */
  registerInteractionLockProbe?: (probe: (() => boolean) | null) => void;
  onUpdateConversationFilter: (
    filter: AgentGUINodeViewModel["rail"]["conversationFilter"]
  ) => void;
  onSelectConversationFilterTarget: AgentGUIConversationFilterTargetSelection;
  onCreateConversation: (options?: {
    projectPath?: string | null;
    source?: string;
  }) => void;
  onSelectConversation: (agentSessionId: string) => void;
  onToggleConversationPinned: (agentSessionId: string, pinned: boolean) => void;
  onMarkConversationUnread: (agentSessionId: string) => void;
  onOpenProjectFiles?: ((action: WorkspaceLinkAction) => void) | null;
  onOpenConversationWindow?: (agentSessionId: string) => void;
  selectProjectDirectory?: () => Promise<{ path: string } | null>;
  onRemoveProject: (path: string) => void;
  onMoveProject: (
    projectId: string,
    beforeProjectId: string | null
  ) => Promise<void>;
  onToggleProjectPinned: (projectId: string, pinned: boolean) => Promise<void>;
  onConfirmDeleteProjectConversations: (
    sectionKey?: string,
    agentTargetId?: string | null
  ) => Promise<string[]>;
  onConfirmDeleteConversations: (agentSessionIds: string[]) => void;
  onRequestDeleteConversation: (agentSessionId: string) => void;
  onRequestRenameConversation: (
    conversation: AgentGUINodeViewModel["rail"]["conversations"][number]
  ) => void;
  onCancelDeleteConversation: () => void;
  onConfirmDeleteConversation: () => void;
}

export type AgentGUIConversationRailPaneProps =
  AgentGUIConversationRailControllerProps & {
    conversationQuery: string;
    onConversationQueryChange: (query: string) => void;
    railQuery: ReturnType<typeof useAgentGUIConversationRailQuery>;
  };

export type AgentGUIConversationRailState = Omit<
  AgentGUIConversationRailControllerProps,
  "conversations" | "userProjects" | "workspaceId"
>;

export interface AgentGUIConversationFilterTargetInput {
  provider: AgentGUIProvider;
  agentTargetId: string;
}

export type AgentGUIConversationFilterTargetSelection = (
  input: AgentGUIConversationFilterTargetInput
) => void;

export type AgentGUIProjectActionDialog =
  | {
      kind: "batch-delete";
      conversationCount: number;
      label: string;
      sessionIds: string[];
    }
  | {
      kind: "batch-delete-conversations";
      conversationCount: number;
      label: string;
      sessionIds: string[];
    }
  | {
      kind: "remove";
      label: string;
      path: string;
    };
