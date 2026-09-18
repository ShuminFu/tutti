// Pure decision logic for ExternalAgentSessionImportWizard.tsx, split out so
// it can be unit tested without mounting the dialog (this app's test runner
// is plain node:test over *.test.ts and has no React rendering harness).

import type {
  ExternalAgentImportArchiveKind,
  ExternalAgentImportScanRequest,
  ExternalAgentImportScanResponse,
  ExternalAgentImportSession,
  WorkspaceAgentProvider
} from "@tutti-os/client-tuttid-ts";

export type ExternalImportProjectGroup = {
  path: string;
  label: string;
  providers: WorkspaceAgentProvider[];
  sessions: ExternalAgentImportSession[];
};

export type ExternalImportScanIdentity =
  | {
      kind: "archive";
      archivePath: string;
      archiveKind: ExternalAgentImportArchiveKind;
    }
  | {
      kind: "local";
      providers: WorkspaceAgentProvider[];
    };

export type ExternalImportScanSource = ExternalImportScanIdentity & {
  days: number;
};

export type ExternalImportScanSnapshot = {
  response: ExternalAgentImportScanResponse;
  identity: ExternalImportScanIdentity;
  days: number;
  cutoffUnixMs: number | null;
  scannedAtUnixMs: number | null;
  complete: boolean;
};

export type ExternalImportScanState = {
  snapshot: ExternalImportScanSnapshot | null;
};

export type ExternalImportScanStateAction =
  | { type: "scan-started" }
  | { type: "scan-failed" }
  | { type: "source-changed" }
  | {
      type: "scan-succeeded";
      response: ExternalAgentImportScanResponse;
      source: ExternalImportScanSource;
      mergeProviders?: boolean;
    };

export type ExternalImportScanDecision =
  | { type: "reuse"; response: ExternalAgentImportScanResponse }
  | {
      type: "request";
      source: ExternalImportScanSource;
      mergeProviders: boolean;
    };

export function isExternalImportArchiveMode(
  archivePath: string | null | undefined
): boolean {
  return Boolean(archivePath?.trim());
}

export function externalImportScanSource({
  archiveKind,
  archivePath,
  days,
  providers
}: {
  archiveKind: ExternalAgentImportArchiveKind;
  archivePath: string | null;
  days: number;
  providers: WorkspaceAgentProvider[];
}): ExternalImportScanSource {
  const normalizedArchivePath = archivePath?.trim();
  if (normalizedArchivePath) {
    return {
      kind: "archive",
      archivePath: normalizedArchivePath,
      archiveKind,
      days
    };
  }
  return {
    kind: "local",
    days,
    providers: [...new Set(providers)]
  };
}

export function externalImportScanIdentity(
  source: ExternalImportScanSource
): ExternalImportScanIdentity {
  return source.kind === "archive"
    ? {
        kind: "archive",
        archivePath: source.archivePath,
        archiveKind: source.archiveKind
      }
    : { kind: "local", providers: [...source.providers] };
}

export function externalImportScanRequest(
  source: ExternalImportScanSource
): ExternalAgentImportScanRequest {
  return source.kind === "archive"
    ? {
        archivePath: source.archivePath,
        archiveKind: source.archiveKind,
        days: source.days
      }
    : { days: source.days, providers: source.providers };
}

export function externalImportScanStateReducer(
  current: ExternalImportScanState | null,
  action: ExternalImportScanStateAction
): ExternalImportScanState | null {
  const snapshot = current?.snapshot ?? null;
  switch (action.type) {
    case "scan-started":
    case "scan-failed":
      return snapshot ? { snapshot } : current;
    case "source-changed":
      return null;
    case "scan-succeeded": {
      const nextSnapshot = snapshotFromScanResponse(
        action.response,
        action.source
      );
      if (action.mergeProviders && snapshot) {
        return {
          snapshot: mergeLocalScanSnapshots(snapshot, nextSnapshot)
        };
      }
      return { snapshot: nextSnapshot };
    }
    default:
      return current;
  }
}

