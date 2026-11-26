## Context

The Grill, writing `plan.md` and the plan check each have a topic of `bfc prime` that prints a skill with numbered steps ("Done when …") followed by what the agent works from: `bfc prime grill` (`grill-intent`), `bfc prime plan` (`write-plan`), `bfc prime check` (`plan-check`). Implementing a Task has no such topic; the agent gets only the few lines of `bfc prime` on `bfc claim`, `bfc handoff` and recording a deviation with `bfc ask`. This plan adds `bfc prime implement [id] [T-n]` and the skill `implement-task`, the per-Task procedure that bfc-cvxz will later run without a person.

Decisions from the Grill (the author's answers):

- **Task choice**: when the user names no Task, the agent takes the Task it already holds; otherwise the first one `bfc ready` would offer (first open, then first Paused, as `bfc claim` without a Task). It says which Task it took. It does not ask.
- **Commits**: one or more commits per Task, all before the Handoff. The agent pushes the code branch, so the next agent in another clone can continue from the Handoff, then pushes the plans ref with `bfc push`.
- **Pull request**: after the last open Task is completed, the agent says so and offers one pull request per plan; only on the user's yes does it run `gh pr create`, with a line `Plan: <Short ID>` in the body so `bfc check find` and `bfc prime check` find the plan. It never merges and never takes a Gate (`done`).
- **Checks**: the agent reads the repository's checks (build, tests, lint) from `CLAUDE.md` / `CONTRIBUTING.md`. A `checks:` field in `.beforecode/config.yaml` is left to bfc-cvxz.
- **Handoff**: the skill prescribes fixed headings in the Handoff text; `bfc handoff` does not enforce them and the Handoff data format does not change. The covered ACs are recorded only in that text.

Settled rules this plan must respect: Gates are for people; `bfc start` only on the user's request (ADR 0009); the PreToolUse hook allows edits only while the Current plan is `approved` or `in-progress` (ADR 0011); skills ship in the binary and the repository's copy wins; `bfc prime` and its topics read only the local plans ref, never fail over plans and cap their output at 8 kB.

## Approach

Follow `bfc prime check` (`cmd/bfc/primecheck.go`) as the model.

1. **Skill.** Add `.beforecode/skills/implement-task/SKILL.md`, embedded through `templates.go` like the other skills. Numbered steps, each with "Done when": (1) make sure there is a Current plan, `approved` or `in-progress`, and run `bfc start <id>` only when the user asked to work on that plan; (2) `bfc pull`; (3) claim the Task (`bfc claim <id> T-n --json`; when the Claim fails, take the next one and say so); (4) read the Task, the acceptance criteria it touches, `plan.md` and every earlier Handoff of the Task, and name the ACs the Task covers; (5) write the failing tests first; (6) implement until they pass; (7) run the repository's checks from `CLAUDE.md` / `CONTRIBUTING.md`; (8) commit, then push the code branch; (9) hand off with the fixed headings; (10) `bfc push`; (11) stop: one Task per turn unless the user asked for more; after the last open Task, offer the pull request. Separate sections for "When a check fails or a decision is missing" (a Handoff without `--done` that says why, plus `bfc ask --to <author> --ref plan.md#AC-n` when a person must decide; never `--done` with failing checks) and "Departing from plan.md" (the existing `bfc ask` rule).
   The Handoff note starts with a one-line summary, because `bfc ready` and `bfc show` show only the first line, followed by the headings `## Changed`, `## Acceptance criteria`, `## Tests`, `## Left`, `## Questions`.

2. **Topic.** Register `implement` in `primeTopics`. `bfc prime` accepts up to three arguments so that `bfc prime implement <id> <T-n>` fits; `bfc prime implement T-n` means the Current plan (parsed like `claimArgs`). `runPrimeImplement` uses `gatherTopic` with the skill, picks the Task (given, else the one the git author holds, else the one `bfc claim` without a Task would take) and renders: the head (`writeTopicHead`), the plan with its state, the chosen Task with its text, state, holder and why it was chosen, the commands for this plan (`bfc claim`, `bfc handoff`, `bfc ask --to <author>`), the acceptance criteria with their text, `plan.md`, and the Task's Handoffs in full, oldest first. Notes instead of a Task: plan not `approved`/`in-progress`, every Task completed (then the pull-request step), unknown `T-n` (lists the Tasks), plan without Tasks. Over 8 kB, `plan.md` is cut at a line with a note to run `bfc show <id>`; the skill, the Task, the ACs and the Handoffs of the Task stay.

3. **Pointers.** `bfc prime`'s command list and the `beforecode` skill (built-in `internal/agent/claude/SKILL.md` and this repository's `.claude/skills/beforecode/SKILL.md`) name `bfc prime implement` for implementing a plan or a Task. `docs/design.md` describes the topic next to the others.

## Affected areas

