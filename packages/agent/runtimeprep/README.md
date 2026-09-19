# Agent Runtime Preparation

`packages/agent/runtimeprep` is the reusable, provider-local preparation layer
for DinTalDock agent sessions. A host calls it on the same machine where the agent
provider will run. That machine is the desktop host for tutti and the managed
Linux VM for VM-backed products such as tsh.

The module owns the canonical system-prompt and skill content, capability-pack
resolution, provider home/plugin materialization, per-session environment
overlays, manifests, and cleanup. Hosts keep transport, workspace path
projection, environment trust filtering, account bootstrap, and deployment
capability selection.

## Host Wiring

```go
preparer := runtimeprep.NewDefaultPreparer(stateDir)
preparer.Profile = runtimeprep.StandardProfile()
preparer.CommandCatalog = commandCatalogAdapter
preparer.ComputerUseAvailable = computerReadinessCheck
preparer.SkillSources = []runtimeprep.SkillSource{pluginSkillSource}

prepared, err := preparer.Prepare(ctx, runtimeprep.PrepareInput{
    WorkspaceID:    workspaceID,
    AgentSessionID: sessionID,
    Provider:       "claude-code",
    Cwd:            cwd,
    BrowserUse:     true,
    ExtraSkills:    sessionSkills,
})
```

`PreparedRuntime.Env` is an overlay for the provider launch, not a complete
process environment. `Cleanup` removes only paths recorded in the session
manifest or the session-scoped runtime root.

Agent Extension hosts may pass a signed, validated
`PrepareInput.ExtensionRuntimePrep` overlay for ACP providers that need
provider-owned state projected into the session. The overlay is declarative and
provider-neutral: it may write the instructions file, create a per-session home,
copy declared opaque files from a user-home source, expose that home through one
validated environment variable, materialize DinTalDock-managed skills into declared
extension skill roots, and merge those roots into supported YAML config keys.
Runtimeprep must not add provider-ID branches for third-party extensions. YAML
config projection must use the shared parser-backed merge helpers and fail
closed on invalid or incompatible config instead of maintaining provider-specific
line parsers.

Codex preparation keeps session state isolated under the run-scoped
`CODEX_HOME`, while linking its writable `models_cache.json` to the provider
user's process-default `~/.codex/models_cache.json`. The link may initially be
dangling: the first Codex refresh creates the shared VM- or host-local cache,
and later sessions reuse it. Hosts must therefore run preparation with `HOME`
set to the provider user's stable local Home, never a session runtime directory
or a remote filesystem projection.

`TuttiAgentPreparer.ResolveAuthSource` lets a host expose one explicit absolute
credential authority into the session-scoped `TUTTI_AGENT_HOME`. When omitted,
the DinTalDock desktop keeps its existing `~/.tutti-agent/auth.json` behavior. An
explicit source may be a dangling symlink target so a host-controlled login can
materialize it atomically later; runtimeprep never follows or deletes that
target during session cleanup. If a configured resolver returns no path,
runtimeprep leaves auth unprojected and does not fall back to the VM user's
`~/.tutti-agent`. DinTalDock Agent preparation also materializes the same resolved
native Skills used by the other supported providers.

The desktop injects `MutagenAuthFileProjector` for this credential projection.
It first attempts a file symlink. When Windows denies that operation, it copies
the current stable auth into each run. If Mutagen is already available, the
projector starts an official `two-way-safe` session with the default real-time
watcher. Cleanup flushes the session and refuses to delete the run home if
Mutagen reports a conflict; otherwise it terminates the session before runtime
deletion. If Mutagen is unavailable, cleanup copies a valid changed run auth
back atomically only when the stable auth still matches the original baseline;
concurrent changes preserve both files for recovery. `.refresh.lock` is always
a symlink or hard link to the stable lock so both homes coordinate through one
OS file object. Mutagen resolution prefers `TUTTI_MUTAGEN_BIN`, then `PATH`.
Packaged Windows amd64 Desktop builds inject the verified v0.18.1 executable
bundled at build time, while unpackaged hosts use the guarded copy fallback
without a runtime download. Other bundled platforms remain to be confirmed.

When `TuttiAgentPreparer.StableSkillBundleRoot` is configured, DinTalDock-managed
Skills are content-addressed under
`<root>/v1/<sha256>/skills` instead of the run-scoped home. Preparation emits
the validated roots through the internal
`TUTTI_AGENT_EXTRA_SKILL_ROOTS_JSON` launch handoff. The app-server adapter
removes that handoff from the child environment and calls
`skills/extraRoots/set` after `initialized` and before `thread/start` or
`thread/resume`. A zero-value root preserves the legacy run-scoped layout.
Active Connector routes may contribute their verified, content-addressed
`skills/` directories to the same handoff. Runtime preparation validates that
each contributed path is absolute, exists as a directory, and is not a symlink;
duplicate roots are removed. Connector processes use separate disposable
execution snapshots, so restarting a process does not invalidate the Skill
paths held by an Agent session.
Stable bundles are immutable rebuildable cache entries and are not removed by
single-session cleanup. When `StableSystemSkillBundleRoot` is also configured,
runtimeprep passes that store as internal launch metadata. After the provider
has installed its embedded `.system` Skills during initialization, the Adapter
snapshots their actual provider-owned content into
`<root>/v1/<sha256>/.system`, atomically replaces the run-scoped directory with
a symlink to that immutable target, and only then refreshes Skills through the
extra-roots RPC. This preserves provider ownership and version updates while
making the earliest rendered Skill paths stable across Sessions. Both internal
handoffs are stripped before child launch.

