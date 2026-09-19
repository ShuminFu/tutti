# Documentation

Use this directory for durable repository knowledge and dated engineering
records. Choose the document type by the question you are trying to answer.

## Where To Start

| Question                                         | Source                                           | Authority                                           |
| ------------------------------------------------ | ------------------------------------------------ | --------------------------------------------------- |
| How is the system built now?                     | [Architecture](./architecture/README.md)         | Current implemented structure and data flow         |
| What rule must a change follow?                  | [Conventions](./conventions/README.md)           | Stable repository requirements                      |
| Why was a consequential choice made?             | [Architecture Decision Records](./adr/README.md) | Accepted decisions and tradeoffs                    |
| What is proposed or currently being implemented? | [Specs and Plans](./specs/README.md)             | Dated working records; not current truth by default |
| How do I build and debug the Android mobile app? | [Mobile Development](./mobile/README.md)         | Practical Android and React Native onboarding       |
| What should I do in this directory?              | The nearest `AGENTS.md`                          | Scoped routing, action rules, and required checks   |

Package READMEs remain the source for a package's public usage and exports.
Code and generated contracts establish observed implementation State. Use the
applicable Policy and Intent to decide what should change; record discrepancies
with Evidence rather than silently treating existing behavior as the requirement.

## Knowledge model

Use these dimensions to assign ownership; they do not require four new directory trees.

| Dimension | Question                      | Contents and owner                                                                                                                                                                                                                     |
| --------- | ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Intent    | What do we want?              | Goals, non-goals, constraints, DoD, user journeys and acceptance criteria in the relevant requirement, spec or issue                                                                                                                   |
| Policy    | How should work be done here? | `AGENTS.md` for top-level rules and routing; `CONTRIBUTING.md` for SOP; `STYLE.md` for style; skills, runbooks, canonical commands and hooks own their execution rules                                                                 |
| State     | What is true now, and why?    | Current / next, decisions and reasons, rejected alternatives, relevant failed attempts, gotchas and blockers; architecture describes the implementation, ADRs record decisions, active work belongs in the existing spec/backlog entry |
| Evidence  | How do we prove it?           | Tests, benchmarks, E2E, reproducers, logs, screenshots, recordings and validation results; reusable source and fixtures live with tests, executions link to the issue or CI artifacts                                                  |

Intent is not proof of implementation, Policy is not proof of execution, and observed behavior does not automatically redefine acceptance criteria. Link these dimensions through the relevant feature or issue so a criterion has evidence and a decision has a reason.

## Knowledge lifecycle

- Maintain one authoritative source per concern; other documents link to it.
- Separate implemented, active, next and blocked State. Retain rejected alternatives and failed attempts when their reasons, conditions and evidence affect future choices; omit chat-by-chat transcripts.
- When work lands, merge enduring goals and acceptance criteria into the maintained specification, decisions into architecture/ADRs and remaining work into the active backlog. Remove redundant execution checklists and completion reports.
- Evidence records revision, environment, command or procedure, actual result and scope. Raw logs and media follow the linked issue/CI artifact retention policy; minimal reproducers, fixtures and selected evidence needed for future regression work may be versioned.
- Supersede or remove stale content deliberately. Do not delete business resources, release notes or useful evidence based solely on dates, filenames or the word "report".

Current architecture lives in `docs/architecture`, accepted decisions in `docs/adr`, and active Intent / next work in `docs/specs`. Reusable gotchas belong in [Troubleshooting](conventions/troubleshooting/README.md).
