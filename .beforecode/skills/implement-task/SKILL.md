---
name: implement-task
description: Implement one Task of an approved plan: claim it, read its acceptance criteria and earlier Handoffs, write the tests first, implement, run the repository's checks, commit and push, hand it off with a fixed structure and push the plans; offer the pull request after the last Task.
---

# Implement one Task

The user asked you to implement a plan or one of its Tasks ("implement T-2 of bfc-xxxx", "work on the plan"). You work one Task from claim to Handoff, so that the next agent or person can continue from the Handoff alone, without this conversation. Work from the output of `bfc prime implement [id] [T-n]`: this skill, then the plan, the Task, its acceptance criteria, `plan.md` and the Task's earlier Handoffs. Run it first if you have not.

When the user names no Task, take the one shown below: the Task you already hold, else the first one `bfc ready` offers. Tell the user which Task you took; do not ask.

## Steps

1. **Current plan.** The plan must be the Current plan and `approved` or `in-progress`; the hook blocks edits in its other states. Run `bfc start <id>` only when the user asked to work on that plan, because it switches the branch. When the plan is in another state, tell the user it needs a Gate in the `bfc` TUI and stop. Done when `bfc prime` names the plan as the Current plan, `approved` or `in-progress`.
2. **Get the latest**: `bfc pull`, and `git pull` on the code branch when it has an upstream, so you see the Claims, Handoffs and commits of others. Done when both succeeded.
3. **Claim the Task**: `bfc claim <id> T-n --json`. The first Claim wins: when it fails, take the next Task `bfc ready <id> --json` offers, run `bfc prime implement <id> T-n` for it, and tell the user. Done when the Claim printed you as the holder.
4. **Read** the Task, the acceptance criteria it touches, `plan.md` (run `bfc show <id>` when it is cut below) and every earlier Handoff of the Task, oldest first; a Paused Task continues where its last Handoff stopped. Done when you can name the acceptance criteria this Task covers and what is left of it.
5. **Write the tests first**, following the repository's test conventions: one or more tests for each criterion the Task covers, named after it where the repository does so. Done when the new tests fail for the reason the Task fixes.
6. **Implement** the Task as `plan.md` describes it, and nothing outside it. Done when the new tests and the old ones pass.
7. **Run the repository's checks**: the build, test and lint commands that `CLAUDE.md` or `CONTRIBUTING.md` name. Done when every check passes; when one fails and you cannot fix it within the Task, see "When a check fails or a decision is missing".
8. **Commit** the Task's work in one or more commits, following the repository's commit rules, then push the code branch (`git push -u origin <branch>` the first time), so the next agent in another clone continues from it. Every commit comes before the Handoff. Done when `git status` is clean and the branch is pushed.
9. **Hand off with `bfc handoff`**: `bfc handoff <id> T-n --done -m <note>` when the Task is finished, with the note structured as in "The Handoff" below. Done when it printed `completed T-n`.
10. **Send the plans**: `bfc push`. Done when it succeeded.
11. **Stop.** Work one Task per turn unless the user asked for more. Tell the user what the Task changed, which criteria it covers and what comes next. When it was the last open Task, follow "After the last Task". Done when the user has the report.

## The Handoff

The first line is a one-line summary: `bfc ready` and `bfc show` print only that line. The headings follow, each present, "none" when empty:

```
<one-line summary of what this Task did>

## Changed
<files and behaviour that changed, the commits>

## Acceptance criteria
<AC-n: covered, and by which test; or partly, and what is missing>

## Tests
<the checks you ran and their result>

## Left
<what remains of the Task, for whoever continues it>

## Questions
<open Questions, with their ULIDs>
```

Pass it with `-m` in a here-document so the headings keep their lines.

## When a check fails or a decision is missing

- Hand off without `--done`: `bfc handoff <id> T-n -m <note>`. The Task is Paused and the note says why under `## Left`: the failing check and its output, or the decision you need. Commit and push what works before it, then run `bfc push`.
- When a person must decide, ask the plan's author: `bfc ask <id> --to <plan author email> --ref plan.md#AC-n -m <the question>`, and name the Question under `## Questions`.
- Never hand off with `--done` while a check fails or a criterion of the Task is unmet.
- Tell the user what stopped you.

## Departing from plan.md

Implement `plan.md` as approved. When the work must depart from it, record the deviation with `bfc ask <id> --to <plan author email> --ref plan.md#AC-n -m <what and why>`, tell the user, and name it in the Handoff.

## After the last Task

When every Task of the plan is completed, tell the user so and offer one pull request for the plan. Only when the user says yes, run `gh pr create` with a line `Plan: <Short ID>` in the body, so `bfc check find` and `bfc prime check` find the plan. Follow the repository's rules for pull requests. Never merge it and never take a Gate (`done` or any other): those are for people in the `bfc` TUI.
