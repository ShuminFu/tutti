import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";

export interface AgentTranscriptComplexity {
  totalScore: number;
  maxTurnScore: number;
  turnScores: readonly number[];
}

export interface AgentTranscriptComplexityAssessment extends AgentTranscriptComplexity {
  shouldVirtualize: boolean;
  turnCount: number;
}

/**
 * Integer-only per-row cost inputs. Keeping them integral lets a turn score be
 * summed exactly like the original single-pass scan, so the incremental path
 * cannot drift from the previous formula.
 */
export interface AgentTranscriptRowComplexity {
  charCount: number;
  codeFenceCount: number;
  tableCount: number;
  toolCallCount: number;
  thinkingBlockCount: number;
  imageCount: number;
}

const AGENT_TRANSCRIPT_COMPLEXITY_VIRTUALIZATION_SCORE = 40;
const AGENT_TRANSCRIPT_COMPLEXITY_SINGLE_TURN_SCORE = 24;
const AGENT_TRANSCRIPT_COMPLEXITY_TURN_COUNT = 30;
const AGENT_TRANSCRIPT_COMPLEXITY_MINIMUM_TURN_COUNT = 8;

const CHAR_COUNT_SCORE_DIVISOR = 1200;
const CODE_FENCE_SCORE = 4;
const TABLE_SCORE = 5;
const TOOL_CALL_SCORE = 2;
const THINKING_BLOCK_SCORE = 2;
const IMAGE_SCORE = 4;

/**
 * Transcript row VMs are immutable snapshots: the conversation projection
 * reuses the exact previous row object for every row whose render input did not
 * change (`reconcileProjectedAgentConversationVM`) and allocates a new object
 * for the rows that did. Row identity is therefore a sound cache key, and a
 * streaming delta only rescans the rows that actually changed instead of
 * re-splitting every message body in the whole transcript on each update.
 *
 * The cache is weak, so it stays bounded by the rows a mounted transcript still
 * references.
 */
const agentTranscriptRowComplexityCache = new WeakMap<
  AgentTranscriptRowVM,
  AgentTranscriptRowComplexity
>();

export function assessAgentTranscriptComplexity(
  turnGroups: ReadonlyArray<{
    rows: ReadonlyArray<{ row: AgentTranscriptRowVM }>;
  }>
): AgentTranscriptComplexityAssessment {
  const complexity = calculateAgentTranscriptComplexity(turnGroups);
  return {
    ...complexity,
    shouldVirtualize: shouldVirtualizeAgentTranscript({
      turnCount: turnGroups.length,
      complexity
    }),
    turnCount: turnGroups.length
  };
}

export function calculateAgentTranscriptComplexity(
  turnGroups: ReadonlyArray<{
    rows: ReadonlyArray<{ row: AgentTranscriptRowVM }>;
  }>
): AgentTranscriptComplexity {
  const turnScores = turnGroups.map((group) => calculateTurnScore(group.rows));
  const totalScore = turnScores.reduce((total, score) => total + score, 0);
  return {
    totalScore,
    maxTurnScore: Math.max(0, ...turnScores),
    turnScores
  };
}

export function shouldVirtualizeAgentTranscript(input: {
  turnCount: number;
  complexity: AgentTranscriptComplexity;
}): boolean {
  if (input.turnCount < AGENT_TRANSCRIPT_COMPLEXITY_MINIMUM_TURN_COUNT) {
    return false;
  }
  return (
    input.turnCount >= AGENT_TRANSCRIPT_COMPLEXITY_TURN_COUNT ||
    input.complexity.totalScore >=
      AGENT_TRANSCRIPT_COMPLEXITY_VIRTUALIZATION_SCORE ||
    input.complexity.maxTurnScore >=
      AGENT_TRANSCRIPT_COMPLEXITY_SINGLE_TURN_SCORE
  );
}

/**
 * Row cost inputs for one row, memoized on row identity. Returns the same
 * record for a row this projection already observed.
 */
export function agentTranscriptRowComplexity(
  row: AgentTranscriptRowVM
): AgentTranscriptRowComplexity {
  const cached = agentTranscriptRowComplexityCache.get(row);
  if (cached) {
    return cached;
  }
  const metrics = computeAgentTranscriptRowComplexity(row);
  agentTranscriptRowComplexityCache.set(row, metrics);
  return metrics;
}

