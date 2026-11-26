## Context

`bfc version` prints `bfc <version>` (`cmd/bfc/root.go`); `main.version` is `dev` unless GoReleaser sets it to the tag with `-ldflags -X main.version={{ .Tag }}` (`.goreleaser.yaml`). `bfc --version` fails with `unknown flag`. Releases of admSla99/BeforeCode carry one archive per OS and architecture, named `bfc_<version without v>_<os>_<arch>.tar.gz` (`.zip` on Windows), each holding `bfc` (`bfc.exe`), `LICENSE` and `README.md`, and a `checksums.txt` with SHA-256 sums. Tags such as `v0.2.0-rc.1` are pre-releases.

Updating is manual today, and an old `bfc` gives agents old instructions (`bfc prime`) and refuses a plans ref whose `format-version` is newer than `core.FormatVersion`. `bfc doctor` already reports that case (`internal/doctor/plans.go`, `checkFormatVersion`), but its fix names only `go install …@latest`.

Decisions from the Grill:

- The latest release is found through the redirect of `https://github.com/admSla99/BeforeCode/releases/latest` to `/releases/tag/<tag>`, and assets are downloaded from `/releases/download/<tag>/<file>`. No GitHub API, so ADR 0015's rule that `bfc` never calls the GitHub API stays true without exceptions.
- `bfc` checks for a newer version only when asked: `bfc update` and `bfc update --check`. No background lookup, nothing in the TUI.
- `bfc update <version>` installs that exact release, also an older one (roll back).
- Pre-releases are ignored: `releases/latest` never points to one. One is installed only when named explicitly as `<version>`.
- `bfc version --json` exists for agents.
- `bfc doctor` reports now, without network, when the plans ref has a newer `format-version` than this binary, and points to `bfc update`.
- Inside an agent, installing refuses like a Gate; `bfc update --check` works for agents and scripts.
- `bfc update --check` exits 0 when the binary is the latest release, 1 when a newer one exists, 2 when the lookup fails or the version cannot be compared.

Rules from `CLAUDE.md` that apply: no network in unit tests (a local `httptest` server stands in for GitHub), tests in `cmd/bfc` run in parallel with `bfcIn` and `setEnv`, and `bfc update` never touches the working tree, the index, the current branch or the plans ref.

## Approach

**`--version` and `version --json`.** Set `root.Version` and the version template `bfc {{.Version}}\n` on the Cobra root, so `bfc --version` prints exactly what `bfc version` prints. `bfc version --json` prints `{"version":"<version>"}`.

**A new package `internal/update`** does the release work with the standard library (`net/http`, `archive/tar`, `archive/zip`, `compress/gzip`, `crypto/sha256`) and compares versions with `golang.org/x/mod/semver`. It takes the base URL as a parameter: the default is `https://github.com/admSla99/BeforeCode`, and the variable `BFC_RELEASES_URL` overrides it (for tests and mirrors), read through `lookupEnv` so tests set it with `setEnv`. It offers:

- `Latest(ctx, base)`: request `<base>/releases/latest` without following the redirect and read the tag from the `Location` header (`…/releases/tag/<tag>`). A missing or malformed redirect is an error.
- `Download(ctx, base, tag, goos, goarch, dir)`: fetch `checksums.txt` and the archive for the platform into a temporary directory, verify the archive's SHA-256 against its line in `checksums.txt`, and extract only `bfc` or `bfc.exe`. A missing asset, a missing checksum line or a mismatch is an error, and nothing else happens. The HTTP client has a timeout; a 404 for a named `<version>` says that release does not exist.
- `Replace(target, newBinary)`: replace the binary in place on all three systems. Write the new binary next to the target (same directory, so renames stay on one filesystem), copy the target's file mode, rename the target to `<target>.old`, rename the new file to the target, and on any failure rename `.old` back. Windows allows renaming a running `.exe` but not overwriting or deleting it, so `.old` is removed best-effort; a later `bfc update` removes a leftover `<target>.old` first.

