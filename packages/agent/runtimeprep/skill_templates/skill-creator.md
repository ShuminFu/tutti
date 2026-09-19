---
name: skill-creator
description: Create or update a reusable agent skill when the user requests a skill or repeatable workflow
---

# Skill creator

Create a focused skill only when requested. Reuse or improve an existing matching skill before adding another.

Default new skills to `<project>/.agents/skills/<skill-name>/SKILL.md`, using the task's actual project cwd. Use `~/.agents/skills/` only when the user requests a personal/global skill. Never write user skills into product installation directories or a Harness private directory. Do not move existing skills or overwrite an unrelated skill without user authorization.

Use a short lowercase kebab-case name, at most 64 characters. Include YAML frontmatter with a nonempty `name` and `description`. Quote strings containing YAML punctuation. The description explains the task and when the skill applies. Put the working procedure, boundaries and relative links in the Markdown body. Add scripts, references or assets only when useful; resolve them against the skill's own directory. Avoid placeholders and duplicated manuals.

Preserve the user's scope and authorization. A skill cannot authorize messages, publishing or destructive actions by itself. Preserve existing optional metadata and invocation policy when editing. `disable-model-invocation: true` disables automatic selection; `user-invocable: false` hides explicit menu selection. `agents/openai.yaml` with `policy.allow_implicit_invocation: false` also disables automatic selection.

After writing, read back the files, verify YAML, referenced paths and absence of unfinished placeholders. Do not execute generated scripts, tests or builds unless authorized. Report the exact path and `/skill-name` invocation. Refresh the skill menu to discover changes; the next explicit invocation re-reads the source. Start a new session to refresh automatic skill guidance for native child agents.