- `.beforecode/skills/implement-task/SKILL.md` (new); `templates.go` if the embed pattern needs it.
- `cmd/bfc/prime.go` (topic map, argument count, command list); `cmd/bfc/primeimplement.go` and `cmd/bfc/primeimplement_test.go` (new); `cmd/bfc/skills_test.go`; fixture data under `cmd/bfc/testdata/` if used.
- Task selection reuses the logic of `cmd/bfc/claim.go`, and Handoff reading that of `cmd/bfc/handoff.go` (`planHandoffs`, `writeHandoff`); move shared code rather than copy it.
- `internal/agent/claude/SKILL.md`, `.claude/skills/beforecode/SKILL.md`.
- `docs/design.md` (Prime section).

## Out of scope

- Starting implementation automatically after approval (bfc-cvxz), and a `checks:` field in `.beforecode/config.yaml`.
- Several agents on one plan at once, worktrees, the phase 4 Build screen.
- Harnesses other than Claude Code.
- Any change to the Handoff data format or to `bfc handoff`, `bfc claim`, `bfc ready`; no structured "covered ACs" field.
- `bfc prime implement` claims nothing and writes nothing; the agent runs `bfc claim` itself.
- Opening or merging pull requests from `bfc`; `bfc` never calls the GitHub API.

## Tasks

- T-1: Write the skill `.beforecode/skills/implement-task/SKILL.md` with the steps, failure rules, Handoff headings and pull-request step from the Approach; embed it and extend `skills_test.go` so the embedded copy matches and the required content is present.
- T-2: Add the `bfc prime implement [id] [T-n]` topic: argument parsing, Task selection, rendering, notes and the 8 kB cap, tested against fixtures with Tasks, Claims and Handoffs.
- T-3: Point to the topic: `bfc prime`'s command list, the built-in and this repository's `beforecode` skill, and `docs/design.md`.

## Acceptance criteria

- AC-1: `.beforecode/skills/implement-task/SKILL.md` is embedded in `bfc`; `bfc prime implement` prints it first, without front matter, and a repository copy in `.beforecode/skills/implement-task/` is printed instead when present.
- AC-2: `bfc prime implement <id> T-n` on an `approved` or `in-progress` plan prints the plan's Short ID, title and state, Task T-n with its text, state and holder, every acceptance criterion with its text, `plan.md`, and every Handoff of T-n in full, oldest first, and no Handoffs of other Tasks.
- AC-3: Without `T-n`, the printed Task is the one the git author of the clone holds; when they hold none, the one `bfc claim` without a Task would take (first open, then first Paused); the output says which rule chose it.
- AC-4: Without an ID the Current plan is used, and `bfc prime implement T-n` names a Task of the Current plan; without a Current plan the output says to name a plan, and to run `bfc start` only when the user asks. More than two arguments after `implement` is an error.
- AC-5: Instead of a Task, the output gives a note and exits 0 when the plan is not `approved` or `in-progress`, when `T-n` is not a Task of the plan (listing its Tasks), when the plan has no Tasks, and when every Task is completed (pointing to the pull-request step of the skill).
- AC-6: The output is at most 8 kB; when it would be longer, `plan.md` is cut at a line with a note to run `bfc show <id>`, and the skill, the Task, the acceptance criteria and the Task's Handoffs are kept whole.
- AC-7: The skill has numbered steps, each with "Done when", covering in this order: Current plan (`bfc start` only on the user's request), `bfc pull`, `bfc claim`, read (Task, ACs, `plan.md`, earlier Handoffs), tests first, implement, the repository's checks from `CLAUDE.md` / `CONTRIBUTING.md`, commit and push the code branch, `bfc handoff`, `bfc push`, stop.
- AC-8: The skill says that a failing check or a missing decision ends with `bfc handoff` without `--done` that says why, plus `bfc ask --to <plan author>` when a person must decide; that the agent never hands off `--done` with failing checks; and that it works one Task per turn unless the user asked for more.
- AC-9: The skill gives the Handoff structure (a one-line summary, then `## Changed`, `## Acceptance criteria`, `## Tests`, `## Left`, `## Questions`), and `bfc handoff` still accepts a note without it.
- AC-10: The skill says that after the last open Task is completed the agent tells the user and offers one pull request for the plan, runs `gh pr create` only on the user's yes with a line `Plan: <Short ID>` in the body, and never merges or takes a Gate; a body with that line is found by `bfc check find`.
- AC-11: `bfc prime` lists `bfc prime implement` among its commands, the unknown-topic error lists `check, grill, implement, plan`, and both the built-in and this repository's `beforecode` skill name `bfc prime implement [id] [T-n]` for implementing a plan or a Task; `bfc doctor` reports this repository's copy as current.
- AC-12: `docs/design.md` describes `bfc prime implement` in the Prime section, next to the other topics.
