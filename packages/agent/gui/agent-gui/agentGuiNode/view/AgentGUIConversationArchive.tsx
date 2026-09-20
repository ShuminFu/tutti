import { useMemo, useState } from "react";
import { Archive } from "lucide-react";
import {
  BareIconButton,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  ScrollArea
} from "@tutti-os/ui-system";
import { selectWorkspaceAgentConsumerSessions } from "@tutti-os/agent-activity-core";
import {
  useOptionalAgentGUIRuntime,
  type AgentGUIRuntime
} from "../../../agentActivityRuntime";
import { useTranslation } from "../../../i18n";
import { useEngineSelector } from "../../../shared/engine/useEngineSelector";
import { useAgentConversationMinuteNowUnixMs } from "../../../shared/agentConversation/components/AgentConversationClock";
import { projectCanonicalAgentGUIConversationSummaries } from "../../../shared/agentGUIConversationSummaryProjection";
import { stabilizeConversationSectionItems } from "../model/agentGuiConversationRail";
import { createAgentGUIConversationRailQueryController } from "../../../agentConversationRailController";
import { AgentGUIConversationRailItem } from "./AgentGUIConversationRailItem";
import type { AgentGUIConversationRailPaneProps } from "./AgentGUIConversationRailPane";

export type ArchiveProps = Pick<
  AgentGUIConversationRailPaneProps,
  | "workspaceId"
  | "activeConversationId"
  | "labels"
  | "uiLanguage"
  | "pendingDeleteConversationId"
  | "isDeletingConversation"
  | "onSelectConversation"
  | "onRequestDeleteConversation"
  | "onCancelDeleteConversation"
  | "onConfirmDeleteConversation"
  | "onRequestRenameConversation"
  | "onToggleConversationPinned"
  | "onMarkConversationUnread"
>;