**The command `bfc update [<version>] [--check] [--json]`** in `cmd/bfc/update.go`:

1. When `main.version` is `dev`, the binary was built from source or with `go install` (GoReleaser is the only build that sets the version). `bfc update` changes nothing, says so, prints `go install github.com/admSla99/BeforeCode/cmd/bfc@latest` and exits non-zero; `--check` prints the same and exits 2.
2. Without `--check`, refuse inside an agent with the same rule as the Gates (`checkPerson`), before any network request.
3. Find the target version: `<version>` (with or without the leading `v`), else `Latest`.
4. `--check`: print the current and the latest version (`--json`: `{"current":…,"latest":…,"newer":true|false}`) and exit 0 or 1 through `exitCodeError`; lookup errors exit 2.
5. Install: when the target equals the running version, print `bfc <version> is already installed` and change nothing. Otherwise `Download`, then `Replace` on `os.Executable()` with symlinks resolved, and print `updated bfc <old> -> <new>`. An older `<version>` is installed too (roll back), and the output says it is older.

**Doctor and messages.** `checkFormatVersion` keeps its failure for a newer `format-version`, because every other command refuses such a ref; its fix becomes `bfc update` (with the `go install` line for source builds). The existing `…; update bfc` errors in `cmd/bfc/plans.go`, `bootstrap.go`, `migrate.go` and `internal/config/config.go` end with `; run bfc update` instead.

**Why this way.** The redirect needs no token, has no API rate limit and keeps ADR 0015 absolute. The standard library is enough for download, checksum and archives, so no self-update library enters the binary. `Replace` is tested in `internal/update` on files in a temporary directory, and the whole command in `e2e` with a built `bfc` and a fake release server, which covers the real in-place replacement without network.

## Affected areas

- `cmd/bfc/root.go`: `--version` on the root, `--json` on `bfc version`.
- `cmd/bfc/update.go` (new) and `cmd/bfc/update_test.go`: the `bfc update` command.
- `internal/update/` (new): `Latest`, `Download`, `Replace` and their tests with `httptest`.
- `internal/doctor/plans.go`: fix text of the newer `format-version` result; its tests.
- `cmd/bfc/plans.go`, `cmd/bfc/bootstrap.go`, `cmd/bfc/migrate.go`, `internal/config/config.go`: the "update bfc" messages.
- `e2e/`: an end-to-end test that builds `bfc` as `v0.1.0`, serves a fake release `v0.2.0` locally and runs `bfc update`.
- `go.mod`: `golang.org/x/mod`.
- `README.md` (install and update), `docs/design.md` (package layout, update command), `CLAUDE.md` (package layout), a new `docs/adr/0018-self-update.md`.

## Out of scope

- Package managers (Homebrew, Scoop, winget, apt).
- Signing releases beyond the existing `checksums.txt`.
- Updating anything but the `bfc` binary: skills and templates in the repository, `bfc setup` files, the plans ref.
- Any automatic or periodic update check, in the TUI, `bfc doctor` or elsewhere; `bfc doctor` makes no network request for this plan.
- A `--pre` flag, listing releases, or the GitHub REST API.
- Updating a `dev` or `go install` build.
- Changes to the release workflow or `.goreleaser.yaml`.

## Tasks

