## Context

The root model `tui.App` (`internal/tui/app.go`) reloads its screens only on a `syncDoneMsg` with `Changed` set, that is when background sync pulled something from the remote. A write by another process on the same clone (Claude Code running `bfc ask`, `bfc edit`, `bfc claim`, `bfc handoff`, `bfc check record`, …, or the CLI in another terminal) moves the local Plans ref but the TUI never notices. In local mode the sync timer is not even running, so nothing is ever reloaded.

What exists already:

- `store.Store.Tip(ctx)` returns the commit the Plans ref points to with one `git rev-parse` (`ErrNotInitialised` when there is no ref).
- The Index is rebuilt from the ref inside `withIndex` when it is stale, so a reload after the tip moved reads the new state; a reload is not needed while the tip is the same.
- Reload paths per screen: `Inbox.load`, `PlanList.load`, `PlanDetailScreen.load`, `reloadGrillMsg` for the Grill. The detail keeps its tab and scroll offset across a reload (`refresh` keeps `YOffset`). The Grill keeps the answer being typed, and already says "The open Question was answered elsewhere." when that happens.
- The plan list and the Inbox keep their cursor as an index (`min(cursor, len-1)`), so a new plan or a new Inbox item above the selection moves the selection to a different plan.

Constraints: no daemon, no server (ADR 0008); `tui` imports `core` only, never `store`; all I/O runs in a `tea.Cmd`; tests run in parallel with the fake adapters and golden files (see CLAUDE.md, TUI conventions).

## Approach

Watch the tip of the local Plans ref by polling, in the app, next to sync:

1. `tui` defines a small interface `Watcher` with `Tip(ctx) (string, error)`. `cmd/bfc` implements it on `store.Store.Tip` (`tuiWatcher` in `tui.go`); tests use a fake whose tip the test moves. A missing ref is an empty tip, not an error.
2. `App` keeps the last tip it saw. A `watchTickMsg` timer, every `AppOptions.WatchInterval` (2 seconds by default, shorter in tests), runs `Tip` in a `tea.Cmd` and returns `watchDoneMsg{tip}`. When the tip differs from the last one, the app stores it and reloads the screens through one shared `reloadScreens` helper (Inbox, list, detail if open, Grill if open), the same set `syncDoneMsg` reloads today. When the tip is the same, nothing else runs: no reload, no Index rebuild. An error from `Tip` is ignored until the next tick. The timer runs in local mode too, independent of the sync timer.
3. The first tip is read at `Init`, so the start does not reload twice. When a sync brought changes, the app reloads the screens as today and also reads the tip without reloading (`watchDoneMsg` with `quiet` set), so the next watch tick on that tip does nothing. Writes by the TUI itself (new plan, edit, review, Grill answer) move the tip too; the watcher reloads once for them, which is harmless.
4. The plan list and the Inbox keep the selection by Short ID (and, in the Inbox, by item kind) across a load: after a load the cursor goes to the row of the same item when it is still there, otherwise it is clamped as today.

Why polling the tip: one `git rev-parse` every 2 seconds is cheap even on Windows, works with packed refs, worktrees and every git version, and needs no file-system watcher dependency or platform code. A file-system watcher (fsnotify on `.git/refs`, `packed-refs`) would react faster but misses packed-ref updates and worktree layouts; it can come later behind the same `Watcher` interface.

## Affected areas

- `internal/tui/app.go`: `Watcher`, `AppOptions.WatchInterval`, `watchTickMsg`, `watchDoneMsg`, the last tip, `reloadScreens` shared with `syncDoneMsg`; `forward` drops the watch messages.
- `internal/tui/planlist.go`, `internal/tui/inbox.go`: keep the selection by Short ID across a load.
- `internal/tui/app_test.go`, `planlist_test.go`, `inbox_test.go`: a fake `Watcher`; tests and golden files for the new behaviour.
- `cmd/bfc/tui.go`: `tuiWatcher` on `store.Store.Tip`, passed to `tui.NewApp`; `tuiOptions` sets the interval.
- `e2e/`: one end-to-end check is optional; the behaviour is covered at the `tui` level.
- `docs/design.md` (TUI section): one sentence that the TUI shows local writes by other processes within a few seconds.

## Out of scope

- Changes in other clones that have not been pushed; they arrive by sync as today.
- Watching the working tree, the index or the normal branches.
- A daemon, a server or a file-system watcher (ADR 0008).
- Changing the sync interval, the sync state model or the status bar text.
- Live update of the Grill's agent turn (it already streams its own events).

## Tasks

- T-1: Add the `Watcher` interface and the watch timer to `tui.App`: read the tip at `Init`, poll every `WatchInterval`, and reload the open screens through a `reloadScreens` helper shared with `syncDoneMsg` only when the tip changed; run it in local mode too. Fake `Watcher` and app tests.
- T-2: Keep the selection by Short ID across a load in the plan list and the Inbox, with tests where a new plan or item appears above the selected one.
- T-3: Implement `tuiWatcher` in `cmd/bfc/tui.go` on `store.Store.Tip`, wire it and the interval into `runTUI`, and add the sentence to `docs/design.md`.

## Acceptance criteria

- AC-1: With the TUI open on the plan list, a test that moves the fake Watcher's tip and adds a plan to the fake Plans sees the new plan on the screen within one watch interval, without a key press.
- AC-2: With the plan detail open, a new version of `plan.md`, a new Task state (claimed, completed) or a new Handoff written outside the TUI appears on the open tab within one watch interval; the tab and the scroll offset stay as they were.
- AC-3: With the Grill open, a Message written outside the TUI appears within one watch interval, and an answer being typed in the input line is still there afterwards (unless the open Question was answered elsewhere, which keeps today's notice).
- AC-4: The Inbox shows a new item that waits on the user (for example a Question addressed to them) within one watch interval after it is written outside the TUI.
- AC-5: In local mode (`AppOptions.Local`), AC-1 holds: the watcher runs although no sync runs.
- AC-6: When the tip does not change, the watch ticks call `Watcher.Tip` only: the fake Plans records no `Load`, `Inbox` or `Detail` call after the first load.
- AC-7: In the plan list and the Inbox, when a new item appears above the selected one, the same plan stays selected after the reload.
- AC-8: A sync that brought changes reloads the screens as today, and the next watch tick on the same tip causes no second reload.
- AC-9: In `cmd/bfc`, `tuiWatcher.Tip` returns the commit of `refs/beforecode/plans` in a temporary repository, a new commit after `bfc new` run as another process, and an empty tip without an error when the ref does not exist.
- AC-10: `go test ./...` (TUI tests with `-race`) and `golangci-lint run` pass; golden files changed by this plan were reviewed by eye.
