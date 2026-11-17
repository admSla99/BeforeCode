# Minimum git version

**Short answer: git 2.38.0.** Only `git merge-tree --write-tree` sets this floor. Every other plumbing command BeforeCode uses was already in git 1.8.0 (2012) or 2.0.0 (2014).

Sources are git's own release notes (`Documentation/RelNotes/`), man pages and source code at release tags in github.com/git/git. Checked on 2026-11-17.

## Commands and features

| Command / feature | Available since | Source |
|---|---|---|
| `git merge-tree --write-tree <c1> <c2>` (new mode: writes the merged tree, reports conflicts) | **2.38.0** | https://github.com/git/git/blob/master/Documentation/RelNotes/2.38.0.adoc (line 16), https://github.com/git/git/blob/v2.38.0/Documentation/git-merge-tree.txt |
| `merge-tree` flags `-z`, `--name-only`, `--[no-]messages`, `--allow-unrelated-histories`; exit 0 = clean, 1 = conflicts, other = error | 2.38.0 | https://github.com/git/git/blob/v2.38.0/Documentation/git-merge-tree.txt |
| `update-index --cacheinfo <mode>,<sha>,<path>` (comma syntax) | 2.0.0 (1.9.x has only the three-argument form) | https://github.com/git/git/blob/v2.0.0/Documentation/git-update-index.txt vs https://github.com/git/git/blob/v1.9.5/Documentation/git-update-index.txt |
| `update-index --index-info` | 1.8.0 or earlier | https://github.com/git/git/blob/v1.8.0/Documentation/git-update-index.txt |
| `hash-object -w --stdin` | 1.8.0 or earlier | https://github.com/git/git/blob/v1.8.0/Documentation/git-hash-object.txt |
| `GIT_INDEX_FILE`, `read-tree`, `write-tree` | 1.8.0 or earlier | https://github.com/git/git/blob/v1.8.0/Documentation/git.txt, https://github.com/git/git/blob/v1.8.0/Documentation/git-read-tree.txt |
| `commit-tree <tree> -p <parent> -m <msg>` | 1.8.0 or earlier (the `-p … <tree>` order fix is in 1.7.11.4) | https://github.com/git/git/blob/v1.8.0/Documentation/git-commit-tree.txt, https://github.com/git/git/blob/master/Documentation/RelNotes/1.7.11.4.adoc |
| `update-ref <ref> <new> <old>` with 40 `0`s or an empty string as `<old>`, meaning "the ref must not exist yet" | 1.8.0 or earlier | https://github.com/git/git/blob/v1.8.0/Documentation/git-update-ref.txt (lines 21-25) |
| `cat-file -p` / `cat-file <type>`, `ls-tree` | 1.8.0 or earlier | https://github.com/git/git/blob/v1.8.0/Documentation/git-cat-file.txt |
| `rev-parse --verify --quiet <ref>^{commit}` | 1.8.0 or earlier | https://github.com/git/git/blob/v1.8.0/Documentation/git-rev-parse.txt, https://github.com/git/git/blob/v1.8.0/Documentation/revisions.txt (line 112) |
| `rev-parse --path-format=absolute --git-common-dir` (index location, ADR 0005) | 2.31.0 | https://github.com/git/git/blob/master/Documentation/RelNotes/2.31.0.adoc |
| `merge-base --is-ancestor` (index update, ADR 0005) | 1.8.0 | https://github.com/git/git/blob/v1.8.0/Documentation/git-merge-base.txt |

"1.8.0 or earlier" means the feature is documented at the v1.8.0 tag. We did not look for the exact version that added it, because it does not affect the minimum.

## Later changes to `merge-tree`

| Version | Change | Source |
|---|---|---|
| 2.38.2 | Fixed a segfault when running in a read-only repository. | https://github.com/git/git/blob/master/Documentation/RelNotes/2.38.2.adoc |
| 2.39.0 | Added `--stdin` (batch mode, which implies `-z` and adds a merge-status field to each result; with `--stdin` the exit code is 0 for both clean and conflicted merges). The man page also began to document the structured `-z` message format (`<count>NUL<paths…>NUL<conflict-type>NUL<message>NUL`). The 2.38.0 source already emits that format: `merge_display_update_messages(..., detailed)` in `merge-ort.c`. | https://github.com/git/git/blob/master/Documentation/RelNotes/2.39.0.adoc, https://github.com/git/git/blob/v2.39.0/Documentation/git-merge-tree.txt, https://github.com/git/git/blob/v2.38.0/merge-ort.c |
| 2.40.0 | Added `--merge-base=<commit>`. It cannot be combined with `--stdin`. | https://github.com/git/git/blob/master/Documentation/RelNotes/2.40.0.adoc, https://github.com/git/git/blob/v2.40.0/Documentation/git-merge-tree.txt |
| 2.41.0 | Reads basic configuration (for example, a setting that disables replace refs). | https://github.com/git/git/blob/master/Documentation/RelNotes/2.41.0.adoc |
| 2.43.0 | Added `-X <option>` / `--strategy-option`. It is in the source from 2.43.0 but missing from the man page until 2.46.0. | https://github.com/git/git/blob/master/Documentation/RelNotes/2.43.0.adoc, https://github.com/git/git/blob/v2.43.0/builtin/merge-tree.c |
| 2.45.0 | With `--merge-base`, the three inputs may be trees instead of commits. | https://github.com/git/git/blob/master/Documentation/RelNotes/2.45.0.adoc |
| 2.49.0 | Fixed a deadlock in `--stdin` mode. | https://github.com/git/git/blob/master/Documentation/RelNotes/2.49.0.adoc |
| 2.50.0 | Added `--quiet`: reports only the exit status, stops at the first conflict and writes no objects. | https://github.com/git/git/blob/master/Documentation/RelNotes/2.50.0.adoc, https://github.com/git/git/blob/v2.50.0/Documentation/git-merge-tree.adoc |

The default (non-`--stdin`) output did not change in any later release. It is the tree OID, then conflicted-file info, then messages, with exit status 0, 1 or other. In the current man page, `--write-tree` is still optional in the synopsis, and the old trivial-merge mode is marked deprecated: https://github.com/git/git/blob/master/Documentation/git-merge-tree.adoc

## Implications for BeforeCode

- The sync design merges our ref tip with the remote ref tip. Both are commits, so the auto-computed merge base in 2.38.0 is enough. Requiring 2.40 would only make sense if we needed `--merge-base`.
- Run plain `git merge-tree --write-tree -z <ours> <theirs>`. Read the result from the exit code, not by parsing message text. Message strings are not stable across versions; only `<conflict-type>` is described as stable.
- Pass an empty string as `<old>` to `update-ref` for "must not exist". It works with both SHA-1 and SHA-256 repositories. Forty zeros is a SHA-1-length value.
- Check `git version` at startup and fail clearly below 2.38.0. Consider recommending 2.38.2 or later because of the read-only-repository segfault fix.
