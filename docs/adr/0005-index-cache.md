# ADR 0005: The SQLite index as a cache of the plans ref

Date: 2026-11-17. Status: accepted. Made for issue #15 (index with full-text search).

## Decision

**Location.** `<git common dir>/beforecode/index.db` (`git rev-parse --path-format=absolute --git-common-dir`), so all worktrees of a clone share one index and it is never in the working tree. One file, rollback journal (no WAL).

**Behind the reading interface.** `index.Open(ctx, store)` gives `Plans` and `Search`; both first call `Update`, so the index never answers from an older state than the plans ref. `bfc list`, `bfc show`, the review commands and the TUI all read through `Plans`; nothing outside `internal/index` reads plans from the ref for display.

**What is stored.** One row per plan: the git tree ID of its directory, the header (Short ID, title, labels, author), the derived state, approvals, last activity, and the text of `intent.md` and `plan.md`. An FTS5 table `plan_text` shares the plan's rowid and holds the title, the body of `intent.md` and `plan.md`, without front matter and without HTML comments: the templates' `<!-- -->` hints are the same in every plan and would match everywhere. The `meta` row `ref_commit` is the plans ref commit the index was built from.

**Updating.** When the tip differs from `ref_commit`:

- If `ref_commit` is an ancestor of the tip, plans whose directory tree ID changed are read again, new ones are added and missing ones removed. Last activity comes from `git log ref_commit..tip`, taking the newest time per plan, and is merged with the stored one by `max`. This gives the same answer as a full read.
- Otherwise (first build, or a history that does not contain `ref_commit`) everything is read again.

The whole update runs in one `BEGIN IMMEDIATE` transaction with a busy timeout, so two `bfc` processes wait for each other. A stale writer cannot leave wrong data: it records its own older `ref_commit`, and the next update re-reads from there.

**Last activity is the newest commit time,** not the time of the first commit `git log` lists. They differ only with clock skew between machines; with `max` the incremental and the full update agree.

**Discard and rebuild.** `PRAGMA user_version` holds the schema version. A database of any other version, or one SQLite reports as `SQLITE_CORRUPT` or `SQLITE_NOTADB` (on open or on any later query), is deleted with its journal files and rebuilt, without a message. Any change to the schema or to the meaning of a column bumps the version; there are no migrations.

**Search.** Each word of the query is quoted as an FTS5 phrase, so the user's text is never FTS5 syntax; all words must match. Ranking is `bm25`, then newest activity. The snippet marks matches with `[` `]` and is folded to one line.

## Considered

- Tracking changes by `git diff --name-only` between commits: tree IDs are simpler and also catch a plan that changed and changed back.
- `mattn/go-sqlite3`: needs CGO; ruled out by the design (one static binary). FTS5 support of `modernc.org/sqlite` is checked in `docs/research/sqlite-fts5.md`.
