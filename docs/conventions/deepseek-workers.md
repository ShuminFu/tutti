# DeepSeek worker delegation

GPT is encouraged to coordinate bounded DeepSeek Flash workers and retain
responsibility for integration and final review. Use a separate, detailed prompt
for each worker and avoid concurrent writes to the same files.

## RnDMaster embedding

The RnDMaster CLI selects the Harness using its provider argument:

```sh
"$RNDMASTER_CLI" session create deepseek-harness \
  --data-dir "$RNDMASTER_DATA_DIR" -C "$WORKER_CWD" \
  --model 'deepseek-flash[1m]' --thinking high \
  --prompt-file "$WORKER_PROMPT_FILE" --request-id "$WORKER_REQUEST_ID" \
  --wait idle --timeout 30m --json
```

Resolve the installed CLI and running instance first. RnDMaster CLI defaults to
Demo when no instance selector is provided; the executable location does not
change that default. For validation, explicitly set the Test `--data-dir` or
`RNDMASTER_ENV=test`, and keep running Demo processes and tasks intact. Explicit
endpoint/data-dir options take precedence over the environment selection. All variables above are
caller-owned values; cwd and file paths must be absolute. Prepare and retain the
request UUID, prompt, stdout, stderr and an immutable result file per attempt.
The prompt must require the worker to write a sibling temporary result then
rename it into place, reporting findings, files changed, checks actually run and
blockers. Inspect that result; an idle session alone does not mean review passed.
Use the same instance and request UUID to recover uncertain submissions.
Preserve the result before cancelling an owned Tutti task through
`"$RNDMASTER_CLI" session cancel <task-id> --data-dir "$RNDMASTER_DATA_DIR" --json`. The RnDMaster `close` command handles
PTY sessions only.

## Native Tutti CLI

Discover exact target IDs and supported settings through `tutti agent list
--json` and `tutti agent start --help`. Inspect model and reasoning choices with
`tutti agent composer-options --agent-id extension:deepseek-harness --cwd
"$WORKER_CWD" --json`. For a configured DeepSeek extension:

```sh
tutti agent start --agent-id extension:deepseek-harness \
  --cwd "$WORKER_CWD" --model 'deepseek-flash[1m]' \
  --reasoning-effort high --prompt "$WORKER_PROMPT" --json
```

Save the returned session identity and use `tutti agent wait --session-id
<session-id> --json` for its next stop point. Use `tutti agent get` to recover
conversation context and inspect the result artifact before accepting the work.
Do not open windows unless requested by the user.

## Model and context contract

`deepseek-flash` is the Dintal gateway's current Flash ID; resolve availability
from the selected installation rather than assuming public API naming matches.
The Harness is `acp:deepseek-harness`, separate from the model ID. High reasoning
maps to the existing `reasoningEffort` setting. The existing `[1m]` convention
requests 1,000,000 tokens: the host strips the marker from outbound model IDs and
runtime preparation writes the context-window value into the Harness config.
It does not grant a model additional server-side capacity.

Keep these settings explicit for each worker; do not change unrelated user
session defaults. Give peer reviewers an exact diff or file scope, read-only
repository access, and an explicit result contract. Delegation does not grant
permission to run checks, publish changes, or recursively launch more workers.
