import { memo, useCallback, useMemo, useRef, useState } from "react";
import { ExternalLink, Link2 } from "lucide-react";
import { FolderIcon, NewWorkspaceLinedIcon, cn } from "@tutti-os/ui-system";
import { WorkspaceUserProjectSelect } from "@tutti-os/workspace-user-project/ui";
import type { WorkspaceUserProjectI18nRuntime } from "@tutti-os/workspace-user-project/i18n";
import { BareIconButton } from "@tutti-os/ui-system/components";
import { CanvasNodeTrashLinedIcon } from "../../shared/canvasNodeChromeIcons";
import { PinFilledIcon } from "../../../app/renderer/components/icons/PinFilledIcon";
import { PinLinedIcon } from "../../../app/renderer/components/icons/PinLinedIcon";
import { useAgentHostApi } from "../../../agentActivityHost";
import { createAgentGUIUserProjectSelectionApi } from "../agentGuiUserProjectSelectionApi";
import { resolveAgentGuiSessionProviderFlatIconUrl } from "../../../agentGuiSessionProviderIconUrls";
import {
  resolveAgentTargetPresentation,
  useAgentTargetPresentations
} from "../../../shared/AgentTargetPresentationContext";
import {
  useAgentTargetInfoRenderer,
  useAgentTargetInfoTarget
} from "../../../shared/AgentTargetInfoRendererContext";
import { AgentTargetInfoTooltip } from "../../../shared/AgentTargetInfoTooltip";
import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import type { AgentGUINodeViewModel } from "../model/agentGuiNodeTypes";
import type { AgentGUIConversationActivityPriorityReason } from "../model/agentGuiConversationActivityView";
import type { AgentGUIViewLabels } from "../AgentGUINodeView";
import type { AgentGUIConversationRailLabels } from "./agentGUIConversationRailLabels";
import styles from "../AgentGUINode.styles";
import { conversationPlainTitle } from "./agentGUIViewUtils";
import { AgentGUIConversationRailRelativeTime } from "./AgentGUIConversationRailClock";
import {
  AgentGUIConversationActionsContextMenu,
  AgentGUIConversationActionsDropdown,
  useConversationActionGroups
} from "./AgentGUIConversationActionsMenu";
import { useAgentGUIConversationRailPeerPairing } from "./agentGUIConversationRailPeerPairingContext";
import { conversationRailPeerPairCount } from "../model/conversationRailPeerPairing";

type AgentGUIConversationIconPresentation =
  | { kind: "image"; url: string }
  | { kind: "mask"; url: string };

function agentGUIConversationIconPresentation(
  provider: string | undefined,
  agentTargetId: string | null | undefined,
  workspaceId: string,
  agentTargets: ReturnType<typeof useAgentTargetPresentations>
): AgentGUIConversationIconPresentation | null {
  const targetPresentation = resolveAgentTargetPresentation({
    agentTargetId: agentTargetId ?? "",
    agentTargets,
    workspaceId
  });
  const maskIconUrl = targetPresentation?.maskIconUrl?.trim() ?? "";
  if (maskIconUrl) {
    return { kind: "mask", url: maskIconUrl };
  }
  const iconUrl = targetPresentation?.iconUrl?.trim() ?? "";
  if (iconUrl) {
    return { kind: "image", url: iconUrl };
  }
  const providerIconUrl = resolveAgentGuiSessionProviderFlatIconUrl(provider);
  return providerIconUrl ? { kind: "mask", url: providerIconUrl } : null;
}

function agentGUIConversationRailTitle(
  item: AgentGUINodeViewModel["rail"]["conversations"][number],
  labels: AgentGUIConversationRailLabels,
  uiLanguage: UiLanguage
): string {
  return conversationPlainTitle(item, labels, uiLanguage);
}