export function externalImportScanDecision(
  state: ExternalImportScanState | null,
  source: ExternalImportScanSource,
  nowMs: number,
  forceRefresh = false
): ExternalImportScanDecision {
  if (forceRefresh) {
    return { type: "request", source, mergeProviders: false };
  }
  const projected = externalImportUsableScan(state, source, nowMs);
  if (projected) {
    return { type: "reuse", response: projected };
  }
  const missing = missingLocalProviders(state?.snapshot ?? null, source);
  if (missing) {
    return {
      type: "request",
      source: { ...source, providers: missing },
      mergeProviders: true
    };
  }
  return { type: "request", source, mergeProviders: false };
}

export function externalImportUsableScan(
  state: ExternalImportScanState | null,
  source: ExternalImportScanSource,
  nowMs = Date.now()
): ExternalAgentImportScanResponse | null {
  const snapshot = state?.snapshot;
  if (!snapshot || !snapshot.complete) {
    return null;
  }
  if (!identitiesCompatible(snapshot.identity, externalImportScanIdentity(source))) {
    return null;
  }
  if (!snapshotCoversCutoff(snapshot, source.days, nowMs)) {
    return null;
  }
  return projectScanSnapshot(snapshot, source, nowMs);
}

export function shouldRetainExternalImportDeselections(
  action: ExternalImportScanStateAction
): boolean {
  return action.type !== "source-changed";
}

export function pruneExternalImportDeselections(
  current: Set<string>,
  knownIds: Iterable<string>
): Set<string> {
  const live = new Set(knownIds);
  const next = new Set<string>();
  for (const id of current) {
    if (live.has(id)) {
      next.add(id);
    }
  }
  return next;
}

function snapshotFromScanResponse(
  response: ExternalAgentImportScanResponse,
  source: ExternalImportScanSource
): ExternalImportScanSnapshot {
  return {
    response,
    identity: externalImportScanIdentity(source),
    days: source.days,
    cutoffUnixMs:
      typeof response.cutoffUnixMs === "number" ? response.cutoffUnixMs : null,
    scannedAtUnixMs:
      typeof response.scannedAtUnixMs === "number"
        ? response.scannedAtUnixMs
        : null,
    complete: response.complete !== false
  };
}

function identitiesCompatible(
  snapshot: ExternalImportScanIdentity,
  target: ExternalImportScanIdentity
): boolean {
  if (snapshot.kind !== target.kind) {
    return false;
  }
  if (snapshot.kind === "archive" && target.kind === "archive") {
    return (
      snapshot.archivePath === target.archivePath &&
      snapshot.archiveKind === target.archiveKind
    );
  }
  return snapshot.kind === "local" && target.kind === "local";
}

function snapshotCoversCutoff(
  snapshot: ExternalImportScanSnapshot,
  days: number,
  nowMs: number
): boolean {
  if (snapshot.cutoffUnixMs == null) {
    return snapshot.days === days;
  }
  const targetCutoff = cutoffFromDays(days, nowMs);
  if (snapshot.cutoffUnixMs === 0) {
    return true;
  }
  if (targetCutoff === 0) {
    return false;
  }
  return snapshot.cutoffUnixMs <= targetCutoff;
}

function missingLocalProviders(
  snapshot: ExternalImportScanSnapshot | null,
  source: ExternalImportScanSource
): WorkspaceAgentProvider[] | null {
  if (!snapshot || source.kind !== "local" || snapshot.identity.kind !== "local") {
    return null;
  }
  if (!snapshotCoversCutoff(snapshot, source.days, Date.now())) {
    return null;
  }
  const covered = new Set(snapshot.identity.providers);
  const missing = source.providers.filter((provider) => !covered.has(provider));
  return missing.length > 0 && missing.length < source.providers.length
    ? missing
    : null;
}

function cutoffFromDays(days: number, nowMs: number): number {
  if (days < 0) {
    return 0;
  }
  const windowDays = days === 0 ? 30 : days;
  return nowMs - windowDays * 24 * 60 * 60 * 1000;
}

