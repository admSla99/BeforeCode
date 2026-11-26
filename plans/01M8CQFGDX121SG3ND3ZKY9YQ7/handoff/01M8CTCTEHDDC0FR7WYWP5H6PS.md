---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T20:57:46Z
agent: claude-code
task: T-4
done: true
---

internal/core/grill.go: MaxGrillRounds=3, IsGrillQuestion (moved from cmd/bfc), GrillQuestion/GrillRound, GrillRounds(msgs, author) and CurrentGrillRound(rounds) (> Max means limit reached). Rule as implemented: a Grill Question starts a new round when the first answer to an earlier Grill Question was written after the previous Grill Question (the plan's wording read literally would put every later Question in its own round). bfc prime grill (and prime plan) print '#### Round i of 3' groups with options, then 'Now: round r of 3...' or the limit line, then '#### Other Questions'. Next: T-5 uses core.GrillRounds in tuiGrill.Snapshot.
