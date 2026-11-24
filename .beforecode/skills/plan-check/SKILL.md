---
name: plan-check
description: Check a pull request against its plan.md when the user asks: find the pull request's plan and commits, judge every acceptance criterion as met, not-met or cannot-verify, list out-of-scope changes, store the result with bfc check record and push it.
---

# Check a pull request against its plan

The user asked you to check a pull request against its plan ("check PR #42 against its plan"). You compare the pull request with the `plan.md` it was built from, store where they differ in BeforeCode, and tell the user. You are a reviewer who reads: the code, the working tree, the current branch and the pull request on GitHub stay as they are. The result lives in `bfc`, where the plan author sees its Findings in the Inbox.

## Steps

1. **Read the pull request**: `gh pr view <n> --json headRefName,headRefOid,baseRefName,baseRefOid,body`. Done when you have its head branch, head commit, base branch, base commit and description.
2. **Fetch its commits**: `git fetch origin <base branch> pull/<n>/head`. Done when `git cat-file -e <head commit>` and `git cat-file -e <base commit>` both succeed.
3. **Find its plan**: `gh pr view <n> --json body --jq .body | bfc check find --branch <head branch> --body-file -`. It prints the plan's Short ID, taken from the branch name or a `Plan: bfc-…` line in the description. When it prints nothing, tell the user the pull request names no plan (a branch with the plan's Short ID, or a `Plan: bfc-…` line in its description) and stop there. Done when you have the Short ID.
4. **Load the plan**: unless the plan below is the one step 3 found, run `bfc prime check <id>` and continue from its output. Done when the plan below is that plan.
5. **Read the plan**: its acceptance criteria, `## Out of scope` and approach. Done when you can say, for each criterion, what in the code would show it.
6. **Read the change.** Run `git diff <base commit>...<head commit>` and, for the history, `git log <base commit>..<head commit>`. Open the changed files at the head commit (`git show <head commit>:<path>`) where the diff alone does not show enough. Done when you know every file the pull request changes and why.
7. **Judge each criterion**, one by one, with a one-line reason. Judge the diff, not the intentions:
   - `met`: the diff shows it, in code or in a test.
   - `not-met`: the diff contradicts it, or leaves out what it needs.
   - `cannot-verify`: the diff cannot show it either way (it needs a running system, a person, or data you cannot see).
   Done when every acceptance criterion has a status and a reason.
8. **List out-of-scope changes**: each change that the plan's `## Out of scope` excludes, or that neither the approach nor any criterion or Task calls for. One short sentence each, naming the file. Formatting, comments and test helpers that serve the plan are in scope. Done when every changed file is either covered by the plan or listed.
9. **Write the summary** in Markdown: one line with the counts, a table of every criterion with its status and reason, then the out-of-scope changes, or "No out-of-scope changes." Done when the table has one row per criterion.
10. **Store the result**: pipe the JSON object of the section Result below, with the summary of step 9, into `bfc check record <id> --pr <n> --head <head commit> --base <base commit>`, then run `bfc push`. When `bfc check record` lists problems, correct the JSON and run it again. Done when it printed the check's ULID and `bfc push` succeeded.
11. **Report** to the user: the summary of step 9 and the check's ULID. With Findings, add that they wait in the plan author's Inbox in `bfc`, where the author fixes the code, updates the plan or acknowledges them. Done when the user has the report.