export function AgentGUIConversationArchive(props: ArchiveProps) {
  const runtime = useOptionalAgentGUIRuntime();
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  if (!runtime?.setSessionArchived || !runtime.listSessionSectionPage)
    return null;
  return (
    <>
      <BareIconButton
        size="md"
        aria-label={t("agentHost.agentGui.archiveView")}
        title={t("agentHost.agentGui.archiveView")}
        onClick={() => setOpen(true)}
      >
        <Archive aria-hidden="true" />
      </BareIconButton>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-lg nodrag [-webkit-app-region:no-drag]">
          <DialogHeader>
            <DialogTitle>{t("agentHost.agentGui.archiveView")}</DialogTitle>
            <DialogDescription>
              {t("agentHost.agentGui.archiveRetention")}
            </DialogDescription>
          </DialogHeader>
          {open ? (
            <ArchiveContents
              {...props}
              runtime={runtime}
              onRequestDeleteConversation={(id) => {
                setOpen(false);
                props.onRequestDeleteConversation(id);
              }}
              onOpenSession={(id) => {
                setOpen(false);
                props.onSelectConversation(id);
              }}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  );
}

function ArchiveContents(
  props: ArchiveProps & {
    runtime: AgentGUIRuntime;
    onOpenSession: (id: string) => void;
  }
) {
  const { t } = useTranslation();
  const engine = props.runtime.getSessionEngine(props.workspaceId);
  const controller = useMemo(
    () =>
      createAgentGUIConversationRailQueryController({
        engine,
        getActiveConversationId: () => null,
        runtime: props.runtime,
        sectionPageSize: 20,
        workspaceId: props.workspaceId
      }),
    [engine, props.runtime, props.workspaceId]
  );
  const store = useMemo(
    () => ({
      getSnapshot: controller.getSnapshot,
      subscribe(listener: () => void) {
        const unsubscribe = controller.subscribe(listener);
        controller.configure({
          archiveOnly: true,
          conversationFilter: { kind: "all" },
          userProjects: []
        });
        const detach = controller.attach();
        return () => {
          unsubscribe();
          detach();
        };
      }
    }),
    [controller]
  );
  const query = useEngineSelector(store, (state) => state);
  const selectItems = useMemo(() => {
    const ids = new Set(
      query.runtimeRailMemberships?.flatMap((section) => section.sessionIds) ??
        []
    );
    let previous: ReturnType<
      typeof projectCanonicalAgentGUIConversationSummaries
    > = [];
    return (state: ReturnType<typeof engine.getSnapshot>) => {
      const byId = new Map(
        projectCanonicalAgentGUIConversationSummaries(
          selectWorkspaceAgentConsumerSessions(state).filter(
            ({ session }) =>
              ids.has(session.agentSessionId) &&
              (session.archivedAtUnixMs ?? 0) > 0
          )
        ).map((item) => [item.id, item])
      );
      previous = stabilizeConversationSectionItems(
        previous,
        [...ids].flatMap((id) => {
          const item = byId.get(id);
          return item ? [item] : [];
        })
      );
      return previous;
    };
  }, [query.runtimeRailMemberships]);
  const items = useEngineSelector(engine, selectItems);
  const now = useAgentConversationMinuteNowUnixMs();
  const page = query.sectionPageStates.get("archive");
  return (
    <ScrollArea className="h-96 min-h-0" scrollbarMode="native">
      {query.runtimeRailSectionsPending && !items.length ? (
        <p role="status" className="p-3 text-sm text-muted-foreground">
          {props.labels.loadingConversations}
        </p>
      ) : null}
      {query.runtimeRailFailed ? (
        <Button variant="ghost" onClick={() => void controller.refresh()}>
          {props.labels.retryConversations}
        </Button>
      ) : null}
      {!query.runtimeRailSectionsPending && !items.length ? (
        <p className="p-3 text-sm text-muted-foreground">
          {t("agentHost.agentGui.archiveEmpty")}
        </p>
      ) : null}
      {items.map((item) => (
        <AgentGUIConversationRailItem
          key={item.id}
          item={item}
          active={props.activeConversationId === item.id}
          isPendingDeleteConversation={
            props.pendingDeleteConversationId === item.id
          }
          isDeletingConversation={props.isDeletingConversation}
          isRailInteractionLocked={controller.isInteractionLocked}
          labels={props.labels}
          uiLanguage={props.uiLanguage}
          workspaceId={props.workspaceId}
          registerItemElement={() => {}}
          onSelectConversation={props.onOpenSession}
          onToggleConversationPinned={props.onToggleConversationPinned}
          onMarkConversationUnread={props.onMarkConversationUnread}
          onRequestDeleteConversation={props.onRequestDeleteConversation}
          onRequestRenameConversation={props.onRequestRenameConversation}
          onCancelDeleteConversation={props.onCancelDeleteConversation}
          onConfirmDeleteConversation={props.onConfirmDeleteConversation}
          presentation={{
            kind: "activity",
            priorityReason: null,
            projectLabel: item.railSectionKey ?? "",
            secondary: {
              kind: "source",
              text:
                archiveRemainingDays(item.archivedAtUnixMs!, now) === 0
                  ? t("agentHost.agentGui.archiveExpired")
                  : t("agentHost.agentGui.archiveRemaining", {
                      days: archiveRemainingDays(item.archivedAtUnixMs!, now)
                    })
            }
          }}
        />
      ))}
      {page?.hasMore ? (
        <Button
          variant="ghost"
          disabled={page.isLoading}
          onClick={() =>
            controller.loadMoreSectionConversations({ id: "archive" })
          }
        >
          {props.labels.showMoreConversations}
        </Button>
      ) : null}
    </ScrollArea>
  );
}

export function archiveRemainingDays(
  archivedAtUnixMs: number,
  nowUnixMs: number
): number {
  return Math.max(
    0,
    Math.ceil((archivedAtUnixMs + 30 * 86_400_000 - nowUnixMs) / 86_400_000)
  );
}