interface AgentGUIConversationRailItemProps {
  item: AgentGUINodeViewModel["rail"]["conversations"][number];
  active: boolean;
  isPendingDeleteConversation: boolean;
  isDeletingConversation: boolean;
  isRailInteractionLocked: () => boolean;
  labels: AgentGUIConversationRailLabels;
  uiLanguage: UiLanguage;
  workspaceId: string;
  registerItemElement: (itemId: string, element: HTMLDivElement | null) => void;
  /**
   * 该条属于「当前会话 + 它的对端」这一组：左侧画一条竖线括起来。
   * `"end"` = 这一组的最后一行，竖线到此为止（中间行的线要贴到下一行，
   * 否则整组看上去是几段断线，而不是一条括号，T5）。
   */
  peerPairGrouped?: false | "member" | "end";
  onSelectConversation: (agentSessionId: string) => void;
  onToggleConversationPinned: (agentSessionId: string, pinned: boolean) => void;
  onMarkConversationUnread: (agentSessionId: string) => void;
  onOpenConversationWindow?: (agentSessionId: string) => void;
  onRequestDeleteConversation: (agentSessionId: string) => void;
  onRequestRenameConversation: (
    conversation: AgentGUINodeViewModel["rail"]["conversations"][number]
  ) => void;
  onCancelDeleteConversation: () => void;
  onConfirmDeleteConversation: () => void;
  presentation?: {
    kind: "activity";
    priorityReason: AgentGUIConversationActivityPriorityReason | null;
    projectLabel: string | null;
    secondary: {
      kind: "project" | "source";
      text: string;
    };
  };
}

