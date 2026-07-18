# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Authoritative instructions: AGENTS.md hierarchy

This repository maintains agent instructions in `AGENTS.md` files. Read the root
`AGENTS.md` plus the closest area file before editing; area files win over the root:

- `apps/desktop/*` -> `apps/desktop/AGENTS.md`
- `services/tuttid/*` -> `services/tuttid/AGENTS.md`
- `packages/agent/gui/*` -> `packages/agent/gui/AGENTS.md`
- `packages/ui/*` -> `packages/ui/AGENTS.md`
- `packages/*` -> `packages/AGENTS.md`

Route by module name too, not only path: any task mentioning AgentGUI / agent
conversation / composer / approvals requires reading
`docs/architecture/agent-gui-node.md` then `packages/agent/gui/AGENTS.md` first.
Any task touching agent session/turn/goal/runtime-operation lifecycle requires
`packages/agent/host/README.md` and the root `AGENTS.md` "Agent Host Boundary"
section first.

Durable docs live in `docs/conventions/` and `docs/architecture/` (start at
their `README.md` files). After any code change, check whether those docs need
updating in the same change.

## Toolchain

Node >= 24, pnpm 10.11.0 (pinned), Go 1.24, pinned golangci-lint
(`pnpm install:golangci-lint`). One-time: `pnpm install`, then `pnpm setup:dev`
to verify prerequisites.

## Commands

Development:

- `make dev-gui` — desktop app with prerequisite checks and a prebuilt `tuttid`
- `pnpm dev:desktop` — raw Electron/Vite loop (needs a warmed daemon binary)
- `pnpm dev:cli` / `pnpm dev:web` — CLI and web dev entrypoints

Validation (see "Validation selection" below before running anything):

- `pnpm check:changed -- --dry-run` — inspect the changed-aware plan
- `pnpm check:changed` — the single final gate for normal work (selects tests, typecheck, lint, boundary checks)
- `pnpm check:changed -- --failed-only` — rerun only failed lanes
- `pnpm check:full` — full validation; only for broad impact or release risk

Focused iteration tools:

- `pnpm typecheck` — incremental native TypeScript (`tsgo`)
- `pnpm lint:ts` (Oxlint, warnings are errors) / `pnpm lint:go` (golangci-lint)
- `pnpm format` — Oxfmt for TS/JS, Prettier for JSON/MD/YAML/CSS/HTML
- `pnpm test:ts` — all TS workspace package tests plus tool tests
- `pnpm test:go` — generates builtin app assets, then the blocking Go workspace test set
- `pnpm test:go:agent-daemon` — focused lane for `packages/agent/daemon`
- `pnpm test:tools` — repository tool tests only (`tools/scripts/*.test.mjs`)

Running a single test:

- One package: `pnpm --filter <package-name> test` (e.g. `pnpm --filter @tutti-os/desktop test`)
- Vitest packages (e.g. `packages/agent/gui`, `packages/ui/system`): append a file filter, `pnpm --filter @tutti-os/agent-gui test -- path/to/file.test.ts`; other packages use `node --test`
- Go: `go test -run TestName ./path/...` inside the module directory. Run `pnpm generate:builtin-apps` first when building/testing `services/tuttid` directly — it embeds generated builtin app assets.

Test/validation logs: root runners write full lane output under
`.tmp/test-runs/*` (with `latest.json` manifests) and `.tmp/check-full-runs`.
Read the manifest first to find the failed lane instead of scanning logs.

Generated code (never hand-edit generated output):

- Daemon HTTP contract: edit `services/tuttid/api/openapi/tuttid.v1.yaml` first, then `pnpm generate:api` (check: `pnpm check:api-generated`)
- Business event protocol: `pnpm generate:event-protocol` from schemas in `packages/events/protocol`
- Runtime defaults: edit `config/tutti.defaults.json`, then `pnpm generate:defaults`
- Workbench Go contract: `pnpm sync:workbench-go-contract`

## Validation selection

