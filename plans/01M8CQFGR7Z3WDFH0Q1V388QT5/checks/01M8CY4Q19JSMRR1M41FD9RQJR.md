---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T22:03:14Z
agent: claude-code
plan: 01M8CQFGR7Z3WDFH0Q1V388QT5
pr: 147
head_sha: 0b31e08b498c7d6f77ff1360166fe970ae864ef6
base_sha: d4043ba9ef8d5afdd4c3e6675904ac51e30af356
plan_blob: d6cba8c0c03ceac321f38bf9117870578c7146ed
criteria:
  AC-1: met
  AC-2: met
  AC-3: met
  AC-4: met
  AC-5: met
  AC-6: met
  AC-7: met
  AC-8: met
  AC-9: met
  AC-10: cannot-verify
out_of_scope: []
---

9 met, 0 not met, 1 cannot verify; no out-of-scope changes.

| Criterion | Status | Reason |
|---|---|---|
| AC-1 | met | `TestAppShowsAPlanWrittenOutside` moves the fake Watcher's tip and adds a plan; the list shows it without a key press (`App.tipRead` reloads on a new tip). |
| AC-2 | met | `TestAppShowsPlanMdWrittenOutsideAndKeepsTheScroll` checks the new plan.md and the first line after scrolling; `TestAppShowsATaskChangedOutside` shows a new Task and Handoff on the open tasks tab. |
| AC-3 | met | `TestAppGrillShowsAMessageWrittenOutsideAndKeepsTheAnswer` shows the new Message and keeps the typed "Half typed". |
| AC-4 | met | `TestInboxShowsAQuestionWrittenOutside` waits for "Questions (2)" and the new Question's text. |
| AC-5 | met | The local subtest of `TestAppShowsAPlanWrittenOutside`; `Init` starts the watch tick whether or not sync runs. |
| AC-6 | met | `TestAppDoesNotReloadWhileTheTipStays` counts no Load/Inbox/Detail call over 4 tip reads; `tuiWatcher.Tip` is one `rev-parse`, the Index is touched only by loads. |
| AC-7 | met | `TestAppKeepsTheSelectedPlanWhenOneAppearsAbove` and `TestInboxKeepsTheSelectedItemWhenOneAppearsAbove` through `keepSelection`. |
| AC-8 | met | `TestAppSyncAndWatcherReloadOnce` runs a real sync that moves the tip and counts exactly one reload (2 loads); after the sync the app asks for a tip read instead of reloading twice. |
| AC-9 | met | `TestTUIWatcherReadsTheTipOfThePlansRef`: the tip of `refs/beforecode/plans`, a new tip after `bfc new` and after a separate `git update-ref`, and "" without an error with no Plans ref. |
| AC-10 | cannot-verify | The diff cannot show the test and lint runs; CI Lint passed, the Test jobs (with `-race` on Linux and macOS) were still running. No golden file changed. |

No out-of-scope changes. Departures from plan.md (no quiet read; the Watcher in `AppOptions`; watch messages returning before `forward`) were recorded as a Question and the author kept plan.md as it is.