## Capability Packs

A deployment capability resolves once into its policy, skills, and environment
contribution:

```go
runtimeprep.CapabilityPack{
    Name: "example",
    Resolve: func(ctx context.Context, input runtimeprep.PrepareInput) (
        runtimeprep.CapabilityContribution,
        error,
    ) {
        return runtimeprep.CapabilityContribution{
            Enabled: true,
            Skills: []runtimeprep.SkillSpec{{
                ID: "example/tool",
                Name: "example-tool",
                Files: map[string]string{"SKILL.md": "# Example\n"},
            }},
            PolicySections: []runtimeprep.PolicySection{{
                Anchor: runtimeprep.PolicyAnchorTools,
                Key: "usage",
                Body: "Use `$example-tool` for example work.",
            }},
            EnvOverlay: []string{"EXAMPLE_ENABLED=1"},
        }, nil
    },
}
```

Policy sections sort by anchor, order, and pack-qualified key. Duplicate pack
names, unknown anchors, duplicate skill IDs, and unsafe skill file paths fail
resolution rather than silently overriding content. Sections apply to both
provider runtime policy and dynamic skill bundles by default; set
`PolicySection.Delivery` when a section is valid for only one delivery path.

`StandardProfile` includes `CoreSkillsPack`, `TuttiDesktopHostPack`, browser
use, and computer use. `CoreSkillsPack` includes the provider-neutral DinTalDock
workflow skills plus `tutti-model-allocation`, whose C0-C3 policy combines the
current DinTalDock Mode effect/speed preferences with live composer model catalogs
and derives speed's bounded 1-4 parallel planning target. Allocation compares
joint Agent/model candidates across every plausible target without favoring the
planning Agent, its current model, or provider defaults.
A non-desktop deployment should compose its own profile from `CoreSkillsPack`
and deployment-owned packs instead of copying the desktop-host policy:

```go
profile := runtimeprep.DeploymentProfile{
    Name:  "managed-vm",
    Title: "Managed VM Runtime",
    Intro: "This session runs inside the managed VM.",
    HostFacts: runtimeprep.HostFacts{
        TurnResources:  runtimeprep.AgentTurnResourcesReadPath,
        WorkspaceScope: runtimeprep.AgentWorkspaceScopeRoom,
        TargetContinuation: runtimeprep.AgentTargetContinuationProfile{
            Mode: runtimeprep.AgentTargetContinuationExceptPrefixes,
            UnsupportedTargetIDPrefixes: []string{"shared-agent:"},
        },
    },
    Packs: []runtimeprep.CapabilityPack{
        runtimeprep.CoreSkillsPack(),
        vmEnvironmentPack,
    },
}
```

Every preparation requires `CommandCatalog`. Runtimeprep reads it once and
builds one immutable, agent-facing command snapshot shared by Skills, policy,
and `command-guide.md`. The host must remove trusted bindings such as room or
workspace inputs before returning the catalog. Runtimeprep filters
non-public/integration commands, preserves output metadata, and adds
wrapper-owned invocation inputs such as wait timeouts. It does not synthesize a
fallback catalog.

Skill and policy Markdown use Go `text/template`. Runtimeprep exposes
`has`, `hasAll`, `hasFamily`, `hasInput`, `inputValues`, `path`, `args`, and
`command`. `command` resolves paths and flags from the snapshot, appends
`--json` only when output metadata advertises JSON, and fails rendering for an
unknown command, input, enum value, or malformed schema.

```gotemplate
{{if has "issue-manager.issue.task.create-batch"}}
Persist tasks with
`{{command "issue-manager.issue.task.create-batch"
    (args "tasks-json" "'[{\"title\":\"<title>\"}]'")}}`.
{{else if has "issue-manager.issue.task.create"}}
Create tasks in order with
`{{command "issue-manager.issue.task.create" (args "title" "<title>")}}`.
{{end}}
```

`DeploymentProfile.HostFacts` contains only behavior that cannot be derived
from command metadata: turn-resource path semantics, workspace scoping, and
target continuation exceptions. Omitted fields use DinTalDock's local-path,
workspace-environment, and continue-all defaults. Command presence, command
paths, inputs, cancellation scope, response support, and conversation reads
belong only in the command snapshot.

## Skill Injection

Skills resolve in this order:

1. skills from the deployment profile's enabled capability packs;
2. host `SkillSource` results;
3. per-session `PrepareInput.ExtraSkills`.

