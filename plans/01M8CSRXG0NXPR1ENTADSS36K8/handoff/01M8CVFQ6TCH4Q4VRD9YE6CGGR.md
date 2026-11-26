---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T21:16:49Z
agent: claude-code
task: T-1
done: true
---

Skill implement-task written, embedded and tested.

## Changed
- .beforecode/skills/implement-task/SKILL.md (new): 11 numbered steps with "Done when", The Handoff, failure rules, departing from plan.md, after the last Task.
- cmd/bfc/primeimplement.go: the implementSkill constant (T-2 adds the topic).
- cmd/bfc/skills_test.go: TestImplementTaskSkillIsBuiltIn; doctor expects implement-task in the skill list.
- Commit 39be9ab on bfc-nxpr-bfc-prime-implement-how-an-agent-works-one-task (not pushed yet: pushing the code branch was blocked by the permission mode; the user decides).

## Acceptance criteria
- AC-7, AC-8, AC-9 (headings), AC-10 (skill text): covered by TestImplementTaskSkillIsBuiltIn.
- AC-9 (bfc handoff takes a plain note): existing TestHandoffReleasesATask.
- AC-10 (Plan: line found by bfc check find): existing tests in check_test.go.
- AC-1 (embedded): TestEmbeddedSkillsMatchTheRepository; printing it is T-2.

## Tests
go build, go test ./... (665 passed), golangci-lint: all pass.

## Left
none

## Questions
none
