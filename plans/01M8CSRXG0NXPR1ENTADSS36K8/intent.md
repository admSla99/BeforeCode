---
title: "bfc prime implement: how an agent works one Task"
labels: []
---

## Problem

The Grill (`bfc prime grill` + `grill-intent`), writing `plan.md` (`bfc prime plan` + `write-plan`) and the plan check (`bfc prime check` + `plan-check`) each give the agent a skill with numbered steps and a "done when" for each. Implementing a Task, the step all of them lead to, has no such skill. When a user says "implement T-2 of bfc-xxxx", the agent gets only a few lines of `bfc prime`: claim a Task, hand it off, record a deviation with `bfc ask`. It is not told:

- to read the Task's acceptance criteria and the previous Handoffs before it starts;
- to work test first and to show which criteria the Task meets;
- to run the repository's checks before it hands off;
- when to commit and whether the commit comes before the Handoff;
- what a Handoff should say, so the next agent or person can continue without the conversation;
- what to do when a check fails or a decision is missing: hand off as Paused, or ask;
- whether to open the pull request after the last Task, and that it must name the plan (`Plan: bfc-…` or the branch), so `bfc prime check` finds it;
- that it works one Task at a time and stops after it, rather than taking the next Task on its own.

So the outcome depends on what each agent guesses and on the repository's own `CLAUDE.md`. Auto-implementation (bfc-cvxz) needs exactly this per-Task procedure to run Tasks without a person in the loop.

## Who it is for

- Developers who ask Claude Code to implement a Task of an approved plan.
- The next agent or person who continues a Paused Task from its Handoff.
- Reviewers and the plan check, which read the pull request against the plan.
- bfc-cvxz, which will start these turns automatically.

## How we know it worked

- `bfc prime implement [id] [T-n]` prints the skill `implement-task` (shipped in the binary like the others, the repository's copy wins), then the plan, the Task to work on (the given one, or the Task the agent holds, or the first one `bfc ready` would offer), its acceptance criteria, `plan.md`, and the full Handoffs of that Task.
- `bfc prime` and the `beforecode` skill point to `bfc prime implement` when the user asks to implement a plan or a Task.
- The skill has numbered steps, each with a "done when", covering: make sure there is a Current plan (`bfc start` only on the user's request), pull, claim, read, write the tests first, implement, run the repository's checks, commit, hand off with a fixed structure (what changed, which ACs it covers, test status, what is left, open questions), push the plans ref.
- A failing check or a missing decision ends with a Handoff without `--done` (Paused) that says why, and a Question to the plan author when a person must decide; the agent never hands off `--done` with failing checks.
- After the last open Task is completed, the skill tells the agent to say so and offer the pull request, naming the plan; it never takes a Gate (`done`) and never merges.
- The agent stops after one Task unless the user asked for more.
- A test runs `bfc prime implement` against a fixture with Tasks and Handoffs and checks the output; the skill text is checked like the other skills (`skills_test.go`).

## Questions for the Grill

- Who chooses the Task when the user names none: the agent (`bfc ready`, first open), or does it ask?
- Commits: one per Task, several allowed, or none (the user commits)? Does the agent push the code branch?
- The pull request: the agent opens it after the last Task (with `gh pr create`), only offers it, or leaves it to the user? One pull request per plan or per Task?
- The repository's checks: read from `CLAUDE.md`/`CONTRIBUTING.md` by the agent, or named in `.beforecode/config.yaml` (for example `checks: [go test ./..., golangci-lint run]`) so bfc-cvxz can run them the same way?
- Handoff format: free text with suggested sections, or required headings that `bfc handoff` checks?
- Should the Handoff of a completed Task record which ACs it covers in a field, or only in the text?

## Out of scope

- Starting implementation automatically after approval (bfc-cvxz).
- Several agents on one plan at once, worktrees, the phase 4 Build screen.
- Harnesses other than Claude Code.
- Changing the data format of Handoffs, unless the Grill decides on a structured field.
