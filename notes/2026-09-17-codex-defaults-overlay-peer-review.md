# Peer review: Codex composer-defaults vs host overlay

Scope: uncommitted working-tree change on branch `dintal-dock`
(`services/tuttid/service/agent/composer_defaults_validation.go`,
`services/tuttid/service/agent/model_validation.go`,
`services/tuttid/service/agent/composer_defaults_host_overlay_test.go`).
Read-only review: no source file was modified, nothing was committed or pushed.

## 1. Verdict

**PASS** — no P0, no P1.

The reported defect is genuinely fixed, the fix is pinned by tests that can only
pass through the new branch, session create and the extension path are
untouched, the fail-open fallback is preserved, and the reason-code contract is
preserved.

Caveat for the reader, so the verdict is not over-read: under a deliberately
strict reading of the P1 definition ("advertised picker value still refused"),
one **pre-existing** mismatch survives in a different deployment shape — a
workspace-scoped model plan, where the picker is workspace-scoped but the
defaults patch contract is not. It is not introduced, worsened, or reachable via
this diff's surface, and fixing it needs a protocol change, so it is filed as
residual risk **R1** rather than P1. If the team's bar is "any reachable
advertised-but-refused case is P1", then reclassify R1 and the verdict becomes
FAIL; I do not think that is the right call for this change.

## 2. P0 (wrong persist/reject in production path, data loss, crash, security)

None found.

Checked specifically:

- Rejection never persists a refused field and never publishes a "changed"
  event: `services/tuttid/service/preferences/service.go:177-186` persists only
  `validation.Applied` and returns before the store call when every field was
  refused.
- The new rejection path returns `AgentComposerDefaultsRejectedField{}` with the
  stable `invalid_value` code via `composerDefaultsRejection`
  (`composer_defaults_validation.go:209-218`, `236-262`), i.e. a per-field
  verdict, not an error that could collapse the whole patch.
- No new crash surface: the branch reads `options.ModelConfig.Options` only, and
  both helpers are nil/empty-safe (`model_validation.go:69-93`, `99-112`).
- Create's model-validation predicate is semantically unchanged. Old
  `model_validation.go` loop (`candidate == model || (base != model && candidate
== base)` with `model` already trimmed by `clampComposerModelForProvider`) is
  logically identical to `composerModelIDInCatalog`
  (`marked && candidate == base`), including the `[1m]`, `[1M]` and
  `x[1m][1m]` corner cases verified against
  `packages/agent/daemon/contextwindow`.

## 3. P1 (advertised value still refused / unadvertised value persisted / extension or no-overlay Codex path broken)

None found.

- Advertised overlay value now accepted: proven by
  `TestValidateAgentComposerDefaultsPatchAcceptsHostOverlayOneMillionModel`.
  Acceptance can only come from the new branch there, because the stub catalog
  (`recordingModelCatalog`, `model_plan_binding_test.go:100-115`) returns only
  `gpt-native`, so the fallback `validateComposerModelForCreate` path would
  reject `deepseek-flash` and `deepseek-flash[1m]`.
- No-overlay Codex native path intact:
  `TestValidateAgentComposerDefaultsPatchStillValidatesNativeCatalogWithoutHostOverlay`
  asserts `gpt-native` is applied (no overlay) and `deepseek-flash[1m]` is
  rejected with `invalid_value`.
- Extension path unchanged: the `agent_extension` early return
  (`composer_defaults_validation.go:198-204`) is untouched, and the existing
  extension tests still pass (`composer_defaults_validation_test.go`).
- Session create untouched: `resolveCreateSessionModelForPlanOrProvider`
  (`model_plan_binding.go:538-580`) and `validateModelAgainstPlan`
  (`model_plan_binding.go:402-416`) are not in the diff.

## 4. P2 / nits

