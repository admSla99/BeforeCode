---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T23:04:50Z
agent: claude-code
task: T-2
done: true
---

internal/update: Latest (tag from the releases/latest redirect) and Download (checksums.txt, SHA-256 check, extract bfc/bfc.exe)

## Changed
internal/update/update.go (new): DefaultBase, ErrNoRelease, ArchiveName, Latest(ctx, base), Download(ctx, base, tag, goos, goarch, dir). Latest uses a client that does not follow redirects, needs a 3xx whose Location ends in /releases/tag/<semver>, and rejects a pre-release tag. Download fetches checksums.txt first (404 -> ErrNoRelease naming the tag), then the archive; mismatch error is "checksum mismatch for <file>: expected <sha>, got <sha>". HTTP timeout 5 min, size limit 512 MiB.
internal/update/updatetest/server.go (new): fake release server (updatetest.New(t, latest, releases...)), fails the test on any path other than /releases/latest and /releases/download/…; Requests() and Downloaded() for T-4 (AC-4, AC-11); Archive() builds GoReleaser-like archives, for T-5.
go.mod: golang.org/x/mod direct. Commit ea86d38.

## Acceptance criteria
AC-6: partly: unknown release -> ErrNoRelease naming the version (TestDownloadUnknownRelease); the command part is T-4.
AC-7: partly: missing asset, missing checksum line, mismatch with expected and actual SHA-256 (TestDownloadFailures); "binary unchanged" is T-3/T-5.
AC-11: partly: updatetest fails on other paths; TestLatestReadsTagFromRedirect checks only /releases/latest is requested.
AC-14: partly: Latest refuses a pre-release (TestLatestRefusesPreRelease).

## Tests
go build ./..., go test ./... (714 passed), golangci-lint run (no issues).

## Left
none. Next: T-3 (update.Replace).

## Questions
none
