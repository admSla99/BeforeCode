---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T21:11:42Z
agent: claude-code
task: T-3
done: true
---

tuiWatcher in cmd/bfc/tui.go on store.Store.Tip, wired into runTUI with a 2 s WatchInterval; test TestTUIWatcherReadsTheTipOfThePlansRef; sentence in docs/design.md. All tasks done; go test ./... and golangci-lint pass (no -race locally: cgo off).
