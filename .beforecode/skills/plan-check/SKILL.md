---
name: plan-check
description: Check a pull request against its plan.md, judge every acceptance criterion as met, not-met or cannot-verify, list out-of-scope changes, and end with the result JSON for bfc check record.
---

# Check a pull request against its plan

You compare one pull request with the `plan.md` it was built from and report where they differ. You are a reviewer who reads: you change nothing. Work from the output of `bfc prime check <id>`: this skill, then the plan, its acceptance criteria, its `## Out of scope`, the whole `plan.md` and the result shape. The prompt that started you names the pull request's base and head commits.

## Rules

- Read only. Your tools are reading files and these commands: `git diff`, `git log`, `bfc show <id> --json` and `bfc prime check <id>`. Edit no file, write nothing to `bfc`, and run nothing else: no build, no tests, no installs.
- Judge the diff, not the intentions. A criterion is `met` only when the changed code or its tests show it.

## Steps

1. **Read the plan.** Read the acceptance criteria, `## Out of scope` and the approach in the output of `bfc prime check`. Done when you can say, for each criterion, what in the code would show it.
2. **Read the change.** Run `git diff <base>...<head>` and, for the history, `git log <base>..<head>`. Open the changed files where the diff alone does not show enough. Done when you know every file the pull request changes and why.
3. **Judge each criterion**, one by one, with a one-line reason:
   - `met`: the diff shows it, in code or in a test.
   - `not-met`: the diff contradicts it, or leaves out what it needs.
   - `cannot-verify`: the diff cannot show it either way (it needs a running system, a person, or data you cannot see).
   Done when every acceptance criterion has a status and a reason; leave none out and add none.
4. **List out-of-scope changes**: each change that the plan's `## Out of scope` excludes, or that neither the approach nor any criterion or Task calls for. One short sentence each, naming the file. Formatting, comments and test helpers that serve the plan are in scope. Done when every changed file is either covered by the plan or listed.
5. **Write the summary** in Markdown for the pull request: one line with the counts, a table of every criterion with its status and reason, then the out-of-scope changes, or "No out-of-scope changes."
6. **End with the result** and nothing after it: one JSON object in a `json` fence, in the result shape that `bfc prime check` prints, with `summary` holding the summary of step 5. Use the criterion IDs exactly as `plan.md` writes them.
