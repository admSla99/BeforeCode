# Context: BeforeCode vocabulary

Use these terms exactly. If a new concept appears, add it here in the same pull request.

| Term | Meaning |
| --- | --- |
| **Plan** | The unit of work. A directory in the plans ref holding `intent.md`, `plan.md` and its history. Identified by a ULID and a Short ID. Its title and labels live in the Intent. |
| **Short ID** | The human handle of a Plan, such as `bfc-7k2m`. At least four characters, longer only when needed to be unique. Fixed when the Plan is created and never changes. |
| **Intent** | `intent.md`. Very short, written by a human: the problem, who it is for, how we know it worked, what is out of scope. |
| **Plan document** | `plan.md`. Written for agents after grilling: context, approach, affected areas, out of scope, tasks, acceptance criteria. This is what gets approved. |
| **Acceptance criterion** | A verifiable statement in `plan.md` with a stable ID (`AC-1`, `AC-2`, …). The plan check reports on each one. |
| **Event** | An append-only record of a state change (`created`, `submitted`, `withdrawn`, `approved`, `changes-requested`, `closed`; later `claimed`, `checked`, `done`). Plan state is derived from events. |
| **Plan state** | `draft` → `in-review` → `approved` → `in-progress` → `done`, or `closed` from any state. Derived, never stored. A Plan without a valid `created` Event has no state: it is an **invalid plan**, reported by `bfc doctor`. |
| **Message** | One post in the discussion of a Plan, by a person or the AI. Messages are never edited. A Message can reply to another Message and can refer to a part of the Plan (a section or an acceptance criterion). |
| **Question** | A Message addressed to a person or to the AI. It is open until it is answered. An open question blocks approval. |
| **Thread** | A Message that replies to nothing, together with all replies to it. People and the AI both post. |
| **Grill** | An interactive session where the AI challenges the developer one question at a time, driven by a skill. Answers are written into the plan; unanswered ones become Questions. |
| **Skill** | A `SKILL.md` file in `.beforecode/skills/` that drives a grill, a review or a plan check. Versioned on normal branches. |
| **Approval** | An `approved` event by someone other than the plan author. It approves one exact version of the Plan document; any later edit to `plan.md` makes it stale and the plan returns to `in-review`. Policy is in `.beforecode/config.yaml`; default is one approver. An agent never approves. |
| **Approval policy** | How many Approvals a Plan needs, set in `.beforecode/config.yaml`. The number in force when the Plan is submitted is the one that counts. |
| **Local mode** | A repository where the Plans ref is never pushed, for solo use. Set at `bfc init --local`. |
| **Task** | A unit of implementation work listed in `plan.md`, taken by an agent after approval. |
| **Claim** | An event that assigns a task to an agent or person. The first claim to reach the remote wins. |
| **Handoff** | A note an agent leaves after working on a task. |
| **Plan check** | Verification of a pull request against `plan.md`, run by Claude Code in GitHub Actions. It reports each acceptance criterion as met, not met, or cannot verify, plus out-of-scope changes. |
| **Finding** | A mismatch reported by a plan check. It labels the pull request `plan-mismatch` and appears in the plan author's Inbox. |
| **Inbox** | The TUI screen listing what waits on the current user: reviews, questions, findings. |
| **Plans ref** | The hidden git ref `refs/beforecode/plans` that stores all plans. Not a branch, not a tag. |
| **Harness** | An agent runtime such as Claude Code or Codex. |
| **Agent adapter** | The Go implementation of the `Agent` interface for one harness. |
