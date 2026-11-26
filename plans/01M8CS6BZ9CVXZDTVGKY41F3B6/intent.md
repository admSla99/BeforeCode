---
title: "Auto-implementation: work the Tasks in Claude Code once a plan is approved"
labels: []
---

## Problem

After a plan is approved, implementation starts only when a person runs `bfc start`, opens Claude Code and tells the agent to claim a Task, then does it again for every next Task. The Tasks of `plan.md` are already written as units one agent can claim, finish and hand off, so this hand-holding is pure waiting: an approved plan sits idle until someone comes back to it.

Auto-implementation is a feature that can be turned on and off. When it is on and a plan becomes `approved`, `bfc` starts Claude Code on it by itself, and the agent takes the Tasks one after another (`bfc claim`, implement, `bfc handoff --done`, `bfc push`, next Task) until none is open, or until something needs a person.

## Who it is for

- Plan authors who want an approved plan implemented without driving each Task by hand.
- Reviewers, indirectly: their approval becomes the start signal, so approving means "go".

## Possible approaches

Constraints from the settled decisions: no daemon and no MCP server (ADR 0008), agents use the `bfc` CLI, Gates stay with people (ADR 0011), `bfc start` changes the branch only on the user's request (ADR 0009), `bfc` never calls the GitHub API (ADR 0015). The Grill already shows the pattern of a headless Claude Code turn started by the TUI (ADR 0012).

1. **The TUI starts it (Build screen light).** While `bfc` runs, the app notices after a sync that a plan the current user authored (or opted into) turned `approved`, and, with the switch on, starts headless Claude Code through the agent adapter, one turn per Task: claim, implement, hand off, then the next Task. Output streams in a screen like the Grill. The user's opt-in is the switch itself, which stands in for "the user asked" of `bfc start`; the work runs in a worktree of the plan's branch, so the user's own checkout is untouched. Fits the existing architecture best and is a step toward the phase 4 Build screen. Weakness: nothing happens while the TUI is closed.
2. **A CLI loop the user starts: `bfc implement [id]`** (or `bfc build`). It waits until the plan is approved (like `bfc wait`), then runs `claude -p` once per Task in order until no Task is open, printing progress. Runs in any terminal or in `tmux`, also on a server. Not a daemon because the user starts and stops it, but it does nothing unless someone left it running.
3. **Claude Code hooks keep an interactive session going.** `bfc setup claude` adds a `Stop` hook (and maybe `SessionStart`): when the agent tries to stop and the Current plan still has an open Task, the hook blocks the stop and tells it to hand off and claim the next one. Uses the hook machinery `bfc` already ships (ADR 0011); no new process. But someone must open Claude Code on the plan first, so it automates "next Task" rather than "start after approval".
4. **Remote run on approval (CI).** A workflow fires when the plans ref is pushed with a new approval and runs Claude Code in GitHub Actions, pushing a branch and a pull request. Fully hands-off, but it reverses ADR 0015 (the plan check left CI on purpose), needs secrets and API costs in CI, and pushes code nobody watched.
5. **A background watcher.** A process that polls the remote and starts agents. Simplest to explain, but it is the daemon ADR 0008 rules out.

A likely shape is 1 or 2 for "start after approval", combined with 3 for "keep taking the next Task", sharing one per-Task prompt from `bfc prime`.

## Questions for the Grill

- Where the switch lives: `.beforecode/config.yaml` (the whole team, config version bump), a local per-clone setting in `.git/beforecode/`, a per-plan label or field, or all of them with an order.
- Whose machine runs it when the plan was approved in another clone: the plan author's, whoever turned it on, or the first to claim.
- Branch and worktree: reuse `bfc start`'s branch rules in a separate worktree, or work in the current checkout.
- When the agent stops: a failing test or build, a Question to a person (`bfc ask --wait`), a Task that needs a decision, a limit on Tasks, time or cost.
- Permissions: which tools the headless agent may use without asking (edits, `go test`, `git commit`), and whether it may push the branch or open the pull request.
- One Task per Claude session (fresh context, the Handoff carries the state) or one session for the whole plan.
- What the user sees: live output, a notice in the plan list, the Inbox when it stops.

## How we know it worked

- With the switch off, nothing changes: approving a plan starts nothing.
- With the switch on, approving a plan with Tasks T-1 to T-3 leads, without any further command from a person, to three `claimed` and three `completed` Events in Task order, each with a Handoff, and commits on the plan's branch.
- The agent works one Task at a time and claims the next only after the previous one is handed off.
- When the agent cannot go on (failing checks, an open Question to a person), it hands the Task off as Paused, stops, and the user sees why.
- Gates are never taken by the agent: `done`, the pull request merge and anything ADR 0011 reserves for people stay with people.
- The user's own working tree and current branch are not touched unless they chose that.

## Out of scope

- Harnesses other than Claude Code (phase 4).
- Several agents working Tasks of one plan in parallel.
- Merging the pull request or marking the plan `done` automatically.
- Running implementation in GitHub Actions or any other remote service, unless the Grill chooses approach 4 on purpose.
