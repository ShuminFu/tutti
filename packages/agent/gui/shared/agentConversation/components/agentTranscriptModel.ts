import { useLayoutEffect, useMemo, useRef } from "react";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import { agentTranscriptRowHasPresentationKind } from "../projection/agentTranscriptPresentation";
import { normalizeAgentTitleText } from "../../utils/agentTitleText.ts";

export interface AgentTranscriptTurnGroup {
  key: string;
  turnId: string | null;
  rows: Array<{
    row: AgentConversationVM["rows"][number];
    rowIndex: number;
  }>;
}

export interface AgentMessageLocatorItem {
  hasAgentResponse: boolean;
  key: string;
  rowKey: string;
  turnGroupIndex: number;
  rowIndex: number;
  summary: string;
}

/**
 * Locator summaries normalize every user message body into plain text, which is
 * proportional to the whole user-visible prompt text. Row VMs are immutable
 * snapshots, so the normalized summary is cached on row identity: the message
 * locator rail keeps its text work per changed row instead of per update.
 */
const agentTranscriptUserMessageSummaryCache = new WeakMap<
  AgentConversationVM["rows"][number],
  string
>();

export interface AgentParticipantTurnProjection {
  dividerRowIndexes: ReadonlySet<number>;
  turnIndexByRowIndex: ReadonlyMap<number, number>;
}

export function useEnteringTranscriptRows(
  rowKeys: string[]
): ReadonlySet<string> {
  const previousKeysRef = useRef<Set<string> | null>(null);
  const previousKeys = previousKeysRef.current;
  const enteringRowKeys = new Set<string>();

  if (previousKeys) {
    for (const key of rowKeys) {
      if (!previousKeys.has(key)) {
        enteringRowKeys.add(key);
      }
    }
  }

  useLayoutEffect(() => {
    previousKeysRef.current = new Set(rowKeys);
  }, [rowKeys]);

  return enteringRowKeys;
}

export function transcriptRowKey(
  row: AgentConversationVM["rows"][number]
): string {
  if (row.kind === "tool-group") {
    return row.expansionKey ?? row.id;
  }
  return row.id;
}

export function isAssistantParticipantContentRow(
  row: AgentConversationVM["rows"][number]
): boolean {
  switch (row.kind) {
    case "message":
      return row.speaker === "assistant";
    case "generated-image":
    case "mcp-app":
    case "processing":
    case "tool-group":
    case "turn-summary":
      return true;
    case "goal-control":
      return false;
  }
}

export function findLastMessageRowIndex(
  rows: readonly {
    row: AgentConversationVM["rows"][number];
    rowIndex: number;
  }[]
): number | null {
  for (let index = rows.length - 1; index >= 0; index -= 1) {
    const entry = rows[index];
    if (
      entry?.row.kind === "message" &&
      entry.row.speaker === "assistant" &&
      entry.row.messages.length > 0
    ) {
      return entry.rowIndex;
    }
  }
  return null;
}

/**
 * Groups the projected rows into presentation turns.
 *
 * `previousGroups` is optional and only used for reference reuse: a group whose
 * rows are still the identical row objects at the identical indexes is returned
 * as the exact previous group object. Downstream projections (turn work
 * section models, virtualizer item keys, participant headers) memoize on group
 * identity, so a streaming delta that only changes the last Turn keeps every
 * earlier group and every earlier derived model stable instead of rebuilding
 * all of them.
 */
export function buildAgentTranscriptTurnGroups(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>,
  rowKeys: ReadonlyArray<string>,
  previousGroups?: readonly AgentTranscriptTurnGroup[]
): AgentTranscriptTurnGroup[] {
  const groups: AgentTranscriptTurnGroup[] = [];
  const reusablePreviousGroupByIndex: Array<AgentTranscriptTurnGroup | null> =
    [];
  const nextTurnIdByRowIndex = buildNextTurnIdByRowIndex(rows);

  rows.forEach((row, rowIndex) => {
    const openGroup = groups.at(-1) ?? null;
    const turnId = transcriptPresentationTurnId(
      row.turnId ?? null,
      nextTurnIdByRowIndex[rowIndex] ?? null,
      openGroup?.turnId ?? null
    );
    const group =
      openGroup && openGroup.turnId === turnId
        ? openGroup
        : createTurnGroup({
            groups,
            previousGroups,
            reusablePreviousGroupByIndex,
            rowKey: rowKeys[rowIndex] ?? transcriptRowKey(row),
            turnId
          });

    group.rows.push({ row, rowIndex });

    const groupIndex = groups.length - 1;
    const previousGroup = reusablePreviousGroupByIndex[groupIndex];
    if (previousGroup) {
      const previousEntry = previousGroup.rows[group.rows.length - 1];
      if (previousEntry?.row !== row || previousEntry.rowIndex !== rowIndex) {
        reusablePreviousGroupByIndex[groupIndex] = null;
      }
    }
  });

  reusablePreviousGroupByIndex.forEach((previousGroup, groupIndex) => {
    const group = groups[groupIndex];
    if (!previousGroup || !group) {
      return;
    }
    if (previousGroup.rows.length !== group.rows.length) {
      return;
    }
    groups[groupIndex] = previousGroup;
  });

  return groups;
}

