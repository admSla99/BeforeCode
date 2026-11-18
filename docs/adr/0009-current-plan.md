# ADR 0009: The Current plan is bound to the branch by its Short ID

Date: 2026-11-18. Status: accepted. Settled with the maintainer in the phase 2 grilling (#20), recorded for issue #55.

## Decision

The **Current plan** is the plan that the work in a clone belongs to. `bfc prime` shows it, and the PreToolUse hook (ADR 0011) allows edits only when it is `approved`.

**The branch name carries it.** A branch belongs to a plan when its name contains the plan's Short ID as a whole word, anywhere in the name: `bfc-a1b2-payments-via-stripe`, `feat/bfc-a1b2`. Short IDs never change (ADR 0002 section 8), so the binding never goes stale.

**`bfc start <id>`** creates the branch `<Short ID>-<slug of the title>` (for example `bfc-a1b2-payments-via-stripe`) from the current HEAD and switches to it. If a local branch with that Short ID exists, it switches to that one. Uncommitted changes are carried as `git switch` carries them; if git refuses, the error is shown and nothing else changes. `bfc start` refuses a `closed` plan and allows every other state. It writes nothing to the Plans ref and no Event.

**`bfc use <id>`** is the fallback for work that cannot be on such a branch, for example a detached HEAD or a branch whose name is fixed elsewhere. It writes the plan's ULID to `.git/beforecode/current` and does not change the branch. `bfc use --clear` removes the file.

**Resolution.** The Short ID in the current branch name wins; otherwise the file; otherwise there is no Current plan. A detached HEAD uses the file only. A Short ID in the branch name that names no plan is reported, not ignored, so a typo does not silently fall back to the file.

**`bfc start` is the first command that changes the current branch.** It does so only because the user asked for it by running the command. The rule that reading and writing plans never touches the working tree, the index or the current branch still holds for every other command, and `bfc start` itself never touches the Plans ref.

## Considered

- **Only the file in `.git/`.** Rejected as the main mechanism: it does not follow `git switch`, so after changing branches the hook would judge edits against the wrong plan. It stays as the fallback.
- **An Event when work starts** (`started`, or the `claimed` reserved for phase 3). Rejected for phase 2: it would put one clone's local choice into the shared ref and change Plan state. `in-progress` comes with Claims in phase 3.
- **The plan in a commit trailer** (`Plan: bfc-a1b2`). Rejected: it is known only after the first commit, and the hook must decide before the first edit.
- **The ULID in the branch name.** Rejected: 26 characters nobody reads. The Short ID is what people already use in pull requests.

## Consequences

- Branches made by `bfc start` look like `bfc-a1b2-payments-via-stripe`. Renaming a branch keeps the binding as long as the Short ID stays in the name.
- Every worktree has its own HEAD, so worktrees (phase 3) each get their own Current plan without extra work.
- Two plans created offline can share a Short ID (ADR 0002 section 8). Such a Short ID in a branch name is ambiguous and reported like an unknown one; a branch named with a longer prefix settles it.

## Notes from the implementation (#56)

- Only the **first** Short ID in the branch name counts, so a title that itself contains something like `bfc-xxxx` in the slug does not compete with it. It is resolved like any plan ID (`core.ResolveID`), which is how "a longer prefix settles it".
- `.git/beforecode/current` lives in the worktree's own git directory (`git rev-parse --absolute-git-dir`), not the common one, so in phase 3 every worktree also has its own `bfc use`, as it has its own HEAD.
- `bfc start` reuses the local branch whose first Short ID is the plan's; with several, it lists them and switches to none.
