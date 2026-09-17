# Peer review — send preflight / dontAsk / visible send failure

Mode: local uncommitted working tree on `dintal-dock`.
Reviewed: 29 modified files (no staged hunks). Untracked `notes/2026-09-17-prompt-preflight-dontask-send-visible.md` ignored.

## Summary

T1 is correct: ACP `ValidatePromptContent` now uses the same declared-composer `imageInput` source as `standardACPSession.promptImage` init, so an idle-reaped Claude/OpenCode session no longer fails image preflight before Resume/Start. T2a is correct: Claude Code drops `dontAsk` from the composer profile, stored values remap to `default` at composer options, session normalize, ACP mapper, SDK sidecar, and interactive option IDs; extension `dontAsk` is untouched. `failOnSetModeError` stays true on Claude ACP. T3 does make current-conversation send failures visible, but two defects undercut the new error taxonomy: `applyACPMode` stamps every `failOnSetModeError` failure (timeouts, plan-mode `set_mode`, transport) as `ErrPermissionModeUnavailable`, and AgentGUI’s dedicated mapper never sees protocol `reason` so it often prepends a generic send-failed line onto already-localized desktop copy.

## Issues

### Issue 1 -- Severity: bug

- File: packages/agent/daemon/runtime/standard_acp_settings.go:70
- Description: Keeping `failOnSetModeError` true is the T2b decision and is not the problem. Wrapping **every** `applyACPMode` failure as `ErrPermissionModeUnavailable` with inner `%v` is. Start/Resume (`standard_acp_session.go:183` and `:384`) and plan-mode `ApplySessionSettings` all call this path. Extension adapters also set `failOnSetModeError` when they have a `PlanModeRuntimeID` (`provider_descriptors.go:253`). A 10s `session/set_mode` timeout is `*acpCallTimeoutError` unwrapping to `context.DeadlineExceeded`; `%v` drops that chain, so Host/`Classify` now emit `invalid_request` / `agent.permission_mode_unavailable` instead of a timeout/retryable failure. Plan-mode confirmation failures get the same sentinel and the GUI copy “choose another permission mode.” The empty-mapped fail-closed in `ApplyPermissionMode` (`standard_acp_permissions.go:44-46`) is correctly scoped; this wrap is not. Claude ACP still has `failOnSetModeError: true` as required.
- Suggestion: Abort on `set_mode` failure without reclassifying unrelated errors. Use two `%w` (or `errors.Join`) so timeout/cancel remain inspectable, and only wrap true “mode not available / unknown mode” replies as `ErrPermissionModeUnavailable`. Do not map plan-mode `set_mode` failures onto permission-tier copy.
- Status: open

### Issue 2 -- Severity: suggestion

- File: packages/agent/gui/agent-gui/agentGuiNode/controller/useAgentGUISubmitInteractionActions.ts:402
- Description: Desktop send errors are wrapped as `{ code: "invalid_request", reason: "agent.permission_mode_unavailable" | "agent.prompt_image_unsupported", message: <desktop i18n> }` (`desktopErrors.ts:70-97`). The engine stores `code` as `errorCode` and `reason` as `errorReason` (`effectExecutor.ts:387-411`), then pending-submit settlement **drops** `errorReason` (`pendingSubmit.reducer.ts:128-135`). `onPromptSendFailed` only forwards `submit.errorCode` and `submit.errorMessage` into `formatPromptSendFailed` (`agentGuiController.errors.ts:35-49`). Dedicated matchers therefore look for `agent.prompt_image_unsupported` on `invalid_request` plus already-localized copy. English permission copy happens to contain “permission mode” / “not available” and remaps; English image copy (“does not support image input yet”) does not match `prompt image input is unsupported`, so the user sees “The message could not be sent. This agent does not support image input yet.” zh-CN permission/image both miss the English matchers and get a doubled prefix. Tests feed `errorCode: "agent.prompt_image_unsupported"` or the raw Go string, which is not the production shape. T3’s “must be visible” still holds; the dedicated AgentGUI strings and the new desktop `errors.invalid_request.agent.*` keys fight each other. `Classify` dual-matching `agentservice` and `agentruntime` sentinels is the right Host passthrough fix (`apierrors.go:533-538`) because SendInput goes through `hostadapter.mapRuntimeError`, which does not remap these sentinels.
- Suggestion: Pass protocol `reason` through pending-submit records (or read `errorReason` in settlement) and match on that before prefixing. If the message is already the desktop reason-specific string, show it once. Add a test with `{ code: "invalid_request", reason: "agent.prompt_image_unsupported" }` as `wrapLocalizedTuttidErrorIfSpecific` produces.
- Status: open