- T-1: Make `bfc --version` print the same line as `bfc version` and add `bfc version --json` (`{"version":…}`), with tests in `cmd/bfc`.
- T-2: Add `internal/update` with `Latest` (the tag from the redirect of `<base>/releases/latest`) and `Download` (archive and `checksums.txt` for an OS and architecture, SHA-256 check, extraction of `bfc` or `bfc.exe` from `.tar.gz` or `.zip`), tested against an `httptest` release server, including a missing redirect, a missing asset, a missing checksum line and a mismatch.
- T-3: Add `update.Replace`: replace a binary in place through `<target>.old` and a rename in the same directory, keep the file mode, restore the old binary on failure, remove a leftover `.old`; tested on files in a temporary directory.
- T-4: Add the `bfc update [<version>] [--check] [--json]` command: `dev` builds refused with the `go install` hint, installing refused inside an agent, `--check` with exit codes 0, 1 and 2, "already installed", roll back to an older `<version>`, the `BFC_RELEASES_URL` override; tests in `cmd/bfc` with the fake server and `setEnv` (the installation itself is covered by T-5, since a test cannot replace the test binary).
- T-5: Add an e2e test: build `bfc` with `-X main.version=v0.1.0`, build a second `bfc` as `v0.2.0` and serve it as a release archive with `checksums.txt` from a local server, run `bfc update` and check that `bfc --version` then prints `bfc v0.2.0`; a second case with a wrong checksum leaves `v0.1.0` in place.
- T-6: Point `bfc doctor`'s fix for a newer `format-version` and the "update bfc" errors to `bfc update`; update tests and golden output.
- T-7: Document it: `README.md` (update with `bfc update`), `docs/design.md` and `CLAUDE.md` (the `internal/update` package and the command), and ADR 0018 recording the redirect lookup without the GitHub API, the `BFC_RELEASES_URL` override and the in-place replacement.

## Acceptance criteria

- AC-1: `bfc --version` prints the same line as `bfc version` (`bfc v0.1.0` for a release build, `bfc dev` for a local build) and exits 0.
- AC-2: `bfc version --json` prints `{"version":"v0.1.0"}` for the release build `v0.1.0` and exits 0.
- AC-3: With a fake release server whose `/releases/latest` redirects to `v0.2.0`, `bfc update` run by the release build `v0.1.0` downloads the archive for the current OS and architecture, verifies it against `checksums.txt`, replaces the running binary and prints `updated bfc v0.1.0 -> v0.2.0`; afterwards `bfc --version` prints `bfc v0.2.0`. The e2e test runs on Linux, macOS and Windows in CI.
- AC-4: When the latest release equals the running version, `bfc update` prints that `bfc v0.2.0` is already installed, downloads no archive and leaves the binary unchanged.
- AC-5: `bfc update --check` installs nothing and exits 0 when the binary is the latest release, 1 when a newer release exists and 2 when the lookup fails; it prints the current and the latest version, and with `--json` prints `{"current":…,"latest":…,"newer":…}`.
- AC-6: `bfc update v0.1.5` installs exactly release `v0.1.5`, also when it is older than the running version, and says it is older; a version with no release fails with a message naming it and leaves the binary unchanged.
- AC-7: A failed download, a missing checksum line or a checksum mismatch leaves the installed binary unchanged, exits non-zero and says why (for a mismatch: the expected and the actual SHA-256).
- AC-8: When the final rename of `update.Replace` fails, the old binary is back at its path.
- AC-9: A `dev` build running `bfc update` changes nothing, prints `go install github.com/admSla99/BeforeCode/cmd/bfc@latest` and exits non-zero; `bfc update --check` prints the same and exits 2.
- AC-10: With `CLAUDECODE` set, `bfc update` refuses before any network request and exits non-zero; `bfc update --check` still works.
- AC-11: `bfc update` requests only `/releases/latest` and `/releases/download/…` under the base URL; the fake server in the tests fails on any other path, and the code contains no `api.github.com`.
- AC-12: No command other than `bfc update` makes a network request to look for a release, including the TUI and `bfc doctor`.
- AC-13: When the plans ref has a `format-version` newer than `core.FormatVersion`, `bfc doctor` reports it with a fix naming `bfc update`, and the errors of other commands for that ref end with `run bfc update`.
- AC-14: `bfc update` without a version never installs a pre-release; `bfc update v0.3.0-rc.1` installs that pre-release.
- AC-15: `go test ./...` passes without network access, and `golangci-lint run` reports nothing.
