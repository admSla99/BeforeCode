# ADR 0015: The plan check runs locally in Claude Code, on request

Date: 2026-11-24. Status: accepted. Decided by the maintainer in #21 for issue #138. Supersedes the GitHub Actions part of ADR 0014; the check file of ADR 0013, `bfc check record`, Findings and `bfc acknowledge` stay.

## Decision

- **Who runs it.** A developer asks Claude Code in their clone, in their own words ("skontroluj PR #42 voči plánu", "check PR #42 against its plan"). The `beforecode` skill and `bfc prime` tell the agent that such a request means `bfc prime check`; the `plan-check` Skill it prints carries the whole procedure. Nothing runs on its own: there is no workflow, no schedule and no hook.
- **The procedure**, all through commands the agent already uses (ADR 0008):
  1. Read the pull request with `gh pr view <n> --json headRefName,headRefOid,baseRefName,baseRefOid,body`. This is read-only; the agent never comments on, labels or otherwise changes the pull request.
  2. Fetch its commits (`git fetch origin <base branch> pull/<n>/head`) without touching the working tree or the current branch.
  3. Find the plan with `bfc check find --branch <head ref> --body-file -`, with the description on stdin: the Short ID in the branch name, else a `Plan: bfc-…` line (unchanged from ADR 0014). Without a plan it tells the user and stops.
  4. Run `bfc prime check <id>` and judge the diff `<base>...<head>` as the `plan-check` Skill says.
  5. Store the result with `bfc check record <id> --pr <n> --head <sha> --base <sha>` and send it with `bfc push`.
  6. Report the summary to the user.
- **Where the result is seen.** Only in `bfc`: `bfc show`, the Inbox in the TUI and, later, the desktop app. `bfc` is the interface for plans, Findings and Acknowledgements; GitHub shows the code. There is no `plan-mismatch` label and no pull request comment.
- **Author and cost.** The check's author is the git author of the clone that ran it, with `agent` set (ADR 0002 section 3). It runs on that developer's Claude Code, like every other AI use in BeforeCode; there is no shared API key and no repository secret.
- **Permissions.** `bfc check` (record and find) is no Gate, so `bfc setup claude` allows it for agents. The agent asks before running `gh` and `git fetch`, as it does for any other command it has no permission for.
- **The workflow goes.** `.github/workflows/bfc-plan-check.yml`, its template, `internal/github` and `bfc setup github` are removed (#139).

## Considered options

- **Keep the GitHub Actions workflow (ADR 0014).** Rejected by the maintainer: it needs a shared API key paid for on every push, and it puts the result on GitHub (label and comment), while `bfc` is meant to be where plans and their checks are seen.
- **Both: the workflow optional, the local check as the default.** Rejected: two ways to run one check, with a template, a release download and a version check to maintain for the one nobody uses.
- **Write a comment to the pull request from the local run.** Rejected: the result belongs in `bfc`; a comment would be a second copy that `bfc` cannot update or acknowledge.
- **A command `bfc check run <pr>`** that starts Claude Code headless through the agent adapter. Rejected for now: the developer already talks to Claude Code, and instructions in the skill are enough. The TUI or desktop app may start a check later through the adapter.

## Consequences

- A pull request is checked only when someone asks. A Finding reaches the plan author's Inbox after their next sync, as before.
- The phase 3 exit criterion becomes: a pull request outside its plan, checked on request, gives a Finding that the plan author sees in the Inbox (#121).
- The fork question of ADR 0014 (#29) no longer applies to the plan check: it runs in a clone with write access to the Plans ref.
- `.goreleaser.yaml` no longer has to keep archive names stable for a workflow, though the release keeps them.
