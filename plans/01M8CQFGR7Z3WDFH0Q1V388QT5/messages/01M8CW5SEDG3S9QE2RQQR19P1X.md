---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T21:28:52Z
agent: claude-code
to: [188631245+admSla99@users.noreply.github.com]
ref: plan.md#AC-8
---

Deviation from plan.md (Approach 3 and Affected areas), found in code review: the quiet tip read after a sync could record a tip that included an outside write the reload had not read, and Init read the tip concurrently with the first loads. Now the screens reload only after a tip read (also at the start and after a sync that brought changes), one read runs at a time and a read asked for meanwhile follows it; there is no quiet read. The Watcher is passed in AppOptions.Watcher, not as a tui.NewApp parameter; WatchInterval defaults to tui.DefaultWatchInterval (2 s); watch messages return from App.Update before forward instead of being dropped in forward. AC-1..AC-10 are unchanged. Keep plan.md as it is, or revise Approach 3 to match?