1. **Comment over-claims picker parity.** The new comment
   (`composer_defaults_validation.go:205-208`) says Composer Options "already
   applied the host overlay ... — the same catalog the picker shows". True for
   the host-endpoint overlay, not for a workspace-bound model plan, because the
   defaults call passes no workspace (see R1). Suggest: "the same catalog the
   picker shows for this target" and a one-line note that workspace-scoped plan
   models are resolved by create, not here.
2. **Documentation impact not captured (AGENTS.md self-evolution rule).**
   `docs/architecture/agent-gui-node.md:233-250` documents the per-field
   defaults negotiation and extension-owned catalogs but never states which
   catalog a model default is validated against. One sentence there ("a
   non-extension model default is validated against the target's advertised
   composer model options, i.e. the same list the picker renders, including 1M
   variants") would have prevented this bug from being re-introduced. The
   symptom → cause chain is also a good candidate line for
   `docs/conventions/troubleshooting/agent-provider-setup.md`.
3. **Missing negative test for the `Requested` exclusion at validator level.**
   `TestAdvertisedComposerModelValuesSkipRequestedBootstrapEcho` only unit-tests
   the helper. A cheap end-to-end addition: with the native catalog stub and no
   overlay, patch a model that is _not_ in the catalog (it will appear in
   `ModelConfig.Options` only as the `Requested` echo, because
   `modelcatalog.SelectModel` synthesizes `{ID: requested}` with `Found=true`;
   see `packages/agent/daemon/modelcatalog/selection.go:28-37` and
   `composer_model_catalog.go:56-66`) and assert `invalid_value`. Optionally also
   assert the rejection's `AvailableModels` equal the overlay ids, which pins
   that the _branch_ (not the fallback catalog) decided.
4. **Branch ignores `ModelSelection`/`Configurable`, the fallback does not.**
   The new branch compares the raw patch value against the advertised list,
   whereas `validateComposerModelForCreate` first applies
   `clampComposerModelForProvider` (`composer_options.go:682-687`), which blanks
   the model for a provider without a model picker. No built-in provider is
   affected today (only `nexight`/`openclaw` lack `ModelSelection`, and they
   have neither a `ModelCatalog` nor a `ModelPlanProtocol`, so their advertised
   list is empty and the branch is skipped), but a future picker-less provider
   with a catalog or host overlay would start rejecting a value the old path
   accepted. Cheap hardening: skip the branch when
   `composerProfileFor(provider).ModelSelection` is false.
5. **The two helpers are near-duplicates.**
   `advertisedComposerModelValues` (`model_validation.go:69-93`) and
   `composerConfigOptionModelValues` (`model_validation.go:205-226`) differ only
   by the `Requested` filter, and the fallback path still uses the unfiltered
   one. Consequence worth a comment: the documented provenance rule ("create
   validation runs against the raw catalog only", `api/generated/types.gen.go:5119`)
   is now enforced on the new branch but not on the fallback; it happens to stay
   harmless only because `normalizeLiveComposerModelOptions`
   (`composer_live_model_discovery.go:710-746`) drops the flag before live
   options are cached. Either unify the helpers or note why the fallback may
   keep treating a `Requested` row as evidence.
6. **claude-code is a silent no-op for the new branch — worth one comment.**
   `ModelOptionsAuthoritative` (`providerregistry/claude_code.go:150`) empties
   `ModelConfig` (`composer_options.go:353`, `760-763`), so without a host/plan
   overlay the branch never fires and the old live-discovery path still decides;
   with an overlay the plan overlay rewrites `ModelConfig` afterwards
   (`composer_options.go:493`), so the branch does fire. The picker for
   claude-code reads the runtime-context projection, which is only equal to
   `ModelConfig.Options` because the overlay writes both — a fragile equality
   that deserves a sentence in the comment.
7. **Test-fixture duplication.** `installCodexHostOverlayWithDeepSeekFlash`
   (`composer_defaults_host_overlay_test.go:140-168`) re-implements
   `setHostModelEndpointContractWithModelContext`
   (`model_plan_binding_test.go:21-59`). Extending the existing helper with a
   `models` parameter would keep one host-endpoint fixture in the package.
   Also consider asserting the stub catalog was not consulted (a stub is already
   installed; a call counter would make question 7 airtight).

## 5. Answers to the review questions

1. **Match the picker, including `[1m]` without a `modelContext` row?**
   Yes, and the new validation is never _stricter_ than the picker: every
   advertised value is accepted, and `X[1m]`/`X[1M]` is accepted whenever `X` is
   advertised even if no 1M row is painted. That superset is deliberate and
   correct here: it is exactly session create's rule
   (`modelplanbiz.ModelsContain` strips the marker,
   `biz/modelplan/model.go:352-365`; used by `validateModelAgainstPlan`), so
   defaults and create now agree. The picker's 1M rows still come only from the
   host table (`composer_context_window_options.go:51-85`), so nothing new is
   _offered_; only the accepted set is aligned with create.
2. **Are `Requested` bootstrap echoes excluded so a typed-but-unadvertised model
   cannot self-validate?** Yes for this branch. `advertisedComposerModelValues`
   skips `option.Requested`, and an unknown requested id always lands in the
   option list as a `Requested` row (`modelcatalog.SelectModel` synthesizes it;
   `composer_model_catalog.go:56-66` appends it), which matches the documented
   provenance contract in `api/generated/types.gen.go:5119`. Two honest
   qualifications: (a) exclusion is only one of the routes by which the patched
   value can appear in a picker list — a host/plan overlay legitimately contains
   it, and claude-code's static fallback appends the requested model _unmarked_
   (`staticClaudeComposerModelOptions`, `composer_live_model_discovery.go:477-499`),
   so for claude-code the branch effectively accepts anything (as the old
   fail-open path did; no regression); (b) the fallback path still treats a
   `Requested` row as evidence (nit 5).
3. **Rejecting native-only ids when the overlay owns the picker — correct vs
   session create?** Correct. Create resolves the same overlay first
   (`resolveCreateSessionModelForPlanOrProvider` → `validateModelAgainstPlan`,
   which rejects `gpt-native` against the host model list), so defaults and
   create agree instead of contradicting each other. `gpt-native` is also not
   painted by the picker in that configuration, so no advertised value is lost.
4. **Fallback still fail-open?** Yes. `len(advertised) > 0` gates the new branch,
   so an empty/all-`Requested` list falls through to
   `validateComposerModelForCreate`, which returns `nil` when the catalog is
   unavailable or empty (`model_validation.go:49-55`). Verified by the
   no-overlay test and by reading the catalog-failure path
   (`composer_model_catalog.go:43-51` → `catalogProjectionOK=false` → only the
   `Requested` echo in `ModelConfig`, hence an empty advertised list).
5. **Reason code still `invalid_value` for a truly unknown model?** Yes. The
   branch builds `InvalidModelError`, and `composerDefaultsReasonCodeForError`
   has no matching substring for it, so it maps to
   `AgentComposerDefaultsReasonInvalidValue` (`composer_defaults_validation.go:250-262`).
   Pinned by
   `TestValidateAgentComposerDefaultsPatchRejectsNativeOnlyModelWhenHostOverlayOwnsPicker`.
   The message is diagnostic-only, and `AvailableModels` comes from the picker
   list, so the 512-rune diagnostic bound in eventstream still applies.
6. **Any missed provider (OpenCode host overlay) still validating against the CLI
   catalog while the menu is the overlay?** No. The branch is provider-generic
   and derives from the same `GetComposerOptions` result the picker renders, and
   the overlay is applied last for every non-extension provider
   (`composer_options.go:493-494`; `applyResolvedModelPlanComposerOverlay`
   rewrites `ModelConfig` wholesale). OpenCode specifically: `ModelSelection`,
   `ModelCatalog=OpenCodeCLI`, `ModelPlanProtocol=openai`, provider-prefixed
   addressing (`providerregistry/opencode.go:87-90`, `Runtime.Endpoint`), so a
   host endpoint for `opencode` yields prefixed overlay values in
   `ModelConfig.Options` and the patch value (also prefixed, from the picker)
   matches. Same for `tutti-agent`. claude-code is the only built-in whose
   `ModelConfig` is intentionally empty, and its old live-discovery path is the
   correct source there.
7. **Do the tests pin the regression (native catalog stub + host overlay)?** Yes.
   `newCodexNativeCatalogService` installs the `gpt-native` stub,
   `installCodexHostOverlayWithDeepSeekFlash` installs the host overlay (with and
   without `modelContext`), and the two together mean acceptance of
   `deepseek-flash[1m]` is only possible through the advertised-options branch;
   test 4 removes the overlay and re-asserts rejection, which proves the overlay
   is what changed the verdict. The remaining gap is only the direct negative
   case in nit 3.

## 6. What I actually read and what I actually ran

Read (not exhaustive): the full diff and both changed files;
`composer_defaults_host_overlay_test.go`; `composer_options.go`
(`GetComposerOptions` pipeline, `clampComposerModelForProvider`,
`composerModelConfig`, `composerSelectedModelOptions`);
`composer_model_catalog.go`; `composer_context_window_options.go`;
`composer_model_options_test.go`; `composer_defaults_validation_test.go`;
`composer_defaults_partial_success_test.go`; `model_plan_binding.go` and its
tests; `service_create.go` (`resolveCreateSessionLaunch`);
`service_create_runtime_test.go`; `composer_live_model_discovery.go`
(live merge, static Claude fallback, normalization); `composer_runtime_model_options.go`;
`codex_model_catalog.go`; `preferences/service.go`
(`PatchAgentComposerDefaultsForTarget`); `eventstream/agent_composer_defaults_payloads.go`;
`packages/agent/runtimeprep/host_model_endpoint.go` + `model_endpoint.go`;
`packages/agent/daemon/contextwindow`; `packages/agent/daemon/modelcatalog`
(`selection.go`, `composer_projection.go`); `biz/modelplan/model.go`
(`ModelsContain`); `providerregistry` descriptors for codex/opencode/claude-code/
cursor/tutti-agent/nexight/openclaw; the generated `Requested` contract comment;
desktop/GUI call sites that send the defaults patch
(`DesktopAgentGUIWorkbenchBody.tsx:610-630`,
`agentComposerDefaultsPatchCoordinator.ts`, `useAgentGUIComposerSettingsActions.ts:400-460`,
`540-560`); docs `agent-gui-node.md` and `docs/conventions/troubleshooting/*`.

Ran (Go 1.26.3, macOS; no lint/build/pack):

- `go test ./services/tuttid/service/agent/ -count=1 -run
'TestValidateAgentComposerDefaultsPatch|TestAdvertisedComposerModelValues|TestComposerModelIDInCatalog'`
  → **ok** (0.67s, run twice). This covers all six new tests.
- `go test ./services/tuttid/service/agent/ -count=1 -run
'Composer|Model|Default|Plan|Overlay|Extension|Catalog'` → 25.5s, 9 failures.
  All 9 trace to the ambient `TUTTI_HOST_MODEL_ENDPOINTS_FILE` in this shell
  (`/Users/jrrc/.dintalclaw-demo/.../host-model-endpoints.json`, i.e. the live
  demo overlay leaking into tests that assume "no host endpoint"), on code paths
  this diff does not touch (`resolveModelPlan` → `hostDefaultModelResolution`,
  `validateModelAgainstPlan`, live-model `modelCatalogSource`). Re-running the
  same four representative failures with
  `env -u TUTTI_HOST_MODEL_ENDPOINTS_FILE -u TUTTI_HOST_MODEL_ENDPOINTS` →
  **ok** (0.4s). The failures are pre-existing test-isolation gaps, not this
  change.
- `env -u TUTTI_HOST_MODEL_ENDPOINTS_FILE -u TUTTI_HOST_MODEL_ENDPOINTS go test
./services/tuttid/service/agent/ -count=1 -run
'TestApplyModelPlanComposerOverlayReplacesModelOptions|TestGetComposerOptionsBoundPlanSkipsProviderNativeModelCatalog|TestValidateModelAgainstPlanRejectsUnknownModel|TestHostDefaultModelEndpoint'`
  → **ok** (0.48s).
- Full package suite (`go test ./services/tuttid/service/agent/ -count=1`) was
  **not completed**: the harness killed it at 600s. A broad filtered run with the
  ambient env cleared aborted on a pre-existing
  `fatal error: concurrent map writes` in the _test fake_
  (`service_test_runtime_test.go:546`, `fakeRuntime.Close`) triggered by a
  background hidden-discovery goroutine in the extension-composer tests —
  unrelated to this diff (neither the fake nor that cleanup path is touched).

## 7. Residual risks

- **R1 (most important, pre-existing): the defaults patch carries no workspace.**
  `ValidateAgentComposerDefaultsPatch` builds `GetComposerOptions` with an empty
  `WorkspaceID` (`composer_defaults_validation.go:87-99`), and the eventstream
  payload has only `agentTargetId` + `patch`
  (`agent_composer_defaults_payloads.go:11-15`), so `resolveModelPlan` returns
  the host-default fallback before it can consult a binding
  (`model_plan_binding.go:330-336`). In a workspace with a bound model plan the
  _picker_ is workspace-scoped and shows the plan's models
  (`TestGetComposerOptionsBoundPlanSkipsProviderNativeModelCatalog`; the plan's
  models come from the plan's own endpoint or an explicit list,
  `service/modelplan/detection.go:110-160`), while the defaults validator sees
  the host-overlay or provider-native list. If the plan lists a model outside
  that list, saving it as a default is still refused with `invalid_value` — the
  same symptom as the reported bug, in a different deployment. It is not
  introduced or worsened here (the old code was equally plan-blind, and further
  from the picker), it cannot be fixed without adding workspace scope to the
  patch contract, and in the DinTalDock demo the plan models come from the same
  gateway as the host overlay, so they usually overlap. Filed as a risk, not a
  P1, for those reasons.
- **R2: `X[1m]` is accepted even when the host published no `modelContext` row.**
  Deliberate and tested (parity with create), but note the consequences the
  window-table gate was written to prevent
  (`composer_context_window_options.go:20-38`): a stored `X[1m]` default makes
  create set `ContextWindow=1M` and hand the CLI a 1M request for a model the
  host never vouched for. That is create's pre-existing behaviour, not something
  this diff changes — but the diff does make such defaults storable for the
  first time, so it is worth a conscious decision.
- **R3: a valid host endpoint with no `models` list blanks the picker.**
  `ModelEndpointConfig.valid()` only requires `baseURL`+`apiKey`
  (`runtimeprep/model_endpoint.go:71-73`), and the overlay replaces
  `ModelConfig` wholesale, so a model-less endpoint yields an empty advertised
  list → fallback to the CLI catalog (fail-open). Create skips model validation
  in exactly that configuration (`model_plan_binding.go:566-570`), so the two
  stay consistent, but a gateway-unknown model can be stored as a default.
- **R4: the branch's authority is `ModelConfig.Options`, not the runtime-context
  projection.** Equal today because every writer fills both; if a future writer
  fills only `RuntimeContext.configOptions` (as claude-code's picker path
  already reads), the branch would silently fall back to the CLI catalog again.
- **R5: package test isolation.** Several tests in this package fail whenever a
  host-overlay env var is present in the shell (see section 6); the new tests are
  hermetic (`t.Setenv` with an isolated temp file, and the no-overlay test clears
  both env vars explicitly), but neighbours are not, so `pnpm test:go` results
  depend on the developer's shell.
