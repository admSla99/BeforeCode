---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T21:40:02Z
agent: claude-code
task: T-3
done: true
---

bfc prime, the beforecode skill (both copies) and docs/design.md point to bfc prime implement.

## Changed
- cmd/bfc/prime.go: the command list names `bfc prime implement [id] [T-n]`.
- internal/agent/claude/SKILL.md and .claude/skills/beforecode/SKILL.md (identical): the description and the list name it.
- docs/design.md: the Prime section describes the topic after bfc prime check.
- Golden files of bfc prime and bfc setup claude regenerated; TestImplementIsInTheInstructions added.
- Commit on the plan branch, not pushed (pushing the code branch waits on the user).

## Acceptance criteria
- AC-11: TestImplementIsInTheInstructions, TestPrimeCommandRefusesAnUnknownTopic; bfc doctor reports "claude skill: .claude/skills/beforecode/SKILL.md matches this bfc".
- AC-12: docs/design.md, by reading.

## Tests
go build, go test ./... (671 passed), golangci-lint: all pass.

## Left
none; every Task is completed, the pull request waits on the user's yes.

## Questions
none
