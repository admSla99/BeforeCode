---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T20:42:29Z
agent: claude-code
task: T-2
done: true
---

bfc ask has --option (StringArray, no comma split, trimmed) and --recommend, checked with core.CheckOptions before writing. messageJSON has options ([] when none) and recommended (null when none), used by ask, show (open_questions, messages) and wait. bfc show lists options under the Question in the Discussion, '(recommended)' marked. Tests in cmd/bfc/options_test.go. Docs for the JSON fields wait for T-7.