export const AgentGUIConversationRailItem = memo(
  function AgentGUIConversationRailItem({
    item,
    active,
    isPendingDeleteConversation,
    isDeletingConversation,
    isRailInteractionLocked,
    labels,
    uiLanguage,
    workspaceId,
    peerPairGrouped,
    registerItemElement,
    onSelectConversation,
    onToggleConversationPinned,
    onMarkConversationUnread,
    onOpenConversationWindow,
    onRequestDeleteConversation,
    onRequestRenameConversation,
    onCancelDeleteConversation,
    onConfirmDeleteConversation,
    presentation
  }: AgentGUIConversationRailItemProps): React.JSX.Element {
    "use memo";
    const pinned = (item.pinnedAtUnixMs ?? 0) > 0;
    const peerPairing = useAgentGUIConversationRailPeerPairing();
    // 徽标数 = 该会话未撤销的配对数；宿主不支持或为 0 时不画。
    const peerPairCount = peerPairing.supported
      ? conversationRailPeerPairCount(peerPairing.index, item.id)
      : 0;
    // 「待配对」标记：虚线描边 + 右槽的 chip，chip 优先于徽标显示（T2）。
    const peerPairMarked =
      peerPairing.supported && peerPairing.marked?.sessionId === item.id;
    const peerPairSlot = peerPairMarked ? "chip" : peerPairCount > 0 ? "badge" : null;
    const [actionsActivated, setActionsActivated] = useState(false);
    const [targetInfoOpen, setTargetInfoOpen] = useState(false);
    const agentTargets = useAgentTargetPresentations();
    const renderAgentTargetInfo = useAgentTargetInfoRenderer();
    const targetInfoTarget = useAgentTargetInfoTarget(item.agentTargetId);
    const hasTargetInfo = Boolean(renderAgentTargetInfo && targetInfoTarget);
    const targetInfoFocusedRef = useRef(false);
    const targetInfoPointerOverIconRef = useRef(false);
    const handleTargetInfoOpenChange = (open: boolean): void => {
      if (
        !open ||
        targetInfoFocusedRef.current ||
        targetInfoPointerOverIconRef.current
      ) {
        setTargetInfoOpen(open);
      }
    };
    const handleTargetInfoIconPointerMove = (): void => {
      targetInfoPointerOverIconRef.current = true;
      setTargetInfoOpen(true);
    };
    const handleTargetInfoIconPointerLeave = (): void => {
      targetInfoPointerOverIconRef.current = false;
      if (!targetInfoFocusedRef.current) {
        setTargetInfoOpen(false);
      }
    };
    const handleTargetInfoFocus = (): void => {
      targetInfoFocusedRef.current = true;
      setTargetInfoOpen(true);
    };
    const handleTargetInfoBlur = (): void => {
      targetInfoFocusedRef.current = false;
      setTargetInfoOpen(false);
    };
    const conversationIcon = agentGUIConversationIconPresentation(
      item.provider,
      item.agentTargetId,
      workspaceId,
      agentTargets
    );
    const conversationIconNode =
      conversationIcon?.kind === "mask" ? (
        <span
          aria-hidden="true"
          className={cn(
            styles.conversationProviderIcon,
            styles.conversationProviderMaskIcon
          )}
          style={{
            WebkitMaskImage: `url("${conversationIcon.url}")`,
            maskImage: `url("${conversationIcon.url}")`
          }}
          onPointerLeave={
            hasTargetInfo ? handleTargetInfoIconPointerLeave : undefined
          }
          onPointerMove={
            hasTargetInfo ? handleTargetInfoIconPointerMove : undefined
          }
        />
      ) : conversationIcon ? (
        <img
          alt=""
          aria-hidden="true"
          className={cn(
            styles.conversationProviderIcon,
            styles.conversationProviderImage
          )}
          draggable={false}
          src={conversationIcon.url}
          onPointerLeave={
            hasTargetInfo ? handleTargetInfoIconPointerLeave : undefined
          }
          onPointerMove={
            hasTargetInfo ? handleTargetInfoIconPointerMove : undefined
          }
        />
      ) : null;
    const setItemElement = useCallback(
      (element: HTMLDivElement | null) => {
        registerItemElement(item.id, element);
      },
      [item.id, registerItemElement]
    );
    const handleMouseLeave = useCallback(() => {
      if (isPendingDeleteConversation && !isRailInteractionLocked()) {
        onCancelDeleteConversation();
      }
    }, [
      isPendingDeleteConversation,
      isRailInteractionLocked,
      onCancelDeleteConversation
    ]);
    const handleSelect = (): void => {
      if (!isRailInteractionLocked()) {
        onSelectConversation(item.id);
      }
    };
    const handleRequestRename = (): void => {
      if (!isRailInteractionLocked()) {
        onRequestRenameConversation(item);
      }
    };
    // Plain closures on purpose: the component memo budget caps leaf caches
    // at 5 and "use memo" lets the compiler stabilize these.
    const handleTogglePinned = (): void => {
      if (!isRailInteractionLocked()) {
        onToggleConversationPinned(item.id, !pinned);
      }
    };
    const handleRequestDelete = (): void => {
      if (!isRailInteractionLocked()) {
        onRequestDeleteConversation(item.id);
      }
    };
    const handleOpenConversationWindow = (): void => {
      if (!isRailInteractionLocked()) {
        onOpenConversationWindow?.(item.id);
      }
    };
    const canMarkUnread = Boolean(
      !item.hasUnreadCompletion &&
      item.isImported !== true &&
      (item.unreadCompletionKey ||
        item.status === "completed" ||
        item.status === "ready")
    );
    const menu = useConversationActionGroups({
      canMarkUnread,
      conversation: item,
      isInteractionLocked: isRailInteractionLocked,
      labels,
      uiLanguage,
      workspaceId,
      onMarkConversationUnread,
      onOpenConversationWindow,
      onRequestRenameConversation
    });
    const activityPresentation =
      presentation?.kind === "activity" ? presentation : null;
    const conversationTitle = agentGUIConversationRailTitle(
      item,
      labels,
      uiLanguage
    );
    const activityStatusLabel = activityPresentation
      ? agentGUIConversationActivityStatusLabel(
          item,
          activityPresentation.priorityReason,
          labels
        )
      : null;
    const activityAccessibleLabel = activityPresentation
      ? [
          conversationTitle,
          activityPresentation.projectLabel,
          activityStatusLabel
        ]
          .filter((value): value is string => Boolean(value))
          .join(", ")
      : undefined;
    const conversationTitleRow = (
      <span className={styles.conversationTitleRow}>
        {conversationIconNode}
        <span className={styles.conversationTitle}>{conversationTitle}</span>
      </span>
    );
    const conversationSelect = (
      <button
        type="button"
        aria-label={activityAccessibleLabel}
        className={styles.conversationSelect}
        onClick={handleSelect}
        onBlur={hasTargetInfo ? handleTargetInfoBlur : undefined}
        onDoubleClick={(event) => {
          event.preventDefault();
          handleRequestRename();
        }}
        onFocus={hasTargetInfo ? handleTargetInfoFocus : undefined}
      >
        {activityPresentation ? (
          <span className={styles.conversationActivityText}>
            {conversationTitleRow}
            <span
              aria-hidden="true"
              className={styles.conversationActivitySecondary}
            >
              {activityPresentation.secondary.kind === "project" ? (
                <FolderIcon
                  aria-hidden="true"
                  className={styles.conversationActivitySecondaryIcon}
                />
              ) : null}
              <span>{activityPresentation.secondary.text}</span>
            </span>
          </span>
        ) : (
          conversationTitleRow
        )}
        <AgentGUIConversationRailRelativeTime
          hideTime={Boolean(activityPresentation)}
          item={item}
          labels={labels}
        />
      </button>
    );
    const conversationSelectWithTargetInfo =
      renderAgentTargetInfo && targetInfoTarget ? (
        <AgentTargetInfoTooltip
          align="start"
          fallbackLabel={targetInfoTarget.label}
          onOpenChange={handleTargetInfoOpenChange}
          open={targetInfoOpen}
          renderer={renderAgentTargetInfo}
          side="bottom"
          sideOffset={6}
          surface="conversation-rail"
          target={targetInfoTarget}
        >
          {conversationSelect}
        </AgentTargetInfoTooltip>
      ) : (
        conversationSelect
      );
    const row = (
      <div
        ref={setItemElement}
        className={styles.conversationItem}
        data-active={active}
        data-presentation={activityPresentation?.kind}
        data-pinned={pinned}
        data-pending-delete={isPendingDeleteConversation}
        data-peer-pair-group={peerPairGrouped || undefined}
        data-peer-pair-marked={peerPairMarked ? "true" : undefined}
        data-peer-pair-slot={peerPairSlot ?? undefined}
        data-testid={`agent-gui-conversation-item-${item.id}`}
        onContextMenuCapture={(event) => {
          if (isRailInteractionLocked()) {
            event.preventDefault();
            event.stopPropagation();
            return;
          }
          setActionsActivated(true);
        }}
        onFocusCapture={() => setActionsActivated(true)}
        onMouseLeave={handleMouseLeave}
        onPointerEnter={() => setActionsActivated(true)}
      >
        {conversationSelectWithTargetInfo}
        {/* 徽标与 chip 共用条目右侧的同一个槽位（flex 项，不再绝对定位压在
            相对时间上，T1）；被标记时槽位让给 chip。 */}
        {peerPairSlot === "chip" ? (
          <span
            className={styles.conversationPeerPairChip}
            data-testid={`agent-gui-conversation-peer-pair-chip-${item.id}`}
            title={labels.peerPairPending}
          >
            {labels.peerPairPending}
          </span>
        ) : peerPairSlot === "badge" ? (
          <span
            className={styles.conversationPeerPairBadge}
            data-testid={`agent-gui-conversation-peer-pair-badge-${item.id}`}
            title={labels.peerPairUnpair(peerPairCount)}
          >
            <Link2 aria-hidden="true" />
            <span>{peerPairCount}</span>
          </span>
        ) : null}
        {actionsActivated || isPendingDeleteConversation ? (
          <div className={styles.conversationActions}>
            {isPendingDeleteConversation ? (
              <button
                type="button"
                className={styles.conversationDeleteButton}
                aria-label={labels.deleteSessionConfirm}
                title={labels.deleteSessionConfirm}
                disabled={isDeletingConversation}
                onClick={(event) => {
                  event.stopPropagation();
                  if (!isRailInteractionLocked()) {
                    onConfirmDeleteConversation();
                  }
                }}
              >
                <span className={styles.conversationDeleteConfirmText}>
                  {labels.deleteSessionConfirm}
                </span>
              </button>
            ) : (
              <>
                {onOpenConversationWindow ? (
                  <BareIconButton
                    className={styles.conversationOpenWindowButton}
                    aria-label={labels.openConversationWindow}
                    title={labels.openConversationWindow}
                    size="md"
                    onPointerDown={(event) => {
                      event.stopPropagation();
                    }}
                    onMouseDown={(event) => {
                      event.stopPropagation();
                    }}
                    onClick={(event) => {
                      event.stopPropagation();
                      handleOpenConversationWindow();
                    }}
                  >
                    <ExternalLink aria-hidden="true" />
                  </BareIconButton>
                ) : null}
                <BareIconButton
                  className={styles.conversationPinButton}
                  data-testid="agent-gui-session-pin"
                  aria-label={pinned ? labels.unpinSession : labels.pinSession}
                  title={pinned ? labels.unpinSession : labels.pinSession}
                  size="md"
                  onPointerDown={(event) => {
                    event.stopPropagation();
                  }}
                  onMouseDown={(event) => {
                    event.stopPropagation();
                  }}
                  onClick={(event) => {
                    event.stopPropagation();
                    handleTogglePinned();
                  }}
                >
                  {pinned ? (
                    <PinFilledIcon aria-hidden="true" />
                  ) : (
                    <PinLinedIcon aria-hidden="true" />
                  )}
                </BareIconButton>
                <BareIconButton
                  className={styles.conversationDeleteButton}
                  aria-label={labels.deleteSession}
                  title={labels.deleteSession}
                  size="md"
                  onPointerDown={(event) => {
                    event.stopPropagation();
                  }}
                  onMouseDown={(event) => {
                    event.stopPropagation();
                  }}
                  onClick={(event) => {
                    event.stopPropagation();
                    handleRequestDelete();
                  }}
                >
                  <CanvasNodeTrashLinedIcon aria-hidden="true" />
                </BareIconButton>
                <AgentGUIConversationActionsDropdown
                  buttonClassName={styles.conversationMoreButton}
                  menu={menu}
                  moreSessionActionsLabel={labels.moreSessionActions}
                />
              </>
            )}
          </div>
        ) : null}
      </div>
    );

    return (
      <AgentGUIConversationActionsContextMenu menu={menu}>
        {row}
      </AgentGUIConversationActionsContextMenu>
    );
  }
);

