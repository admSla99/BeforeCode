---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T23:17:12Z
agent: claude-code
task: T-4
done: true
---

bfc update [<version>] [--check] [--json]: dev hint, agent refusal, --check exit 0/1/2, already installed, roll back, BFC_RELEASES_URL

## Changed
cmd/bfc/update.go (new): newUpdateCmd(version), registered in root.go. Order: dev build -> error with the go install hint (exit 1, --check exit 2); without --check checkPerson before any request; <version> normalized to a leading v and validated; base = BFC_RELEASES_URL (lookupEnv) else update.DefaultBase; target = <version> else update.Latest. --check prints "installed/latest" lines or JSON {current,latest,newer}, exit 1 (silent error) when newer, 2 on lookup failure. Install: "bfc X is already installed" when equal; else Download into a temp dir, Replace on executable(cmd), print "updated bfc A -> B" plus "(B is older than A)" on roll back; --json {current,latest,newer,installed}.
cmd/bfc/workdir.go: place.executable and executable(cmd) (os.Executable with symlinks resolved), so tests replace a fake binary instead of the test binary. The package is imported as bfcupdate because prime_test.go has a flag var named update.
cmd/bfc/update_test.go (new). Commit 23d3720.

## Acceptance criteria
AC-3: covered at command level by TestUpdateInstallsLatest (fake binary); the real binary is T-5.
AC-4: TestUpdateAlreadyInstalled. AC-5: TestUpdateCheck, TestUpdateCheckJSON. AC-6: TestUpdateRollsBackToNamedVersion, TestUpdateUnknownVersion. AC-7: TestUpdateChecksumFailures. AC-9: TestUpdateRefusesDevBuild. AC-10: TestUpdateInsideAgent. AC-11: fake server fails on other paths; TestNoGitHubAPI scans the .go files. AC-14: TestUpdateInstallsNamedPreRelease (+ Latest refuses a pre-release, T-2).

## Tests
go build ./... (also CGO_ENABLED=0), go test ./... (735 passed), golangci-lint run (no issues).

## Left
none. Next: T-5 (e2e). Note: plan.md, not the Grill answers, was implemented: no --force for dev builds (plan.md out of scope) and no warning after a roll back below the ref's format-version.

## Questions
none
