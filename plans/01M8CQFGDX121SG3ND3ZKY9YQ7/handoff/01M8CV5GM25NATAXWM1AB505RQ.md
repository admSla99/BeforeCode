---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T21:11:15Z
agent: claude-code
task: T-5
done: true
---

Grill screen in rounds: GrillQuestion.Round (adapter fills it from core.GrillRounds), GrillTurn.Round and Answers []GrillAnswer replace Answer. open() is the first open Question of the last round; header 'round r/3 · Qi/n'. After an answer (answered()): next open of the round, else phaseLimit when round 3 is done, else TurnAsk for round+1 with the round's answers. Enter in phaseLimit starts nothing; f finishes. Tests: internal/tui/grill_rounds_test.go; fake records NUL-separated rounds. Prompt text of the round (number, limit, rules) is T-6.