function agentGUIConversationActivityStatusLabel(
  item: AgentGUINodeViewModel["rail"]["conversations"][number],
  priorityReason: AgentGUIConversationActivityPriorityReason | null,
  labels: AgentGUIConversationRailLabels
): string | null {
  if (item.needsUserAction || item.status === "waiting") {
    return labels.activityStatusWaiting;
  }
  if (item.status === "working") return labels.activityStatusWorking;
  if (item.status === "failed") return labels.activityStatusFailed;
  if (item.hasUnreadCompletion) return labels.activityStatusUnread;
  if (priorityReason !== null) {
    return labels.activityStatusRecentlyActive;
  }
  return null;
}

export function AgentGUIProjectRailHeader({
  disabled,
  labels,
  selectProjectDirectory,
  workspaceUserProjectI18n
}: {
  disabled?: boolean;
  labels: Pick<
    AgentGUIViewLabels,
    "projectRailCreateProject" | "projectRailLinkExistingProject"
  >;
  selectProjectDirectory?: () => Promise<{ path: string } | null>;
  workspaceUserProjectI18n: WorkspaceUserProjectI18nRuntime;
}): React.JSX.Element {
  "use memo";
  const agentHostApi = useAgentHostApi();
  const userProjectApi = useMemo(
    () =>
      createAgentGUIUserProjectSelectionApi({
        selectProjectDirectory,
        userProjects: agentHostApi.userProjects
      }),
    [agentHostApi.userProjects, selectProjectDirectory]
  );

  return (
    <div className={styles.projectRailHeader}>
      <div className={styles.projectRailTitle}>
        <span>
          {workspaceUserProjectI18n.tFirst(["projectSelect.projectLabel"])}
        </span>
      </div>
      <div className={styles.projectRailAddProject}>
        <WorkspaceUserProjectSelect
          api={userProjectApi}
          classNames={{
            content: cn(
              styles.composerMenuContent,
              "w-[240px] min-w-[240px] nodrag [-webkit-app-region:no-drag]"
            ),
            item: cn(
              styles.composerMenuItem,
              "nodrag [-webkit-app-region:no-drag]"
            ),
            trigger: cn(
              styles.projectRailAddProjectTrigger,
              "nodrag [-webkit-app-region:no-drag]"
            )
          }}
          contentAlign="end"
          contentSide="bottom"
          contentSideOffset={6}
          disabled={disabled}
          i18n={workspaceUserProjectI18n}
          labels={{
            addProject: labels.projectRailCreateProject,
            createProjectTitle: labels.projectRailCreateProject,
            linkExistingProject: labels.projectRailLinkExistingProject,
            projectLabel: workspaceUserProjectI18n.tFirst([
              "projectSelect.addProject"
            ])
          }}
          renderAddProjectIcon={() => (
            <NewWorkspaceLinedIcon
              aria-hidden
              data-workspace-user-project-add-icon="true"
              size={15}
            />
          )}
          selectedProjectPath={null}
          service={agentHostApi.userProjects?.service ?? null}
          shouldApplyPreparedSelection={false}
          showCreateProjectAction
          showKnownProjectOptions={false}
          showNoProjectAction={false}
          onProjectPathChange={() => {}}
        />
        <NewWorkspaceLinedIcon
          aria-hidden
          className={styles.projectRailAddProjectIcon}
        />
      </div>
    </div>
  );
}