Defined in `docs/conventions/testing.md#validation-selection` — a scope policy,
not a cumulative checklist:

- UI-presentation-only changes (no logic/behavior change): run no checks at all. This exception takes precedence.
- Everything else: inspect `pnpm check:changed -- --dry-run`, iterate with focused commands on failing surfaces, then run one final `pnpm check:changed`. Do not re-run its selected lanes as separate preflights.
- Add standalone checks only when the dry-run plan omits a needed capability (e.g. `pnpm --filter @tutti-os/desktop build` for desktop runtime behavior, `pnpm check:i18n` for user-visible copy, `pnpm check:defaults-generated` for defaults).

Husky hooks: `pre-commit` runs lint-staged plus staged boundary checks;
`pre-push` runs `pnpm check:changed -- --push-ready`.

## Architecture

Tutti is a local-first desktop product: an Electron app supervising a
long-running local Go daemon. Hybrid pnpm workspace (`pnpm-workspace.yaml`) +
Go workspace (`go.work`).

- `services/tuttid` — the primary business core: business rules, domain workflows, durable local state (SQLite at `~/.tutti/tuttid.db`, dev: `~/.tutti-dev/tuttid.db`), HTTP/query API, adapters. If a feature needs domain decisions or state transitions, it lands here.
- `apps/desktop` — Electron shell only: `src/main` (lifecycle, transport, IPC, daemon supervision), `src/preload` (typed bridge), `src/renderer/src/app/windows/*` (window shells) and `src/renderer/src/features/*` (feature modules; `services/internal/**` is private to its feature), `src/shared` (desktop-local contracts + i18n). Must not become a second business core — call `tuttid` or IPC adapters instead of re-implementing workflows.
- `apps/cli` — terminal entrypoint for the daemon CLI capability protocol; command metadata and execution stay in `tuttid`.
- `packages/*` — real shared boundaries only, grouped by responsibility: `agent/*` (lifecycle host, canonical store, activity engine, GUI), `clients/*`, `events/*` (schema-first event protocol), `browser/*`, `ui/*` (ui-system tokens/primitives, i18n runtime, react-hooks), `workbench/*`, `workspace/*`, `configs/*`. Code stays local (desktop TS in `apps/desktop`, daemon Go in `services/tuttid`) until a real multi-consumer boundary exists. Never create `shared`/`common`/`utils` packages.
- `tools/scripts/*` — repo automation: check/generate/release scripts wired into the root `package.json`.

### Agent Host boundary (most important invariant)

`packages/agent/host` is the single owner of agent lifecycle semantics: when a
session/turn/goal/runtime-operation is created, may be sent, is terminal, and
how it recovers. `services/tuttid/service/agent` and other consumers are
adapters that translate HTTP/query/composer/transport concerns and delegate
through `ApplicationHost()`. New lifecycle semantics need a scenario in
`packages/agent/host/conformance` first; missing Host capability gets added to
Host, never reimplemented in an adapter. `pnpm check:agent-host-boundary`
enforces this ratchet.

## Hard rules (from root AGENTS.md)

- Published workspace packages use the `@tutti-os/*` scope.
- Reuse `@tutti-os/ui-system` components, semantic color tokens, typography, and spacing before writing bespoke UI or raw color values; extend the UI system rather than working around it.
- All user-visible copy goes through the relevant i18n layer — no hardcoded UI text, labels, empty states, or user-facing errors. Chinese UI copy must not end with a Chinese full stop (。).
- Business-code files stay at or below 800 lines; decompose before growing past it.
- Daemon HTTP contract changes start in `services/tuttid/api/openapi/tuttid.v1.yaml`.
- Conventional Commits with DCO sign-off: `git commit -s -m "type(scope): subject"`.
- `README.md`/`CONTRIBUTING.md` changes must update the `zh-CN` and `zh-TW` variants in the same PR (English is source of truth); `docs/` is English-only.
- For merge/rebase conflicts, never resolve with `--ours`/`--theirs` unless explicitly asked; inspect both branch intents.
