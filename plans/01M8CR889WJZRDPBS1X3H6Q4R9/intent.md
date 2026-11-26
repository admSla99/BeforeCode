---
title: "Grill: finishing never leaves an unanswered Question that blocks the plan"
labels: []
---

## Problem

In the Grill screen of the TUI the agent asks a Question and waits for the answer. When the author does not answer and finishes the Grill (`ctrl+f` while the answer line is open), the summary is written but the Question stays open. An open Question blocks approval (`core.CheckOpenQuestions`), and the only way to close it is an answer with `bfc reply`, which the user does not run, or reopening the Grill, which again sits on the same Question. The author asked to stop, yet the plan ends up blocked by a Question nobody needs answered.

## Who it is for

Plan authors who grill their plans in the `bfc` TUI and decide they have answered enough.

## How we know it worked

- Finishing the Grill while a Question waits for an answer does not leave that Question blocking the plan: after the finish the plan can be submitted and approved as if the Question had not been asked.
- The Question is not silently lost: the Discussion and `bfc show` still list it, marked as skipped by the author when the Grill finished, so an agent writing `plan.md` knows it was not decided.
- Before the Grill finishes with an open Question, the screen says so and the author confirms; Esc returns to the Question.
- Opening the Grill again after it finished does not get stuck on the skipped Question.
- A plan that is already blocked this way (an open Grill Question from before this change) can be unblocked from the TUI without the CLI.
- Questions outside the Grill (from reviewers or agents during implementation) still block approval until answered, as today.

## Out of scope

- Withdrawing or skipping Questions in general, outside the Grill.
- Answering Questions in the plan detail (bfc-mmst) and options for answers (bfc-121s).
- Changing what blocks approval for non-Grill Questions.