function createTurnGroup({
  groups,
  previousGroups,
  reusablePreviousGroupByIndex,
  rowKey,
  turnId
}: {
  groups: AgentTranscriptTurnGroup[];
  previousGroups: readonly AgentTranscriptTurnGroup[] | undefined;
  reusablePreviousGroupByIndex: Array<AgentTranscriptTurnGroup | null>;
  rowKey: string;
  turnId: string | null;
}): AgentTranscriptTurnGroup {
  const group: AgentTranscriptTurnGroup = {
    key: turnId ?? `orphan:${rowKey}`,
    turnId,
    rows: []
  };
  const previousGroup = previousGroups?.[groups.length];
  groups.push(group);
  reusablePreviousGroupByIndex.push(
    previousGroup && previousGroup.key === group.key ? previousGroup : null
  );
  return group;
}

/**
 * `nextTurnIdByRowIndex[index]` is the presentation turn id of the first row
 * after `index` whose own `turnId` is not null, mirroring the previous forward
 * `find` scan. It is computed in one backward pass so a transcript with many
 * turnless session-level rows no longer allocates a tail slice per row.
 */
function buildNextTurnIdByRowIndex(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>
): Array<string | null> {
  const nextTurnIdByRowIndex: Array<string | null> = Array.from(
    { length: rows.length },
    () => null
  );
  let nextTurnId: string | null = null;
  for (let rowIndex = rows.length - 1; rowIndex >= 0; rowIndex -= 1) {
    nextTurnIdByRowIndex[rowIndex] = nextTurnId;
    const rowTurnId = rows[rowIndex]?.turnId;
    if (rowTurnId !== null) {
      nextTurnId = rowTurnId ?? null;
    }
  }
  return nextTurnIdByRowIndex;
}

function transcriptPresentationTurnId(
  rowTurnId: string | null,
  nextTurnId: string | null,
  currentTurnId: string | null
): string | null {
  if (rowTurnId || !currentTurnId) {
    return rowTurnId;
  }
  // A session-level row can occur chronologically inside a live Turn. Keep it
  // in that Turn's presentation group only when the next lifecycle-owned row
  // proves the surrounding Turn is unchanged; the row itself stays turnless.
  return nextTurnId === currentTurnId ? currentTurnId : null;
}

export function buildTurnGroupIndexByRowIndex(
  turnGroups: readonly AgentTranscriptTurnGroup[]
): ReadonlyMap<number, number> {
  const rowIndexToTurnGroupIndex = new Map<number, number>();
  turnGroups.forEach((group, groupIndex) => {
    group.rows.forEach(({ rowIndex }) => {
      rowIndexToTurnGroupIndex.set(rowIndex, groupIndex);
    });
  });
  return rowIndexToTurnGroupIndex;
}

export function buildUserMessageLocatorItems(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>,
  rowKeys: ReadonlyArray<string>,
  turnGroupIndexByRowIndex: ReadonlyMap<number, number>
): AgentMessageLocatorItem[] {
  const items: AgentMessageLocatorItem[] = [];
  rows.forEach((row, rowIndex) => {
    if (row.kind !== "message" || row.speaker !== "user") {
      return;
    }
    const summary = summarizeUserMessageRow(row);
    if (!summary) {
      return;
    }
    const rowKey = rowKeys[rowIndex] ?? transcriptRowKey(row);
    items.push({
      hasAgentResponse: hasAgentResponseForTurn(rows, row, rowIndex),
      key: `user-message:${rowKey}`,
      rowKey,
      turnGroupIndex: turnGroupIndexByRowIndex.get(rowIndex) ?? rowIndex,
      rowIndex,
      summary
    });
  });
  return items;
}

export function hasAgentResponseForTurn(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>,
  userRow: AgentConversationVM["rows"][number],
  userRowIndex: number
): boolean {
  const turnId = userRow.turnId ?? null;
  for (let index = userRowIndex + 1; index < rows.length; index += 1) {
    const row = rows[index];
    if (!row) {
      continue;
    }
    if (row.kind === "generated-image" || row.kind === "mcp-app") {
      return !turnId || row.turnId === turnId;
    }
    if (row.kind !== "message") {
      continue;
    }
    if (row.speaker === "user") {
      return false;
    }
    if (turnId && row.turnId !== turnId) {
      return false;
    }
    if (row.speaker === "assistant") {
      return true;
    }
  }
  return false;
}

