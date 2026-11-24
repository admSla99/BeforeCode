# ADR 0014: The plan check runs Claude Code headless in GitHub Actions and stores its result through `bfc`

Date: 2026-11-22. Status: superseded by ADR 0015 (the plan check runs locally in Claude Code, on request); the check file, `bfc check record`, `bfc check find`, Findings and `bfc acknowledge` stay. Settled with the maintainer in the phase 3 grilling (#21), recorded for issue #99. Builds on ADR 0001 (reading and writing the ref from Actions), ADR 0008 (agents use the CLI) and ADR 0013 (the check file).

## Decision

### The workflow

- `bfc setup github` writes `.github/workflows/bfc-plan-check.yml` from a template in the binary. The workflow downloads the release `bfc` of the version that wrote it, with its checksum, so the check runs the same rules as the clones that wrote the plan. A `dev` build refuses to write it. Rerunning `bfc setup github` rewrites the file for the installed version; `bfc doctor` warns when it is missing or from another version. BeforeCode's own workflow is the same file except that it builds `bfc` from the pull request's source.
- It runs on `pull_request` (`opened`, `synchronize`, `reopened`, `edited`) with `permissions: contents: write` (to write the ref, ADR 0001) and `pull-requests: write` (label and comment). It fetches `refs/beforecode/plans` itself, because pushes to the ref trigger no workflow.
- **Finding the plan.** The Short ID in the pull request's branch name, resolved like the Current plan (ADR 0009), else a `Plan: bfc-…` line in the description. Without a plan the job writes a notice to its summary and ends green, with no label and no comment.
- **Pull requests from forks** (`head.repo.full_name != github.repository`) are skipped with a notice: `GITHUB_TOKEN` is read-only there (ADR 0001). Supporting them is #29.

### Claude Code and `bfc`

- Claude Code runs headless (`claude -p`) with `ANTHROPIC_API_KEY` from the repository secrets, the one shared key of the AI usage decision. It is prompted with `bfc prime check <id>` (the `plan-check` Skill and the plan) and allowed only to read files and run `git diff`, `git log`, `bfc show` and `bfc prime`. It edits nothing.
- It ends with the result as JSON: every acceptance criterion as `met`, `not-met` or `cannot-verify`, the out-of-scope changes, and a Markdown summary. `bfc check record` validates it against `plan.md` and writes `checks/<ULID>.md` (ADR 0013); `bfc push` sends it, merging and retrying, never forcing. A failed or invalid run fails the job and writes nothing.
- **The workflow, not `bfc`, talks to GitHub.** With Findings it adds the `plan-mismatch` label (creating it when missing) and posts the summary as one comment, which later runs update in place (found by a hidden marker); without Findings it removes the label and updates the comment. A plan that is not `approved` or `in-progress` is still checked, and the comment says so first.
- **The check warns, it does not block.** The job fails only when the check itself could not run, never because of a Finding.
- The plan author sees the Finding in the Inbox after the next Sync. They fix the code (the next push runs a new check), update the plan, or acknowledge it with `bfc acknowledge` (ADR 0013 section 7). The label goes away only with the next clean check, because `bfc` never calls the GitHub API.

## Considered options

- **`anthropics/claude-code-action`.** Rejected: it brings its own prompt, permissions and comment handling, while the agent adapter, `bfc prime` and `--json` are already how BeforeCode drives Claude Code (ADR 0008). Writing to the ref must go through `bfc`, which validates the result.
- **`bfc` calls the GitHub API** for the label and the comment. Rejected: `bfc` would need a token, an HTTP client and GitHub-specific code for one workflow step that `gh` already does with `GITHUB_TOKEN`.
- **`go install` or building `bfc` in the workflow.** Rejected for other repositories: it needs Go in the job, is slow, and runs whatever `bfc` is newest rather than the version that wrote the file. BeforeCode itself builds from source on purpose, to check the pull request's `bfc`.
- **A workflow copied from the documentation.** Rejected: it drifts from the installed `bfc`, and `bfc doctor` could not tell.
- **`pull_request_target` for forks.** Rejected for now: it runs with a write token and secrets next to untrusted code; it needs the spike in #29 first.
- **Blocking the pull request on a Finding.** Rejected for now: the check is new and its judgement is not trusted yet (`docs/design.md` section 3).

## Consequences

- ADR 0010 widens: `.github/workflows/bfc-plan-check.yml` lives on normal branches and is changed through pull requests.
- A repository needs the `ANTHROPIC_API_KEY` secret; without it the job fails with a message that says how to add it.
- `.goreleaser.yaml` must publish the archives and checksums the workflow downloads, under stable names.
- Every check run is a commit to the Plans ref by `github-actions[bot]`, which the file names as its author (ADR 0002 section 3).
