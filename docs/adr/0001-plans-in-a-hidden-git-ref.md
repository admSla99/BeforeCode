# ADR 0001: Plans live in a hidden git ref

Date: 2026-11-16. Status: accepted. Verified on real GitHub on 2026-11-16 (issue #4).

## Decision

All plans, messages, events, handoffs and checks are stored as plain files in the tree of `refs/beforecode/plans` on the same remote as the code. Only `.beforecode/config.yaml`, `.beforecode/skills/` and `.beforecode/templates/` live on normal branches. The file formats are in ADR 0002.

## Why

- Sharing needs nothing but `git push` and `git fetch`. No server, no account.
- Discussion and approvals never pollute code history or collide with branch protection on `main`.
- Beads uses the same pattern (`refs/dolt/data`) and removed its earlier sync-branch-plus-hidden-worktree design.
- Unlike Beads we store plain Markdown, not a Dolt database: plans are documents, readable with `git show`, with no pinned database dependency.

## Verified locally (git 2.43, two clones, bare remote)

- Writing through plumbing leaves `git status` and the branch list untouched.
- A plain `git clone` does not fetch the ref; the refspec `+refs/beforecode/*:refs/beforecode-remote/*` does.
- A concurrent second push is rejected as non-fast-forward; fetch + `git merge-tree --write-tree` + push succeeds without conflict when both sides only added files.

## Verified on GitHub (git 2.54, 2026-11-16)

Run with `scripts/spike-hidden-ref/run.sh admSla99/bfc-ref-spike` against a public scratch repository, three runs, all passing. Workflow runs 43961166899, 43961482623 and 43961746731.

- **Push and fetch.** A plumbing commit pushed to `refs/beforecode/plans` from one clone is fetched in a second clone with `+refs/beforecode/*:refs/beforecode-remote/*` and its files are readable with `git show`. `git status`, `HEAD` and branches stay untouched.
- **Concurrent writes.** The second of two concurrent pushes is rejected. GitHub reports it as `! [rejected] … (fetch first)`, not with the literal word `non-fast-forward`. Fetch, `git merge-tree --write-tree`, a two-parent `commit-tree` and a new push succeed and keep the files from both sides.
- **Read from Actions.** A workflow triggered by `pull_request` fetches the ref with the same refspec after `actions/checkout` and reads a file from it.
- **Write from Actions.** The same workflow writes a file through plumbing and pushes it with `GITHUB_TOKEN`. The workflow must declare `permissions: contents: write`, because the repository default for `GITHUB_TOKEN` is `read`, both in the scratch repository and in BeforeCode. The commit author is `github-actions[bot]`.
- **No workflow triggers.** A probe workflow on `[push, create, delete]` without filters ran for branch pushes, branch creation and branch deletion, but never for any push to `refs/beforecode/plans` (checked in the first and third runs; in the second the run listing lagged, so the script now waits for it).
- **REST API.** `GET /repos/{owner}/{repo}/git/matching-refs/beforecode` lists `refs/beforecode/plans`. `GET /branches` does not.
- **Web UI.** `github.com/{owner}/{repo}/tree/refs/beforecode/plans` returns 404, and the ref does not appear in branch or tag lists. The commits are still viewable by SHA (`/tree/<sha>` returns 200).
- **Clones.** A plain `git clone` does not carry the ref; `git ls-remote` shows it. `git clone --mirror` does carry it.

Not verified: whether a GitHub fork carries the ref. GitHub does not allow forking a repository into the account that owns it, and the maintainer account has no organization. Tracked in #29. Running the plan check on pull requests from forks is a separate, documented limit (see Consequences).

## Consequences

- The ref is invisible in the GitHub web UI. Reading plans needs `bfc` (or `bfc export`). The REST API can list and read it, which a later web view can use.
- Plain clones do not carry it (forks: not verified). New team members run `bfc bootstrap`. Mirror clones and backups made with `--mirror` do carry it.
- Branch protection does not apply. Anyone with write access can rewrite the ref. Accepted for now.
- Pushes to the ref do not trigger GitHub Actions. The plan check runs from the pull request and fetches the ref itself.
- The plan check workflow must declare `permissions: contents: write` to write `checks/` into the ref.
- The sync code must treat any rejected push of the ref (`fetch first` or `non-fast-forward`) as "fetch, merge in memory, retry", not match one message.
- On pull requests from forks GitHub gives `GITHUB_TOKEN` read-only access (GitHub documentation; not tested here). The plan check cannot write to the ref or label such a pull request. Acceptable because BeforeCode users have write access and push branches to the same repository. Follow-up: #29.
- Everyone who uses `bfc` needs write access to the repository.
