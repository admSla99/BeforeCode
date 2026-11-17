# ADR 0006: `bfc doctor`: checks, severity and safe fixes

Date: 2026-11-17. Status: accepted. Settled with the maintainer for issue #18.

## Decision

**One unit per check.** `internal/doctor` holds the checks in a fixed order. Each check has a `run`, which reports `ok`, `warn`, `FAIL` or `skip` with one line of detail, the items it lists, and for `FAIL` how to fix it. Some checks also have a safe `fix`. `doctor.Run` applies a fix only with `--fix`, then runs the check again. The command in `cmd/bfc` prints the results and exits 1 when any check is `FAIL`. A check that needs an earlier check to pass, or does not apply, is `skip`.

**The checks.**

| Check | Fails when | Fix shown | `--fix` |
| --- | --- | --- | --- |
| git version | no git, or older than 2.38 | install git 2.38 or newer | |
| repository | not inside a git working tree | `git init` or change directory | |
| config | `.beforecode/config.yaml` missing or invalid | `bfc init`, or correct the file | |
| plans ref | `refs/beforecode/plans` missing | `bfc bootstrap` when the remote has plans, else `bfc init` | |
| fetch refspec | `+refs/beforecode/*:refs/beforecode-remote/*` missing for the sync remote | `git config --add remote.<r>.fetch '…'` | adds it |
| remote | no such git remote, not reachable, or no plans ref there | `git remote add`, `git ls-remote`, `bfc push` or `bfc init` | |
| in sync | the local plans ref is behind the remote's or diverged from it | `bfc pull` | pulls |
| format-version | missing, unreadable, newer or older than this binary | update bfc, or no repair (see below) | |
| index | the database cannot be read: damaged, not SQLite, another schema version | `bfc doctor --fix` | rebuilds it |
| event files | an event file breaks ADR 0002, or cannot be read | no repair | |
| plans | a plan has no valid `created` event (an invalid plan) | no repair; `bfc new` | |
| skipped events | (warning) events that state derivation skips | | |
| short IDs | (warning) two plans with the same stored Short ID | | |

**Severity.** Skipped events and duplicate Short IDs are warnings. ADR 0002 produces both on purpose under concurrent work: an `approved` after a `changes-requested` is skipped, and two plans created offline can share a Short ID. Neither is wrong, so neither changes the exit code.

**The index passes while it is only behind.** A missing index, or one built from an older plans ref commit, is normal after a pull: the next read builds or updates it without a message (ADR 0005). Only a database that cannot be read fails. The check opens the database read-only (`mode=ro`), checks the schema version and runs `PRAGMA quick_check`. Running `bfc doctor` without `--fix` never changes the index.

**The remote is asked, not fetched.** The remote check and the in-sync check use one `git ls-remote`, bounded by 30 seconds. `bfc doctor` without `--fix` changes no ref. When the remote's commit is not in the local object store, the check cannot tell behind from diverged and says so. Then a `git fetch` (with the refspec) or `bfc doctor --fix` settles it.

**`--fix` pulls when behind or diverged.** A pull fast-forwards or makes a merge commit (ADR 0004), so the ref is never rewritten. On a conflict nothing is committed, the check stays `FAIL`, and the way out is `bfc pull --keep local|remote`. `--fix` never pushes: a clone that is ahead of the remote passes and is told that `bfc push` shares its commits.

**Local mode** (`sync.enabled: false`) skips the refspec, remote and in-sync checks. A missing `config.yaml` fails with `bfc init` as the fix. The remote checks then use `origin`, as the other commands do without a config file.

**Event file validation** (`core.ValidateEvent`) follows ADR 0002 sections 1, 3 and 4. The file name is an upper-case ULID with `.yaml`. The common fields must be present and `plan` equal to the directory. The type must be one of format version 1, and the reserved `claimed`, `checked` and `done` are not valid in it. `created` needs a `short_id` that is `bfc-` with 4 to 16 characters of the plan's ULID. `submitted`, `approved` and `changes-requested` need a `plan_blob` that is a git object ID. `changes-requested` needs a `message` ULID, and `closed` may have one. Unknown fields are allowed. Whether an event is allowed for its author is not checked here. State derivation decides that, and the skipped-events check reports it. A file whose object cannot be read is listed, and the run goes on.

**Problems in the plans ref have no automatic fix.** Events are append-only, and repairing a corrupted ref is out of scope for #18. For a broken event file, an invalid plan or a missing `format-version`, the fix line says that `bfc doctor` does not rewrite the ref and how to inspect the file.

## Considered

- **Fetching in the in-sync check.** That would tell behind from diverged in every case, but `bfc doctor` without `--fix` would move `refs/beforecode-remote/plans`. Rejected; `ls-remote` changes nothing.
- **Failing on an index that is behind the plans ref.** Rejected, because every `bfc pull` would leave `bfc doctor` failing until the next read.
- **Pulling only when behind**, as the issue first said. Diverged is the common case after two people work offline, and a merge is as safe as a fast-forward.
