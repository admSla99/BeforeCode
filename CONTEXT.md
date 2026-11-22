# Context: BeforeCode vocabulary

Use these terms exactly. If a new concept appears, add it here in the same pull request.

| Term | Meaning |
| --- | --- |
| **Plan** | The unit of work. A directory in the plans ref holding `intent.md`, `plan.md` and its history. Identified by a ULID and a Short ID. Its title and labels live in the Intent. |
| **Short ID** | The human handle of a Plan, such as `bfc-7k2m`. At least four characters, longer only when needed to be unique. Fixed when the Plan is created and never changes. |
| **Intent** | `intent.md`. Very short, written by a human: the problem, who it is for, how we know it worked, what is out of scope. |
| **Plan document** | `plan.md`. Written for agents after grilling: context, approach, affected areas, out of scope, tasks, acceptance criteria. This is what gets approved. |
| **Acceptance criterion** | A verifiable statement in `plan.md` with a stable ID (`AC-1`, `AC-2`, …). The plan check reports on each one. |
| **Event** | An append-only record of a state change (`created`, `submitted`, `withdrawn`, `approved`, `changes-requested`, `closed`, and from format v2 `claimed`, `released`, `completed`, `done`). Plan state is derived from events. |
| **Plan state** | `draft` → `in-review` → `approved` → `in-progress` → `done`, or `closed` from any state; `done` and `closed` are final. A plan is `in-progress` when it is approved and a Task has been claimed; editing `plan.md` returns it to `in-review`. Derived, never stored. A Plan without a valid `created` Event is an **invalid plan**: it has no lifecycle state, is listed with the state `invalid`, and is reported by `bfc doctor`. |
| **Last activity** | The time of the newest change to any file of a Plan in the Plans ref. Plans are listed newest activity first. |
| **Message** | One post in the discussion of a Plan, by a person or the AI. Messages are never edited. A Message can reply to another Message and can refer to a part of the Plan (a section or an acceptance criterion). |
| **Question** | A Message addressed to a person or to the AI. It is open until it is answered. An open question blocks approval. |
| **Thread** | A Message that replies to nothing, together with all replies to it. People and the AI both post. |
| **Discussion** | Every Message of a Plan presented as one document: each Thread in the order of its first Message, each reply under the Message it replies to, each Question marked open or answered. `bfc show` prints it after the documents. |
| **Grill** | An interactive session where the AI challenges the developer one question at a time, driven by a Skill. Each question is a Question by the AI to the plan author, each answer a Message that answers it. |
| **Grill round** | One step of a Grill: the AI records a Question to the plan author with `bfc ask`, asks it, and records the author's answer word for word with `bfc reply --verbatim`. Not a Review round. |
| **Skill** | A `SKILL.md` file in `.beforecode/skills/` that drives a grill, a review or a plan check. Versioned on normal branches. Not the `beforecode` skill that `bfc setup claude` writes to `.claude/skills/`: that is a Claude Code skill which only points the agent to Prime. |
| **Gate** | A lifecycle step that only a person may take, never an agent: submit, approve, request changes, withdraw, close, done. Acknowledging a Finding is for people too. |
| **Approval** | An `approved` event by someone other than the plan author. It approves one exact version of the Plan document; any later edit to `plan.md` makes it stale and the plan returns to `in-review`. Policy is in `.beforecode/config.yaml`; default is one approver. An agent never approves. |
| **Review round** | The time from a `submitted` Event to the `withdrawn` or `changes-requested` Event that ends it. Only Approvals given within the open round count; a new `bfc submit` starts a new round that needs new Approvals. |
| **Approval policy** | How many Approvals a Plan needs, set in `.beforecode/config.yaml`. The number in force when the Plan is submitted is the one that counts. |
| **Local mode** | A repository where the Plans ref is never pushed, for solo use. Set at `bfc init --local`. |
| **Task** | A unit of implementation work listed in the `## Tasks` section of `plan.md` with a stable ID (`T-1`, `T-2`, …), taken by an agent or a person after approval. Tasks have no dependencies; the order of the list is the order of the work. A Task is open, claimed, Paused or completed. |
| **Claim** | An Event by which an agent or person takes a Task and becomes its holder. When two Claims on one Task meet, the earlier one wins in every clone. A Claim coordinates the work; it does not lock the code. |
| **Handoff** | A note the holder of a Task leaves when they stop working on it: either the Task is completed, or it is released and becomes Paused, so the next one picks it up with the note. |
| **Paused** | A Task whose holder released it with a Handoff. Anyone may claim it again. |
| **Plan check** | Verification of a pull request against `plan.md`, run by Claude Code in GitHub Actions. It reports each acceptance criterion as met, not met, or cannot verify, plus out-of-scope changes. Only the newest check of a pull request counts. |
| **Finding** | A mismatch in the newest plan check of a pull request: an acceptance criterion that is not met, or an out-of-scope change. "Cannot verify" is not a Finding. A Finding labels the pull request `plan-mismatch` and waits in the plan author's Inbox until a newer check of that pull request is clean or the check is acknowledged. |
| **Acknowledgement** | A Message by a person that accepts the Findings of one plan check on purpose, with the reason. A newer check needs its own. |
| **Waiting on you** | The section of Prime that lists what waits on the git author of the clone, the same as the Inbox: Findings on their plans, plans in review they may approve now, and open Questions addressed to them. |
| **Inbox** | The TUI start screen listing what waits on the current user: Findings on their plans, reviews they may approve, Questions addressed to them; later also their Tasks in progress, Tasks ready for an agent and Paused Tasks. |
| **Sync** | Pull, then push of the Plans ref. Pull fetches the remote's Plans ref and fast-forwards or merges it into the local one; push sends the local one, merging and retrying when someone pushed first. Never forced. |
| **Conflict** | A file that the local and the remote Plans ref changed differently, so they do not merge on their own. Only `intent.md` and `plan.md` can conflict; nothing is committed until one side is kept with `--keep local` or `--keep remote`. |
| **Plans ref** | The hidden git ref `refs/beforecode/plans` that stores all plans. Not a branch, not a tag. |
| **Index** | The local SQLite cache of the Plans ref in `.git/beforecode/index.db`, used to list and search plans. Never a source of truth: deleting it loses nothing. |
| **Current plan** | The plan the work in a clone belongs to: the one whose Short ID is in the current branch name, otherwise the one set with `bfc use`. |
| **Prime** | The instructions `bfc prime` prints for an agent at the start of a session: how to work with `bfc`, then the Current plan. |
| **Harness** | An agent runtime such as Claude Code or Codex. |
| **Agent adapter** | The Go implementation of the `Agent` interface for one harness. |
