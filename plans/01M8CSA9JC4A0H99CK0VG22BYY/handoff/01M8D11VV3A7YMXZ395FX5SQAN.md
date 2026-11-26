---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T22:54:07Z
agent: claude-code
task: T-1
done: true
---

bfc --version prints the same line as bfc version; bfc version --json prints {"version":…}

## Changed
cmd/bfc/root.go: Version and the template "bfc {{.Version}}\n" on the Cobra root; --json flag on bfc version printing {"version":"<version>"} (compact, one line). cmd/bfc/root_test.go: two new tests. Commit 4d455ce on branch bfc-4a0h-bfc-update-and-bfc-version.

## Acceptance criteria
AC-1: covered by TestVersionFlagPrintsSameLineAsVersion (v0.1.0 and dev).
AC-2: covered by TestVersionJSON.

## Tests
go build ./..., go test ./... (706 passed), golangci-lint run (no issues).

## Left
none. Next: T-2 (internal/update Latest and Download).

## Questions
none