function projectScanSnapshot(
  snapshot: ExternalImportScanSnapshot,
  source: ExternalImportScanSource,
  nowMs: number
): ExternalAgentImportScanResponse {
  const cutoff = cutoffFromDays(source.days, nowMs);
  const allowedProviders =
    source.kind === "local" ? new Set(source.providers) : null;
  const sessions = snapshot.response.sessions.filter((session) => {
    if (allowedProviders && !allowedProviders.has(session.provider)) {
      return false;
    }
    if (cutoff === 0) {
      return true;
    }
    const updated = session.lastUpdatedAtUnixMs ?? 0;
    return updated === 0 || updated >= cutoff;
  });
  const projects = externalImportGroupsFromScan(
    { ...snapshot.response, sessions },
    (path) => path
  ).map((group) => ({
    path: group.path,
    label: group.label,
    providers: group.providers,
    sessionCount: group.sessions.length,
    messageCount: group.sessions.reduce(
      (total, session) => total + session.messageCount,
      0
    ),
    lastUpdatedAtUnixMs: group.sessions.reduce(
      (latest, session) => Math.max(latest, session.lastUpdatedAtUnixMs ?? 0),
      0
    )
  }));
  const providers = snapshot.response.providers
    .filter((provider) =>
      allowedProviders ? allowedProviders.has(provider.provider) : true
    )
    .map((provider) => {
      const providerSessions = sessions.filter(
        (session) => session.provider === provider.provider
      );
      return {
        ...provider,
        sessionCount: providerSessions.length,
        messageCount: providerSessions.reduce(
          (total, session) => total + session.messageCount,
          0
        )
      };
    });
  return {
    ...snapshot.response,
    sessions,
    projects,
    providers,
    scannedSessions: sessions.length,
    scannedMessages: sessions.reduce(
      (total, session) => total + session.messageCount,
      0
    )
  };
}

function mergeLocalScanSnapshots(
  current: ExternalImportScanSnapshot,
  incoming: ExternalImportScanSnapshot
): ExternalImportScanSnapshot {
  if (current.identity.kind !== "local" || incoming.identity.kind !== "local") {
    return incoming;
  }
  const sessionsById = new Map(
    current.response.sessions.map((session) => [session.id, session])
  );
  for (const session of incoming.response.sessions) {
    sessionsById.set(session.id, session);
  }
  const providers = [
    ...current.identity.providers,
    ...incoming.identity.providers.filter(
      (provider) => !current.identity.providers.includes(provider)
    )
  ];
  const mergedResponse: ExternalAgentImportScanResponse = {
    ...incoming.response,
    sessions: [...sessionsById.values()],
    errors: [...current.response.errors, ...incoming.response.errors],
    providers: [
      ...current.response.providers.filter(
        (provider) =>
          !incoming.response.providers.some(
            (next) => next.provider === provider.provider
          )
      ),
      ...incoming.response.providers
    ]
  };
  const mergedSource: ExternalImportScanSource = {
    kind: "local",
    providers,
    days:
      current.days < 0 || incoming.days < 0
        ? -1
        : Math.max(current.days, incoming.days)
  };
  const projectedIdentity: ExternalImportScanIdentity = {
    kind: "local",
    providers
  };
  const cutoffUnixMs =
    current.cutoffUnixMs === 0 || incoming.cutoffUnixMs === 0
      ? 0
      : current.cutoffUnixMs == null || incoming.cutoffUnixMs == null
        ? null
        : Math.min(current.cutoffUnixMs, incoming.cutoffUnixMs);
  const snapshot: ExternalImportScanSnapshot = {
    response: mergedResponse,
    identity: projectedIdentity,
    days: mergedSource.days,
    cutoffUnixMs,
    scannedAtUnixMs: incoming.scannedAtUnixMs ?? current.scannedAtUnixMs,
    complete: current.complete && incoming.complete
  };
  snapshot.response = projectScanSnapshot(snapshot, mergedSource, Date.now());
  return snapshot;
}

export function externalImportRequestSource(
  archivePath: string | null,
  archiveKind: ExternalAgentImportArchiveKind | null,
  registerProjects: boolean
): {
  archivePath?: string;
  archiveKind?: ExternalAgentImportArchiveKind;
  registerUserProjects: boolean;
} {
  const normalizedArchivePath = archivePath?.trim();
  if (!normalizedArchivePath) {
    return { registerUserProjects: registerProjects };
  }
  return {
    archivePath: normalizedArchivePath,
    ...(archiveKind ? { archiveKind } : {}),
    registerUserProjects: false
  };
}

