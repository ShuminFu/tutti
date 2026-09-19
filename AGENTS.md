# DinTalDock / Tutti

A local-first desktop application and shared-package workspace. Repository overview: [README](README.md).

## Repository boundaries

- `services/tuttid` owns daemon product behavior; `packages/agent/host` owns provider-neutral session, turn, goal and runtime-operation lifecycle semantics.
- `apps/desktop` owns desktop composition and UI. Published workspace packages use `@tutti-os/*`.
- This file owns repository-wide rules and routing; scoped `AGENTS.md` files own area-specific guidance.

## Related repositories

The related projects occupy independent Git repositories with separate branches, versions and delivery. This repository's internal package workspace does not make the platform a single monorepo. Sibling `../` links assume adjacent checkouts.

| Repository                                  | Relationship / change owner                                                                                                                               |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [rndmaster-dev](../rndmaster-dev/AGENTS.md) | Embeds this repository's Web UI and manages `tuttid` through `cliagent-backend`; embedding, packaging and product integration belong there                |
| [dintal-claw](../dintal-claw/AGENTS.md)     | Native application host loading the RnDMaster bundle; application shell and master loading belong there                                                   |
| [engine-core](../engine-core/AGENTS.md)     | Embeddable Go runtime kernel consumed by DinTalClaw; distinct from this repository's provider-neutral application lifecycle core in `packages/agent/host` |

RnDMaster selects this repository's source via its [UPSTREAM.lock](../rndmaster-dev/third_party/tutti/UPSTREAM.lock); source and artifact responsibilities are described in [FORK](FORK.md). Local source changes do not automatically update the consumer's pin or installed artifacts. Read the target repository's `AGENTS.md` before cross-repository edits.

## Task routing

| Task                                                       | Read first                                                                                                                                          |
| ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Feature development, bug fixes, contributions and releases | [CONTRIBUTING](CONTRIBUTING.md)                                                                                                                     |
| Code style, UI components and copy                         | [STYLE](STYLE.md)                                                                                                                                   |
| Intent, Policy, State and Evidence                         | [Documentation](docs/README.md)                                                                                                                     |
| Desktop changes                                            | [Desktop guide](apps/desktop/AGENTS.md)                                                                                                             |
| Daemon changes and HTTP contracts                          | [Daemon guide](services/tuttid/AGENTS.md), [API contracts](docs/conventions/api-contracts.md)                                                       |
| Session / turn / goal / runtime-operation lifecycle        | [Host contracts](packages/agent/host/README.md), then the nearest area guide                                                                        |
| AgentGUI, composer, approvals or interactive prompts       | [AgentGUI architecture](docs/architecture/agent-gui-node.md), [area guide](packages/agent/gui/AGENTS.md)                                            |
| Session replay                                             | [Replay guide](packages/agent/session-replay/AGENTS.md)                                                                                             |
| Shared packages or UI system                               | [Packages guide](packages/AGENTS.md), [UI guide](packages/ui/AGENTS.md)                                                                             |
| Windows or platform-specific behavior                      | [Windows architecture](docs/architecture/windows-platform-support.md)                                                                               |
| Delegate independent work                                  | [DeepSeek workers](docs/conventions/deepseek-workers.md)                                                                                            |
| Checks, hooks or troubleshooting                           | [Testing](docs/conventions/testing.md), [hooks](docs/conventions/local-git-hooks.md), [troubleshooting](docs/conventions/troubleshooting/README.md) |

## Command entrypoints

- Local development: `make dev-gui`
- Changed-aware validation: `pnpm check:changed`; full validation: `pnpm check:full`
- Behavioral suites: `bash integration-tests/run.sh --list`
- Benchmarks: `bash benchmarks/run.sh --list`
- Toolchain, setup and release commands: [CONTRIBUTING](CONTRIBUTING.md)
