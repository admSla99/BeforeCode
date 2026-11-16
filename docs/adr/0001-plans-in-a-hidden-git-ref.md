# ADR 0001: Plans live in a hidden git ref

Date: 2026-11-16. Status: accepted, pending verification on GitHub (see the phase 0 spike issue).

## Decision

All plans, questions, threads, events, handoffs and checks are stored as plain files in the tree of `refs/beforecode/plans` on the same remote as the code. Only `.beforecode/config.yaml` and `.beforecode/skills/` live on normal branches.

## Why

- Sharing needs nothing but `git push` and `git fetch`. No server, no account.
- Discussion and approvals never pollute code history or collide with branch protection on `main`.
- Beads uses the same pattern (`refs/dolt/data`) and removed its earlier sync-branch-plus-hidden-worktree design.
- Unlike Beads we store plain Markdown, not a Dolt database: plans are documents, readable with `git show`, with no pinned database dependency.

## Verified locally (git 2.43, two clones, bare remote)

- Writing through plumbing leaves `git status` and the branch list untouched.
- A plain `git clone` does not fetch the ref; the refspec `+refs/beforecode/*:refs/beforecode-remote/*` does.
- A concurrent second push is rejected as non-fast-forward; fetch + `git merge-tree --write-tree` + push succeeds without conflict when both sides only added files.

## Consequences

- The ref is invisible in the GitHub web UI. Reading plans needs `bfc` (or `bfc export`).
- Forks and plain clones do not carry it. New team members run `bfc bootstrap`.
- Branch protection does not apply. Anyone with write access can rewrite the ref. Accepted for now.
- Pushes to the ref do not trigger GitHub Actions. The plan check runs from the pull request and fetches the ref itself.
- Everyone who uses `bfc` needs write access to the repository.

## Not yet verified

- Push and fetch of the ref against real GitHub.
- Reading and writing the ref from GitHub Actions with `GITHUB_TOKEN`.
