# ADR 0004: Sync of the plans ref: merge, retry and conflicts

Date: 2026-11-17. Status: accepted. Settled with the maintainer for issue #12 (`bfc push`, `bfc pull`, `bfc sync`).

## Decision

Sync lives in `internal/store` (`Store.Pull`, `Store.Push`, `Store.Sync`) and prints nothing. The CLI reports its results; the TUI background sync (#17) calls the same functions.

**Pull.** `git fetch --prune <remote> +refs/beforecode/*:refs/beforecode-remote/*`, with the refspec on the command line so pull works whether or not `.git/config` has it. Then, comparing the local plans ref (ours) with `refs/beforecode-remote/plans` (theirs):

| Situation | Result |
| --- | --- |
| Remote has no plans ref | Nothing changes |
| No local plans ref | The local ref is set to theirs |
| Theirs is an ancestor of ours | Up to date (local commits stay for the next push) |
| Ours is an ancestor of theirs | Fast-forward, no merge commit |
| Diverged, or unrelated histories | `git merge-tree --write-tree --allow-unrelated-histories`, then a two-parent `commit-tree` (ours first) with the message `bfc: merge <remote>` |

The local ref moves with `update-ref <ref> <new> <old>`, so a concurrent local writer makes pull fail instead of losing a commit. The working tree, index and branch are never touched.

**Unrelated histories are merged**, not refused. Two clones that both ran `bfc init` before either pushed have unrelated root commits; both hold the same `format-version` and otherwise only added files, so the merge is clean.

**Conflicts.** Exit code 1 of `merge-tree` means a conflict; the conflicting paths come from `-z --name-only --no-messages`, never from message text. Nothing is committed, the local ref is unchanged, and a `ConflictError` lists, for each path, the blob of each side (empty for a side that deleted it) and the two commits. The CLI prints `git show <commit>:<path>` for both versions. Because events, messages, handoffs and checks are new files named by ULID, only `intent.md` and `plan.md` can conflict in practice.

**Resolution without an interactive UI.** `bfc pull --keep local|remote` (also on `bfc sync`) merges as usual and, for the conflicting files only, takes the chosen side's version (or removes the file if that side deleted it). The merge commit message names the side and the files. The user then brings in what they need from the other version with `bfc edit` and pushes. `merge-tree -X ours|theirs` was not used: it needs git 2.43 and resolves per hunk, not per file.

**Push.** `git push --porcelain <remote> refs/beforecode/plans:refs/beforecode/plans`: never `--force`, `--force-with-lease`, `--mirror` or a `+` refspec. A test runs every push path through a logging git wrapper and fails on any of them. A line with flag `!` and summary `[rejected]` (GitHub says `fetch first`, a plain bare remote `non-fast-forward`; ADR 0001) means the remote has commits we lack: pull without `--keep`, then try again. Any other failure, such as `[remote rejected]`, network or authentication, is returned as a `RemoteError` carrying git's own standard error.

**Bounded retries: 5 attempts.** Each round costs a fetch and a push; two writers who race resolve in the second attempt, so five only fail when the remote changes continuously. Push then returns `ErrPushRetriesExhausted` and the user runs it again. A conflict met during a retry stops push at once, with nothing pushed.

**Sync** is pull, then push. **Remote:** `--remote`, else `sync.remote` from `.beforecode/config.yaml`, else `origin` when there is no config file. In local mode (`sync.enabled: false`) push, pull and sync refuse unless `--remote` is given.

## Consequences

- A merge commit records every concurrent write, which keeps the history of the ref an honest audit trail at the cost of some extra commits.
- `--keep` resolves whole files. Combining two edits of the same document is a manual `bfc edit` after the merge; an interactive resolution UI can come later on top of `ConflictError`.
- git may prompt for credentials on push or fetch, as plain `git push` would. The TUI background sync will need to avoid that (#17).
