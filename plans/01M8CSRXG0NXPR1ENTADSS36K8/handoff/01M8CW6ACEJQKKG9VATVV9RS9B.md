---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T21:29:10Z
agent: claude-code
task: T-2
done: true
---

bfc prime implement [id] [T-n] prints the skill, the chosen Task, its commands, the criteria, plan.md and the Task's Handoffs.

## Changed
- cmd/bfc/primeimplement.go: runPrimeImplement, chooseTask (named, held by the git author, else as bfc claim), renderImplementTopic with notes, taskHandoffs as the fitDoc tail so only plan.md is cut.
- cmd/bfc/prime.go: topic "implement", bfc prime takes up to three arguments.
- cmd/bfc/primeimplement_test.go and testdata/TestPrimeImplementOfAGivenTask.golden; prime_test.go expects implement among the known topics.
- Commit on the plan branch, not pushed (pushing the code branch waits on the user).

## Acceptance criteria
- AC-1: TestPrimeImplementCommand (repository copy wins), TestPrimeImplementOfAGivenTask (no front matter).
- AC-2: TestPrimeImplementOfAGivenTask.
- AC-3: TestPrimeImplementChoosesTheTask, TestPrimeImplementCommand.
- AC-4: TestPrimeImplementCommand.
- AC-5: TestPrimeImplementNotes.
- AC-6: TestPrimeImplementStaysWithin8kB.
- AC-11 (unknown-topic error lists implement): TestPrimeCommandRefusesAnUnknownTopic.

## Tests
go build, go test ./... (670 passed), golangci-lint: all pass.

## Left
none

## Questions
none