/**
 * The import wizard is "busy" whenever a scan or import request is in
 * flight. Dismissing the dialog does not cancel that request (there is no
 * AbortController wired up; the backend keeps running to completion either
 * way), but a disappearing dialog reads as "the import stopped". All three
 * ways a Radix Dialog can be dismissed - Escape, click-outside, and the
 * built-in "X" close button - must agree on this single condition so none
 * of them can slip through while busy.
 */
export function isExternalImportWizardBusy({
  importing,
  loading
}: {
  importing: boolean;
  loading: boolean;
}): boolean {
  return loading || importing;
}

/**
 * Decides whether a Dialog onOpenChange(nextOpen) call should be allowed
 * through. Only closes (nextOpen === false) are ever blocked, and only
 * while busy; opening, and any change once idle, is always allowed.
 */
export function shouldAllowExternalImportDialogOpenChange({
  importing,
  loading,
  nextOpen
}: {
  importing: boolean;
  loading: boolean;
  nextOpen: boolean;
}): boolean {
  if (nextOpen) {
    return true;
  }
  return !isExternalImportWizardBusy({ importing, loading });
}

export function externalImportGroupsFromScan(
  scan: ExternalAgentImportScanResponse | null,
  projectLabelFallback: (path: string) => string,
  labelOverride?: string
): ExternalImportProjectGroup[] {
  if (!scan) {
    return [];
  }
  const labelByPath = new Map(
    scan.projects.map((project) => [project.path, project.label])
  );
  const groupByPath = new Map<string, ExternalImportProjectGroup>();
  for (const session of scan.sessions) {
    const path = session.projectPath;
    let group = groupByPath.get(path);
    if (!group) {
      group = {
        path,
        label:
          labelOverride ?? labelByPath.get(path) ?? projectLabelFallback(path),
        providers: [],
        sessions: []
      };
      groupByPath.set(path, group);
    }
    if (!group.providers.includes(session.provider)) {
      group.providers.push(session.provider);
    }
    group.sessions.push(session);
  }
  return [...groupByPath.values()].sort(
    (left, right) =>
      externalImportGroupRecency(right) - externalImportGroupRecency(left)
  );
}

function externalImportGroupRecency(group: ExternalImportProjectGroup): number {
  return group.sessions.reduce(
    (latest, session) => Math.max(latest, session.lastUpdatedAtUnixMs ?? 0),
    0
  );
}

export function filterExternalImportGroups(
  groups: ExternalImportProjectGroup[],
  search: string
): ExternalImportProjectGroup[] {
  const query = search.trim().toLowerCase();
  if (!query) {
    return groups;
  }
  const result: ExternalImportProjectGroup[] = [];
  for (const group of groups) {
    const labelMatch =
      group.label.toLowerCase().includes(query) ||
      group.path.toLowerCase().includes(query);
    const sessions = labelMatch
      ? group.sessions
      : group.sessions.filter((session) =>
          session.title.toLowerCase().includes(query)
        );
    if (sessions.length > 0) {
      result.push({ ...group, sessions });
    }
  }
  return result;
}

export function externalImportSelectionProjects(
  sessions: ExternalAgentImportSession[],
  deselectedSessionIds: Set<string>
): {
  path: string;
  providers?: WorkspaceAgentProvider[];
  sessionIds?: string[];
}[] {
  const byPath = new Map<
    string,
    {
      path: string;
      providers: Set<WorkspaceAgentProvider>;
      sessionIds: string[];
    }
  >();
  for (const session of sessions) {
    if (deselectedSessionIds.has(session.id)) {
      continue;
    }
    let entry = byPath.get(session.projectPath);
    if (!entry) {
      entry = {
        path: session.projectPath,
        providers: new Set(),
        sessionIds: []
      };
      byPath.set(session.projectPath, entry);
    }
    entry.providers.add(session.provider);
    entry.sessionIds.push(session.id);
  }
  return [...byPath.values()].map((entry) => ({
    path: entry.path,
    providers: [...entry.providers],
    sessionIds: entry.sessionIds
  }));
}