export function summarizeUserMessageRow(
  row: Extract<AgentConversationVM["rows"][number], { kind: "message" }>
): string {
  const cached = agentTranscriptUserMessageSummaryCache.get(row);
  if (cached !== undefined) {
    return cached;
  }
  const summary = normalizeLocatorSummary(
    row.messages.map((message) => message.copyText ?? message.body).join(" ")
  );
  agentTranscriptUserMessageSummaryCache.set(row, summary);
  return summary;
}

export function normalizeLocatorSummary(value: string): string {
  return normalizeAgentTitleText(value);
}

export function escapeCssString(value: string): string {
  return value.replace(/["\\]/g, "\\$&");
}

export function findTurnDividerRowIndexes(
  turnIndexById: ReadonlyMap<string, number>,
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>
): ReadonlySet<number> {
  const dividerRowIndexes = new Set<number>();
  const previousTurnIds = new Set<string>();

  rows.forEach((row, rowIndex) => {
    const currentTurnId = row.turnId ?? null;
    if (!currentTurnId) {
      return;
    }

    const turnIndex = turnIndexById.get(currentTurnId) ?? -1;
    const previousTurnId = rows[rowIndex - 1]?.turnId ?? null;
    if (
      rowIndex > 0 &&
      turnIndex > 0 &&
      previousTurnId &&
      previousTurnId !== currentTurnId &&
      !agentTranscriptRowHasPresentationKind(
        rows[rowIndex - 1],
        "turn-boundary"
      ) &&
      !previousTurnIds.has(currentTurnId)
    ) {
      dividerRowIndexes.add(rowIndex);
    }

    previousTurnIds.add(currentTurnId);
  });

  return dividerRowIndexes;
}

/**
 * Participant-header presentation (Agent board session detail): a presentation
 * turn starts at each user message and continues until the next user message.
 * Canonical Turn ids deliberately do not participate because recovery or
 * provider continuation may split one visible reply across multiple Turns.
 */
export function buildAgentParticipantTurnProjection(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>
): AgentParticipantTurnProjection {
  const dividerRowIndexes = new Set<number>();
  const turnIndexByRowIndex = new Map<number, number>();
  let turnIndex = 0;

  rows.forEach((row, rowIndex) => {
    if (rowIndex > 0 && row.kind === "message" && row.speaker === "user") {
      turnIndex += 1;
      dividerRowIndexes.add(rowIndex);
    }
    turnIndexByRowIndex.set(rowIndex, turnIndex);
  });

  return {
    dividerRowIndexes,
    turnIndexByRowIndex
  };
}

/**
 * Participant-header presentation: standalone tool-group rows belong to the
 * work that produced the NEXT assistant message, so they attach to that
 * message (rendered inside its block) instead of sitting after the previous
 * one. Trailing tool rows with no following assistant message stay standalone.
 */
export function attachLeadingToolRowsToFollowingMessages(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>
): AgentConversationVM["rows"] {
  const result: AgentConversationVM["rows"] = [];
  let pendingToolRows: Extract<
    AgentConversationVM["rows"][number],
    { kind: "tool-group" }
  >[] = [];
  for (const row of rows) {
    if (row.kind === "tool-group") {
      pendingToolRows.push(row);
      continue;
    }
    if (row.kind === "message" && row.speaker === "assistant") {
      if (pendingToolRows.length > 0) {
        result.push({
          ...row,
          leadingToolRows: [...(row.leadingToolRows ?? []), ...pendingToolRows]
        });
        pendingToolRows = [];
        continue;
      }
      result.push(row);
      continue;
    }
    if (pendingToolRows.length > 0) {
      result.push(...pendingToolRows);
      pendingToolRows = [];
    }
    result.push(row);
  }
  result.push(...pendingToolRows);
  return result;
}

/**
 * Read hook owning the display-row projection for the transcript view: in
 * participant-header mode tool-group rows attach to the following assistant
 * message, presentation turns start at user messages, and row keys derive from
 * the same pass. Keeping the memoization in this model module (next to
 * `useEnteringTranscriptRows`) keeps the view component within the
 * degradation-check memoization budget.
 */
export function useAgentTranscriptDisplayRows(
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>,
  participantHeadersEnabled: boolean
): {
  rows: ReadonlyArray<AgentConversationVM["rows"][number]>;
  rowKeys: string[];
  participantTurnProjection: AgentParticipantTurnProjection | null;
} {
  return useMemo(() => {
    const displayRows = participantHeadersEnabled
      ? attachLeadingToolRowsToFollowingMessages(rows)
      : rows;
    return {
      rows: displayRows,
      rowKeys: displayRows.map(transcriptRowKey),
      participantTurnProjection: participantHeadersEnabled
        ? buildAgentParticipantTurnProjection(displayRows)
        : null
    };
  }, [rows, participantHeadersEnabled]);
}
