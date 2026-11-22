# ADR 0013: Data format v2: Tasks, Claims, Handoffs and checks

Date: 2026-11-22. Status: accepted. Settled with the maintainer in the phase 3 grilling (#21), recorded for issue #99. Extends ADR 0002; everything in ADR 0002 that this document does not change holds for `format-version` 2.

## Decision

Phase 3 adds everything that changes plan state in one bump: `format-version` 2. A binary supports exactly version 2 from then on (ADR 0002 section 2).

### 1. Migration

- `bfc migrate` rewrites `format-version` from `1` to `2` in one new commit to the Plans ref and changes nothing else. On a v2 ref it says there is nothing to do. It never pushes; the next Sync does.
- A v2 binary reads a v1 ref and refuses to write to it with a message that names `bfc migrate`. `bfc doctor` reports a v1 ref as a failure with `bfc migrate` as the fix; `bfc doctor --fix` does not migrate.
- After one teammate migrates and pushes, a v1 binary refuses to write (ADR 0002 section 2), so everyone upgrades. This is why the migration is an explicit command and not a side effect.
- v2 only adds, so a v1 ref is a valid v2 ref once the file says `2`.

### 2. Layout

```
format-version                 2
plans/<ULID>/
  intent.md
  plan.md
  events/<ULID>.yaml
  messages/<ULID>.md
  handoff/<ULID>.md            new: one Handoff per file
  checks/<ULID>.md             new: one plan check result per file
```

`handoff/` and `checks/` are append-only like `events/` and `messages/`: files are never modified, renamed or deleted.

### 3. Tasks in `plan.md`

A Task is a top-level list item in the `## Tasks` section, in the form `- T-<n>: <text>`, following the rules of acceptance criteria (ADR 0002 section 6):

- `<n>` is a positive integer without leading zeros, `<text>` is non-empty, continuation lines are indented by at least two spaces.
- IDs are unique within the file and are never reused or renumbered, because Claims and Handoffs refer to them.
- Tasks have no dependencies. The order of the list is the order of the work: `bfc ready` and `bfc claim` without a Task take the first one that is free.
- `bfc edit` warns about malformed items and duplicate IDs, `bfc submit` refuses them. An empty `## Tasks` is allowed.

```markdown
## Tasks

- T-1: Add the Stripe client behind the `payments.Provider` interface.
- T-2: Handle the webhook idempotently,
  keyed by the event ID.
```

### 4. Events

| `type` | Extra fields | Who may write it | Effect |
| --- | --- | --- | --- |
| `claimed` | `task` (`T-<n>`) | anyone, agents included | The writer holds the Task |
| `released` | `task`, `handoff` (ULID) | the holder of the Task | The Task is Paused; anyone may claim it again |
| `completed` | `task`, `handoff` (ULID) | the holder of the Task | The Task is done |
| `done` | `message` (optional) | anyone, never with `agent` (a Gate) | `in-progress` → `done`, which is final |

The reserved `checked` is dropped: a plan check does not change plan state, so its result is a file in `checks/` and not an Event.

**At write time** `bfc` refuses a `claimed` unless the plan is `approved` or `in-progress`, the Task exists in the current `plan.md` and it is open or Paused. It refuses `released` and `completed` from anyone but the holder (compared by email), and `done` outside `in-progress`.

**Task state** is derived per Task from its `claimed`, `released` and `completed` Events in ULID order, after the valid `submitted` that comes first:

- `open` → `claimed` by a `claimed`; `claimed` → `paused` by a `released`, `claimed` → `completed` by a `completed`; `paused` → `claimed` by a `claimed`.
- A `claimed` while the Task is claimed or completed is skipped. **Two Claims on one Task: the earlier ULID wins**, so every clone agrees once it has both Events. `bfc claim` pulls before it writes and pushes and derives again right after, so it says at once when it lost.
- A `released` or `completed` by someone other than the holder, or for a Task that is not claimed, is skipped.
- Task state does not read `plan.md`: a Task removed from `plan.md` keeps its Events, and readers list only the Tasks the current `plan.md` has.

**Plan state** (ADR 0002 section 4) gains two steps after the review round is evaluated:

- A plan whose state is `approved` is `in-progress` when at least one Task has a valid `claimed`.
- Editing `plan.md` still makes the Approvals stale, so an `in-progress` plan returns to `in-review` like an `approved` one. Its Claims stay and the plan is `in-progress` again once the new version is approved.
- A valid `done` (one that finds the plan `in-progress`) makes the state `done`. Like `closed` it is final: every later Event is skipped, `closed` included, and every command that writes refuses the plan.

### 5. Handoffs

File `plans/<plan ULID>/handoff/<handoff ULID>.md`. YAML front matter with the common fields (ADR 0002 section 3), `task` and `done` (boolean), then the note in Markdown.

```markdown
---
author:
  name: Jana Nováková
  email: jana@example.com
at: 2026-11-22T10:15:00Z
agent: claude-code
task: T-2
done: false
---

The webhook handler is in place; the idempotency key is not stored yet. Next: the migration for `processed_events`.
```

`bfc handoff` writes the Handoff and its `released` (`done: false`) or `completed` (`done: true`) Event, whose `handoff` field names it, in one commit. A Handoff without its Event is shown as a note and changes nothing.

### 6. Plan check results

File `plans/<plan ULID>/checks/<check ULID>.md`, written by `bfc check record` (ADR 0014).

```markdown
---
author:
  name: github-actions[bot]
  email: 41898282+github-actions[bot]@users.noreply.github.com
at: 2026-11-22T11:00:00Z
agent: claude-code
plan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D
pr: 42
head_sha: 9f2c…
base_sha: 1a7e…
plan_blob: 3b18e512dba79e4c8300dd08aeb37f8e728b8dad
criteria:
  AC-1: met
  AC-2: not-met
  AC-3: cannot-verify
out_of_scope:
  - Changes the retry policy of the email worker.
---

<the summary posted to the pull request>
```

- `criteria` names every acceptance criterion of `plan.md` at `plan_blob`, each `met`, `not-met` or `cannot-verify`. `bfc check record` refuses a result that leaves one out, names one that does not exist, or uses another status.
- **Findings.** For each pull request only its newest check (by ULID) counts. Each `not-met` criterion and each `out_of_scope` item of that check is a Finding. `cannot-verify` is shown but is not a Finding: the check reports it often, and a label on nearly every pull request would mean nothing.
- A check whose `plan_blob` is not the current `plan.md` is stale; it still counts until the next check of that pull request.

### 7. Acknowledgements

A Message (ADR 0002 section 7) may have the new `ref` form `checks/<ULID>`. A Message with such a `ref`, written by a person (no `agent`) with `bfc acknowledge`, is an **Acknowledgement** of that check: its Findings no longer wait on anyone. A newer check of the same pull request needs its own. The reason stays in the Discussion.

## Considered options

- **Tasks as `### T-1` headings or as files in the ref.** Rejected: headings make the task body free-form and hard to cut for Prime; files would split what gets approved (`plan.md`) from the work. A list item mirrors acceptance criteria, which people already know.
- **Task dependencies** (`after: T-1`). Rejected for now: the list order says the same for the plans we write, and the Inbox can group Tasks visually without a data model (#82).
- **"The first Claim to reach the remote wins" by commit order.** Rejected: after a merge in memory (ADR 0004) the order of commits in the ref says nothing reliable about who pushed first, and two clones could derive different holders. The ULID gives one answer everywhere; the sync around `bfc claim` makes the practical result the same.
- **A `checked` Event next to the check file.** Rejected: a check changes no state, and two places for one fact can disagree.
- **An `acknowledged` Event.** Rejected: an Acknowledgement is a reason someone gives, which belongs in the Discussion; a Message is already append-only and shown everywhere.
- **`cannot-verify` as a Finding.** Rejected, see section 6.
- **Migration in `bfc doctor --fix`.** Rejected: it decides which binaries may still write in the whole team, so it must be an explicit step, not a side effect of a repair.
- **One bump per feature.** Rejected: each bump is another migration and another period in which old binaries refuse to write.

## Consequences

- `format-version` 2 is written by `bfc init`; existing repositories run `bfc migrate` once.
- State derivation still needs only the object ID of `plan.md`, not its content.
- Readers must list Tasks from `plan.md` and join their state from Events; a Task with Events but no line in `plan.md` is reported by `bfc doctor`.
- `bfc doctor` lists skipped `claimed`, `released`, `completed` and `done` Events like other skipped Events, and reports a check or Handoff file it cannot read.
