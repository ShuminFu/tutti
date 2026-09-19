# Mobile AgentGUI And DeviceLink Design

Status: accepted product direction; Personal direct-lane MVP implemented in
Android, iOS Simulator shell closed — remaining work is accounted-Simulator
acceptance of login/pairing/DeviceLink, then the stage list below.

This is the active product and protocol record. Development onboarding, toolchain,
and day-to-day debugging live in [Mobile Development](../mobile/README.md);
transport build details live in `packages/device-link/README.md`. Earlier stage
logs, per-task lists, and progress tables were removed — Git history keeps them.

## Goal And Non-Goals

A phone signed into the same account as the user's Tutti Personal desktop pairs
explicitly, prefers P2P and falls back to the relay tunnel, then reads and
operates Agent sessions through the existing generated clients, Agent Activity,
and Agent Host. The main view is the conversation; the session list opens as a
side drawer.

The MVP is explicitly **not** remote SSH/terminal/desktop, arbitrary localhost
port forwarding, iOS device distribution, background push, offline queues or
content caching, file/image/voice input, a full copy of the desktop right rail,
or a full Web/React Native component-source unification. Only the bounded,
observable, removable ALPN seam needed for the TSH switchover is kept.

## Principles

- **One fact, one owner.** Session/Turn/Interaction/Goal/runtime-operation
  lifecycle stays in `packages/agent/host`; the phone uses the same workspace
  `AgentSessionEngine` semantics with no `MobileSession`/`RemoteSession` or
  second state machine. `tsh-server` owns account, device, pairing, discovery,
  and short-lived rendezvous only. DeviceLink owns the authenticated link,
  product-neutral dual-path race, and bidirectional streams; it never
  interprets Agent DTOs, and product adapters inject direct/relay identity and
  path selection. React Native owns navigation, presentation, and local UI state.
- **Reuse the protocol, do not mirror pages.** The phone transports no desktop
  HTML and copies no DOM. It renders Native components over the existing Agent
  OpenAPI DTOs, events, and generated clients. It may expose only part of the
  protocol's settings, but must not add a simplified create API or hard-code
  Agent, model, permission, or run-mode values; later Composer exposure only
  widens the UI.
- **Events lower latency, snapshots calibrate.** The event stream is not an
  independent truth: workspace entry, conversation open, event gaps, reconnect,
  and foreground recovery all reconcile against an authoritative snapshot, and
  the phone persists no resumable Agent state machine.
- **Close the loop before unifying components.** Behavior, projections, state
  semantics, and the visual language are shared from day one; Web and Native
  component sources, icons, and token origins converge later.

## Ownership And Boundaries

Data/DI structure, service scopes, live-lane and polling rules, and the
background grace window are current implementation facts recorded in
[Mobile Development §1.1](../mobile/README.md#11-数据层与作用域不要重造). Do not
redefine them from this document.

- DeviceLink is a release-enabled shared core that keeps the production ICE,
  QUIC, certificate-pinning, candidate-filtering, and privacy behavior; the
  product adapter owns room/device identity, rendezvous, relay credentials, and
  path policy.
- The Agent live lane is a workspace-scoped DeviceLink stream owned by a
  dedicated service; `packages/agent/daemon/liveprotocol` owns its subscriber
  semantics and it is not DeviceLink semantics.
- Control-plane storage, logs, and metrics carry identity and classification
  only. Agent payloads stay on the data plane: neither the control plane nor the
  relay persists session content.

## Contract Rules That Must Not Drift

- Same-account constraint: pairing only between devices of one account, with a
  durable device identity and explicit unpair/revocation path.
- `protocolEpoch` mismatch fails fast with an explicit error state. Development
  releases coordinate instead of maintaining a compatibility matrix.
- Concurrent Desktop and Mobile operation must not produce duplicate submits or
  divergent state: stable submit/request identity plus read-authoritative-state
  before retry after an ambiguous delivery.
- Unrecognized payloads degrade to an explicit unsupported fallback rather than
  breaking the conversation.

## Remaining Work

- Accounted iOS Simulator acceptance: real login, manual pairing code,
  DeviceLink, and an Agent conversation loop (shell, Native module adapter, Go
  XCFramework boundary, and shared TypeScript platform cleanup are done).
- Stages after the Android Personal MVP: device/pairing control plane
  completion, relay and Agent API transport hardening, Mobile app shell,
  AgentGUI MVP breadth, stability work, and iOS parity.
- Acceptance target: direct-first with relay-usable, not a P2P success-rate
  gate; offline device, revoked pairing, and epoch mismatch each need a
  distinguishable error; reconnect after background/foreground calibrates from a
  snapshot without duplicates or permanent gaps.

## Later Evolution

Complete Composer settings, attachments/images/references and richer
Markdown/code, promote the necessary semantic tokens into the platform-neutral
`@tutti-os/ui-system` entry, progressively share conversation component sources
while keeping `.web.tsx` / `.native.tsx` primitives, enable the TSH/VM
workspace lane in the same app core, then iOS device signing and distribution.
Protocol compatibility windows, push, and background capability are designed
only after user scale and release cadence stabilize.
