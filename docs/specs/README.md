# Active Specs And Plans

This directory contains dated work that is still proposed or in progress. It
is intentionally small: completed plans are removed once their durable result is
represented by current architecture, conventions, ADRs, and tests. Git
history remains available for implementation archaeology.

Specs are not the source of truth for behavior that has already landed. When a
spec completes, update the current document that owns the result and delete the
dated plan. A spec that is kept but has shrunk should be rewritten in place —
delete the migration log, per-task checklist, and progress tables rather than
leaving them below the remaining decisions.

There are currently three active specs:

- [Agent Provider Status Read/Detect Split](./2026-06-28-agent-status-read-detect-split-design.md): proposed, not implemented; the current service still probes on read behind a fingerprint/TTL cache.
- [Provider-Native Subagents](./2026-07-15-provider-native-subagents.md): accepted architecture; Codex and Claude Code child routing implemented, remaining ACP and cleanup work listed in the spec.
- [Mobile AgentGUI And DeviceLink Design](./2026-07-23-mobile-agentgui-device-link-design.md): accepted product direction; Android Personal direct-lane MVP implemented, iOS Simulator accounted acceptance and later stages remain active.