### Issue 3 -- Severity: suggestion

- File: packages/agent/gui/agent-gui/agentGuiNode/controller/agentGuiController.errors.ts:71
- Description: `isPermissionModeUnavailableFailure` treats any text containing `acp mode confirmation failed`, or both `permission mode` and `not available`, as a permission-tier mismatch. The new sentinel string is `agent session permission mode is not available`, so **any** raw wrapped `applyACPMode` error matches even without `Classify`. Combined with Issue 1, a plan-mode or timed-out `set_mode` is shown as “Choose another permission mode and send again.” Image mapping reuses `promptImagesUnsupported` (“current model”), which is the paste-block string, not the capability-level desktop copy.
- Suggestion: Match structured `reason` only. Drop the `acp mode confirmation failed` substring. Keep image copy at capability level (`errors.invalid_request.agent.prompt_image_unsupported`) rather than “current model.”
- Status: open

### Issue 4 -- Severity: suggestion

- File: packages/agent/daemon/runtime/standard_acp_permissions.go:44
- Description: Unknown-mode fail-closed is only on `ApplyPermissionMode` when the mapper returns empty. Start/Resume still call `applyACPMode(startupModeID)`; empty `modeID` skips (`standard_acp_settings.go:19-27`). A Claude session that reached Start with `PermissionModeID=mysteryMode` would not abort. Controller `normalizePermissionModeIDWithFallback` remaps unknown Claude ids to `default` on reconstruct/settings, so this is hard to hit after load. The new adapter test only covers `ApplyPermissionMode` after a successful Start (`acp_provider_claude_test.go:159`), not Start/Resume itself. Stored `dontAsk` is remapped by the ACP mapper before the empty check, so T2a remap correctly wins over T2b fail-closed for that id. Extensions that still declare `dontAsk` keep it.
- Suggestion: If mismatch must abort at send/resume, fail in `applyACPMode`/`startupModeID` when a non-empty requested id maps to empty, not only in `ApplyPermissionMode`. Cover Start/Resume in the adapter test.
- Status: open

### Issue 5 -- Severity: nit

- File: packages/agent/daemon/providerregistry/claude_code.go:113
- Description: The added comment is history of WHAT was removed and how ACP abort used to happen. The following pre-existing Chinese block is about `bypassPermissions` / 完全放行, so the hole now reads as if that prose still describes `dontAsk`. `claude_sdk_interactive.go:296` says “claude-agent-acp never exposed it” on the SDK option mapper, which is the wrong runtime. `promptImageCapability`’s comment is useful WHY and can stay.
- Suggestion: One line at the missing `dontAsk` slot: retired composer id; remap to default. Move ACP abort WHY next to `failOnSetModeError` / the mapper. Fix the SDK comment to name the sidecar path.
- Status: open

## What looks right (not issues)

- T1: `promptImageCapability` (`standard_acp_adapter.go:285-294`) matches `promptImage` init (`standardACPProviderPromptImageSupported(provider, initializeResult)`): declared `imageInput` first, live initialize only if a session exists. Host still validates before Resume/Start (`lifecycle.go:569`), which is why declared-capability is the only preflight source that can work after idle reap. OpenCode/Claude tests allow images with no live session; Hermes undeclared still rejects.
- T2a: Claude composer profile no longer offers `dontAsk`. `GetComposerOptions`, `normalizePermissionModeIDWithFallback`, ACP `permissionModeID`, SDK `permissionModeValue` / `claudeSDKPermissionMode` / `claudeCodeModeFromID` all remap stored `dontAsk` → `default`. Extension fixtures in `service_update_settings_test.go` / `manager_test.go` still use `dontAsk`.
- T2b / keep fail-closed: Claude ACP `failOnSetModeError` remains true (`acp_provider_claude.go:49`, asserted in test). No degrade-to-WARN.
- T3: Failed current-conversation submits restore the draft only when empty and call `onPromptSendFailed`. Same current-conversation gate as goal-control. Switching conversations already `clearDetailError`. Architecture now says Native Mobile also “renders the failure as conversation detail error”; this diff only wires AgentGUI — leftover product/doc gap, not a silent AgentGUI regression.
