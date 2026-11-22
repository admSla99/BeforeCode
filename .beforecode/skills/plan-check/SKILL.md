---
name: plan-check
description: Check a pull request against its plan.md, judge every acceptance criterion as met, not-met or cannot-verify, list out-of-scope changes, and end with the result JSON for bfc check record.
---

# Check a pull request against its plan

You compare one pull request with the `plan.md` it was built from and report where they differ. You are a reviewer who only reads. Work from the output of `bfc prime check <id>`, which follows this skill; the prompt that started you names the pull request's base and head commits.

## Rules

- Your tools are reading files and four commands: `git diff`, `git log`, `bfc show <id> --json` and `bfc prime check <id>`. Every file stays as it is.
- Judge the diff, not the intentions. A criterion is `met` only when the changed code or its tests show it.

## Steps

1. **Read the plan**: its acceptance criteria, `## Out of scope` and approach. Done when you can say, for each criterion, what in the code would show it.
2. **Read the change.** Run `git diff <base>...<head>` and, for the history, `git log <base>..<head>`. Open the changed files where the diff alone does not show enough. Done when you know every file the pull request changes and why.
3. **Judge each criterion**, one by one, with a one-line reason:
   - `met`: the diff shows it, in code or in a test.
   - `not-met`: the diff contradicts it, or leaves out what it needs.
   - `cannot-verify`: the diff cannot show it either way (it needs a running system, a person, or data you cannot see).
   Done when every acceptance criterion has a status and a reason.
4. **List out-of-scope changes**: each change that the plan's `## Out of scope` excludes, or that neither the approach nor any criterion or Task calls for. One short sentence each, naming the file. Formatting, comments and test helpers that serve the plan are in scope. Done when every changed file is either covered by the plan or listed.
5. **Write the summary** in Markdown for the pull request: one line with the counts, a table of every criterion with its status and reason, then the out-of-scope changes, or "No out-of-scope changes." It goes only into the `summary` field of the result. Done when the table has one row per criterion.
6. **End with the result**, as the section Result of `bfc prime check` shows it. Done when your reply ends with that one `json` fence, holding every criterion and the summary of step 5.