Skill IDs are stable logical identities. Materialized directory slugs may gain
a suffix to avoid overwriting an existing user skill. If an injected ability
also needs policy or environment changes, inject a capability pack instead of
an isolated extra skill.

`Prepare` and `RenderSkillBundle` use the same resolver, so provider files and
the skill-bundle API cannot drift.

The canonical DinTalDock CLI skill treats the daemon and CLI as the only supported
execution control plane. Provider Agents must not inspect or modify DinTalDock's
backing SQLite databases to infer runtime state or bypass a rejected command.
When the source Agent has the DinTalDock execution snapshot capability, the skill
also renders the checkpoint/action matrix and a bounded recovery protocol:
refresh an outdated fence once, correct a documented mutation schema once, and
report a repeated rejection with its stable reason and hint.

## Product Boundaries

The module must not import `services/tuttid/*`. Product adapters translate
their command catalog and readiness types into the narrow interfaces here.
DinTalDock Account HTTP and local credential paths remain in
`services/tuttid/service/tuttiagent`. The host-neutral issue/login/verify and
cleanup ordering lives in `packages/agent/daemon/tuttiagentauth`; only provider
home/config/Skills preparation lives in this module.

Provider-specific runtime protocol and session control belong to
`packages/agent/daemon`, not this module.

## Session Runtime Instructions

Non-`claude-code` providers write the rendered tutti runtime policy to a
session-private `<runtimeRoot>/runtime-instructions.md` and export
`TUTTI_RUNTIME_INSTRUCTIONS_FILE` on `PreparedRuntime.Env`. Hosts copy that
variable onto stdio MCP server entries so an MCP `initialize` `instructions`
field can carry the policy without writing `AGENTS.md` into the user
repository. `claude-code` already injects the same policy through
`claude-system-prompt.md` and does not write `runtime-instructions.md`.

Set `TUTTI_RUNTIME_INSTRUCTIONS_CWD_WRITE=off` to skip the cwd
`AGENTS.md` / `CLAUDE.md` managed-block write from `InstructionFilePreparer`
and extension runtime instructions. Skills and other prepare branches stay
unchanged. Unset or any other value keeps the current cwd write.

## Standard local skills

`localskills` owns discovery and YAML metadata parsing for all local providers.
The composer and provider prompt projection consume the same catalog. Sources
are ordered as follows (first valid identity wins):

1. `.agents/skills/<name>/SKILL.md`, then `.agents/<name>/SKILL.md` at the cwd,
   followed by each ancestor up to and including the nearest `.git` root.
   Worktree `.git` files count as roots; outside Git, traversal ends at the
   filesystem root.
2. The same two layouts under the user's Home.
3. Provider-native skill sources, including Claude `.claude/skills`; these keep
   their native invocation semantics. Codex plugin sources come from its native
   `skills/list`, `plugin/list` and `plugin/read`, including enabled state and installed version selection.
4. Built-in skills. `skill-creator` is available without installation and writes
   new project skills to `.agents/skills`; global output requires user intent.

The standard root tiers take precedence over native sources even when the
native source is closer to cwd. Plugin identities retain their namespace.
Symlinked source files are deduplicated by physical path, while scripts and
references continue to resolve against the selected skill directory. No user
skills are copied, migrated, or deleted. Built-in runtime materializations and
Claude native reference entries are session-owned delivery artifacts, not
additional user skill roots.

Metadata requires nonempty string `name` and `description`. Names permit
letters, digits, dots, underscores and hyphens (first character alphanumeric,
maximum 64 characters). Invalid YAML, duplicate keys, unreadable files and name
conflicts produce source-qualified diagnostics. `user-invocable: false` hides
explicit selection; `disable-model-invocation: true` and
`agents/openai.yaml`'s `policy.allow_implicit_invocation: false` suppress automatic
selection. Custom Agent skill selections also apply to native plugin sources.

A leading `/name` or `$name`, or an exact structured skill selection, resolves
against the current catalog before provider validation and dispatch. The
provider receives source-reading instructions; canonical/display prompt data
stays intact. Unknown native commands, inline prose and code examples are not
rewritten. Deleted, renamed or mismatched standard selections fail visibly.

Each preparation emits `TUTTI_LOCAL_SKILLS_FILE` (a session catalog) and
`TUTTI_LOCAL_SKILLS_SELECTION_FILE` (the selection and native source snapshot).
Both values are file paths so large catalogs do not overflow Windows environment
limits. These are internal launch metadata, not user configuration overrides.
The session catalog refreshes before dispatch. DeepSeek projects it into the
registry shared by child agents; native plugin sources refresh at new/resumed
session preparation. Claude receives native reference skills plus the common
system-prompt index. Codex receives the index in its runtime instructions,
including plugin skill references in isolated/fast-start sessions, without
copying personal credentials or enabling plugin MCP services. Agents forward
selected skill sources to native child tasks. New sessions refresh automatic
native-child guidance; menu refresh and explicit invocation reread local files.
