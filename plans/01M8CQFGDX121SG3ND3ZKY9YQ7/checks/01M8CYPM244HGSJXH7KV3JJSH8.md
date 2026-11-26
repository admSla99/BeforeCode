---
author:
  name: admSla99
  email: 188631245+admSla99@users.noreply.github.com
at: 2026-11-26T22:13:01Z
agent: claude-code
plan: 01M8CQFGDX121SG3ND3ZKY9YQ7
pr: 145
head_sha: 0ea12c0927cbe4669b6b9b68d823f13a7e754952
base_sha: d4043ba9ef8d5afdd4c3e6675904ac51e30af356
plan_blob: 69aaf1701f449c4ba13bd007590d2bfcd8f324eb
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
  AC-13: met
  AC-14: met
  AC-15: met
  AC-16: met
out_of_scope: []
---

16 met, 0 not-met, 0 cannot-verify; no out-of-scope changes.

| Criterion | Status | Reason |
| --- | --- | --- |
| AC-1 | met | TestAskWritesOptions checks `options: [A, 'B, with comma']` and `recommended: A` in the front matter and reads them back in order. |
| AC-2 | met | TestAskRefusesInvalidOptions covers every refusal and checks the plans ref is unchanged. |
| AC-3 | met | TestMessageJSONHasOptions checks ask, show (open_questions, messages) and wait; empty is `[]` and `null`. |
| AC-4 | met | TestMessageWithoutOptions parses an old file; ParseMessage stays a non-strict yaml.Unmarshal, so old binaries ignore the fields. |
| AC-5 | met | TestGrillShowsOptions golden; enter stores the recommended option's text, ↑ then enter the other one; the adapter stores it verbatim. |
| AC-6 | met | TestGrillCustomAnswer: custom item opens the answer line, esc returns to the list without storing, typed text is stored. |
| AC-7 | met | Existing Grill goldens change only in the counters and one hint line; a Question without options shows only the answer line. |
| AC-8 | met | Goldens cover the option list, long options cut (TestGrillCutsLongOptions), the answer line of a Question with options (TestGrillAnswerLineWithOptions) and a picked option being saved (TestGrillSavingAPickedOption). |
| AC-9 | met | A picked option is stored as plain text; TestShowListsOptions shows it as a plain body in the Discussion. |
| AC-10 | met | ADR 0017 added, ADR 0012 links to it, ADR 0002 section 7, design.md and CONTEXT.md updated. |
| AC-11 | met | TestGrillRounds: [Q1 Q2 Q3] then the next round, other Messages ignored, late answers stay in their round. |
| AC-12 | met | Round tests show `round 1/3 · Q1/3`, Q2 without a turn, and the round-2 turn gets all three answers in its prompt. |
| AC-13 | met | TestGrillStopsAfterTheLastRound: no turn after round 3, limit message, `f` and `w` work. |
| AC-14 | met | TestGrillResumesARoundAndDropsAnsweredElsewhere shows Q2/3 on reopening and Q3/3 after sync; the adapter fills rounds. |
| AC-15 | met | TestGrillTurnPromptNamesTheRound checks the round 1 and round 3 prompts; grill-intent stops after round 3. |
| AC-16 | met | The prime grill golden shows rounds of 3, options with the recommended one, and the current round. |

No out-of-scope changes: `bfc prime plan` again lists the Questions as on main (its goldens are unchanged from the base).