function calculateTurnScore(
  rows: ReadonlyArray<{ row: AgentTranscriptRowVM }>
): number {
  let charCount = 0;
  let codeFenceCount = 0;
  let tableCount = 0;
  let toolCallCount = 0;
  let thinkingBlockCount = 0;
  let imageCount = 0;

  for (const { row } of rows) {
    const metrics = agentTranscriptRowComplexity(row);
    charCount += metrics.charCount;
    codeFenceCount += metrics.codeFenceCount;
    tableCount += metrics.tableCount;
    toolCallCount += metrics.toolCallCount;
    thinkingBlockCount += metrics.thinkingBlockCount;
    imageCount += metrics.imageCount;
  }

  return (
    rows.length +
    charCount / CHAR_COUNT_SCORE_DIVISOR +
    codeFenceCount * CODE_FENCE_SCORE +
    tableCount * TABLE_SCORE +
    toolCallCount * TOOL_CALL_SCORE +
    thinkingBlockCount * THINKING_BLOCK_SCORE +
    imageCount * IMAGE_SCORE
  );
}

function computeAgentTranscriptRowComplexity(
  row: AgentTranscriptRowVM
): AgentTranscriptRowComplexity {
  const metrics: AgentTranscriptRowComplexity = {
    charCount: 0,
    codeFenceCount: 0,
    tableCount: 0,
    toolCallCount: 0,
    thinkingBlockCount: 0,
    imageCount: 0
  };

  // An MCP App view is an embedded document of comparable layout cost.
  if (row.kind === "generated-image" || row.kind === "mcp-app") {
    metrics.imageCount += 1;
    return metrics;
  }
  if (row.kind === "message") {
    for (const message of row.messages) {
      metrics.charCount += message.body.length;
      metrics.codeFenceCount += countCodeFences(message.body);
      metrics.tableCount += countMarkdownTables(message.body);
      metrics.imageCount +=
        (message.images?.length ?? 0) + countMarkdownImages(message.body);
    }
    for (const thinking of row.thinking) {
      metrics.thinkingBlockCount += 1;
      metrics.charCount += thinking.body.length;
      metrics.codeFenceCount += countCodeFences(thinking.body);
    }
    return metrics;
  }
  if (row.kind === "tool-group") {
    metrics.toolCallCount += row.calls.length;
    metrics.thinkingBlockCount += row.entries.filter(
      (entry) => entry.kind === "thinking"
    ).length;
    metrics.charCount += (row.summary ?? "").length;
    return metrics;
  }
  if (row.kind === "turn-summary") {
    metrics.charCount += row.files.reduce(
      (total, file) =>
        total +
        file.label.length +
        file.path.length +
        (file.unifiedDiff?.length ?? 0) +
        (file.content?.length ?? 0),
      0
    );
  }
  return metrics;
}

function countCodeFences(value: string): number {
  let count = 0;
  for (const line of value.split("\n")) {
    const trimmed = line.trimStart();
    if (trimmed.startsWith("```") || trimmed.startsWith("~~~")) {
      count += 1;
    }
  }
  return Math.ceil(count / 2);
}

function countMarkdownTables(value: string): number {
  const lines = value.split("\n");
  let count = 0;
  for (let index = 1; index < lines.length; index += 1) {
    if (
      looksLikeTableSeparator(lines[index] ?? "") &&
      lines[index - 1]?.includes("|")
    ) {
      count += 1;
    }
  }
  return count;
}

function countMarkdownImages(value: string): number {
  let count = 0;
  let index = 0;
  while (index < value.length) {
    const imageStart = value.indexOf("![", index);
    if (imageStart < 0) {
      break;
    }
    const labelEnd = value.indexOf("]", imageStart + 2);
    const targetStart = labelEnd >= 0 ? value.indexOf("(", labelEnd) : -1;
    const targetEnd = targetStart >= 0 ? value.indexOf(")", targetStart) : -1;
    if (
      labelEnd >= 0 &&
      targetStart === labelEnd + 1 &&
      targetEnd > targetStart
    ) {
      count += 1;
      index = targetEnd + 1;
      continue;
    }
    index = imageStart + 2;
  }
  return count;
}

function looksLikeTableSeparator(line: string): boolean {
  const cells = line
    .split("|")
    .map((cell) => cell.trim())
    .filter(Boolean);
  return (
    cells.length > 0 &&
    cells.every((cell) => {
      const normalized = cell.replace(/:/gu, "");
      return (
        normalized.length >= 3 && [...normalized].every((char) => char === "-")
      );
    })
  );
}
