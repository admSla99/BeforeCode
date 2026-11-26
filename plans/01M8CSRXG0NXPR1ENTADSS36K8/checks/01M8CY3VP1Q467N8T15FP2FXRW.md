---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T22:02:46Z
agent: claude-code
plan: 01M8CSRXG0NXPR1ENTADSS36K8
pr: 146
head_sha: 34ec5a77f6fe63f003bed7c1e00449b5d9d3bb04
base_sha: d4043ba9ef8d5afdd4c3e6675904ac51e30af356
plan_blob: e9351e12328b02104c0db3e81a7dbe4142cf4842
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
  AC-10: met
  AC-11: met
  AC-12: met
out_of_scope:
  - Step 2 of `.beforecode/skills/implement-task/SKILL.md` also runs `git pull` on the code branch, which the Approach (only `bfc pull`) does not call for.
---

12 of 12 criteria met, 1 out-of-scope change.

| Criterion | Status | Reason |
|---|---|---|
| AC-1 | met | `implement-task` is embedded through `.beforecode/skills`; `TestPrimeImplementOfAGivenTask` shows it first without front matter, `TestPrimeImplementCommand` shows the repository copy winning. |
| AC-2 | met | `TestPrimeImplementOfAGivenTask` + golden: Short ID, title, state, Task text/state/holder, every AC, plan.md, only T-2's Handoffs oldest first. |
| AC-3 | met | `chooseTask` and `TestPrimeImplementChoosesTheTask`: held Task, then first open, then first Paused, with the rule printed. |
| AC-4 | met | `TestPrimeImplementCommand`: no Current plan note with `bfc start` advice, `t-2` names a Task of the Current plan, three arguments fail. |
| AC-5 | met | `TestPrimeImplementNotes`: draft, unknown Task (lists Tasks), no Tasks, all completed (points to "After the last Task"); notes return nil. |
| AC-6 | met | Handoffs are the `fitDoc` tail; `TestPrimeImplementStaysWithin8kB` keeps skill, Task, ACs, Handoffs and cuts plan.md with the note. |
| AC-7 | met | The skill's steps 1 to 11 in the required order, each with "Done when", checked by `TestImplementTaskSkillIsBuiltIn`. |
| AC-8 | met | Section "When a check fails or a decision is missing" and step 11 (one Task per turn), checked by the same test. |
| AC-9 | met | Section "The Handoff" with the summary line and five headings; `TestHandoffReleasesATask` already shows a plain note is accepted. |
| AC-10 | met | Section "After the last Task": offer, `gh pr create` on yes with `Plan: <Short ID>`, never merge or take a Gate; existing `bfc check find` tests read that line. |
| AC-11 | met | `primeInstructions`, both `beforecode` skill copies (identical) and the unknown-topic error list `implement`; `TestImplementIsInTheInstructions`, `TestPrimeCommandRefusesAnUnknownTopic`, golden of `bfc setup claude`. |
| AC-12 | met | `docs/design.md` Prime section describes `bfc prime implement` right after `bfc prime check`. |

Out-of-scope changes:

- `.beforecode/skills/implement-task/SKILL.md`: step 2 also runs `git pull` on the code branch, which the Approach (only `bfc pull`) does not call for.
