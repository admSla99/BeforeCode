---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T20:49:57Z
agent: claude-code
task: T-3
done: true
---

tui.GrillQuestion has Options/Recommended (filled by tuiGrill.Snapshot). Grill screen: when an open Question has options it shows a list (recommended marked and selected, 'Write my own answer' last); up/down select, enter answers with the option text via answer(); enter on the custom item opens the answer line (typing=true), esc there returns to the list (BackToOptions). setChoice() resets per Question (choiceFor). Keys Up/Down/BackToOptions in keys.go. Tests: internal/tui/grill_options_test.go + 2 goldens; old goldens unchanged.
