---
title: "bfc update and bfc --version"
labels: []
---

## Problem

`bfc version` prints the installed version, but `bfc --version`, the flag most CLIs accept and people type first, fails with `unknown flag: --version`.

Updating `bfc` is manual: open the latest GitHub release, download the archive for the system, check it against `checksums.txt`, unpack it and replace the binary on the `PATH`. People and agents fall behind without noticing. That matters more and more, because the instructions agents follow (`bfc prime`), the skills and the data format all ship in the binary, so an old `bfc` gives old instructions and may refuse a ref a newer `bfc` migrated.

## Who it is for

- Developers who installed `bfc` from a GitHub release and want the newest one with one command.
- Agents and scripts that want to know the version (`bfc --version`, and `bfc version --json` if useful).

## How we know it worked

- `bfc --version` prints the same line as `bfc version` (`bfc v0.x.y`, `bfc dev` for a local build) and exits 0.
- `bfc update` finds the latest release of admSla99/BeforeCode, and when it is newer than the running binary, downloads the archive for the current OS and architecture, verifies it against `checksums.txt`, and replaces the running binary in place, on Linux, macOS and Windows (where a running `.exe` cannot simply be overwritten).
- When the installed version is already the latest, `bfc update` says so and changes nothing.
- `bfc update --check` only reports whether a newer version exists, without installing; exit code tells scripts the answer.
- A failed download or a checksum mismatch leaves the installed binary unchanged and says why.
- A `dev` build or a binary installed with `go install` is not overwritten blindly: `bfc update` says how that installation is updated instead (for example `go install …@latest`).
- `bfc update` refuses inside an agent, or at least asks a person: an agent replacing the tool it runs is a surprise.
- Unit tests run against a local fake release server; no network in tests.

## Questions for the Grill

- How to find the latest release: the redirect of `github.com/<repo>/releases/latest` (no API) or the GitHub REST API. ADR 0015 says `bfc` never calls the GitHub API (about pull requests); does that rule cover a release lookup too?
- Should `bfc` ever check for updates on its own (for example once a day, a one-line notice in the TUI or in `bfc doctor`), or only when asked? Any automatic check needs a way to turn it off and must never slow a command down.
- `bfc update <version>` to pin or roll back to a given release: needed now or later?
- Pre-releases: ignored unless asked for?
- Should `bfc version --json` exist for agents, and should `bfc doctor` warn when the binary is older than the format of the plans ref?

## Out of scope

- Package managers (Homebrew, Scoop, winget, apt); they update `bfc` their own way.
- Signing releases beyond the existing `checksums.txt`.
- Updating anything else than the `bfc` binary itself (skills and templates in the repository, `bfc setup` files, the plans ref).
