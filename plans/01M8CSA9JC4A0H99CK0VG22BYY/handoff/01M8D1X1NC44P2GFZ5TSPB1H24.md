---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T23:08:57Z
agent: claude-code
task: T-3
done: true
---

update.Replace: in-place replacement through <target>.old, file mode kept, old binary restored on failure

## Changed
internal/update/replace.go (new): Replace(target, newBinary) -> replace(target, newBinary, rename). Removes a leftover <target>.old, copies the new binary to a temp file in target's directory with the old mode, renames target -> .old, temp -> target; on failure of the second rename puts .old back. .old is removed best-effort (Windows keeps it while the old exe runs). internal/update/replace_test.go (new). Commit on branch bfc-4a0h-bfc-update-and-bfc-version.

## Acceptance criteria
AC-8: covered by TestReplaceRestoresOldBinaryWhenFinalRenameFails.
AC-7: partly: TestReplaceLeavesTargetWhenMovingItAsideFails; the full flow is T-5.

## Tests
go build ./..., go test ./... (719 passed), golangci-lint run (no issues).

## Left
none. Next: T-4 (the bfc update command).

## Questions
none
