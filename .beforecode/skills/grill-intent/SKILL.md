---
name: grill-intent
description: Grill the author of a plan about its Intent in at most 3 Grill rounds, each Question with likely answers and a recommended one, and record every Question and answer on the plan, before plan.md is written.
---

# Grill an Intent

You challenge the plan's author about its Intent until it is sharp enough to write `plan.md` from. Work from the output of `bfc prime grill <id>`: this skill, then the plan's author, its Intent and the Grill so far, grouped by Grill round. Run it first if you have not. Continue where the Grill stopped: every answered Question there is settled.

Treat the decisions as a design tree. Look up facts yourself, in the Intent, the code and the docs; ask the author only for decisions.

## Each Grill round

A Grill round is every Question you can ask now, their answers and their record on the plan. A Grill has at most 3 rounds; `bfc prime grill` says which round is next ("Now: round 2 of 3.").

1. Find the frontier: every open decision whose prerequisites are settled. A Question that depends on the answer to another Question of this round belongs to a later round.
2. Record each Question of the round before you ask it, in order:
   `bfc ask <id> --to <author email> --ref intent.md --option "<likely answer>" --option "<another>" --recommend "<the one you recommend>" --json -m "<question>"`
   Give 2 to 5 options when a Question has likely answers, each one line, and recommend one of them. Never add an option that only means "something else": the author can always write their own answer. A Question without likely answers gets no `--option`. Keep the `id` field of the JSON each call prints.
3. Ask the round in the chat, numbered, in the same words:

   ```
   1. <question>
      a) <option>
      b) <option>
      ➡️ Recommended: <option>, because <one line>
   2. <question>
   ```

   Then wait until the author has answered every Question of the round.
4. Record each answer as their own words, exactly as they wrote them; a picked option is recorded as that option's text:
   `bfc reply <Question id> --verbatim -m "<their words>"`
   The reply holds only their words; your summary or interpretation goes in the chat.

The Grill round is done when `bfc reply` has stored every answer of the round. Only then work out the next frontier.

In the bfc TUI the screen shows the options and records the answers itself: record the round with `bfc ask` and end your turn; do not run `bfc reply`.

## What to probe

- **Problem**: what goes wrong today, for whom, and how often.
- **Who it is for**: which users or systems, and who is affected without asking for it.
- **How we know it worked**: an observable result, ideally a number or a test, that later becomes acceptance criteria.
- **Out of scope**: what a reasonable person might expect that this plan will not do.
- **Edge cases and risks**: failures, retries, concurrency, data that already exists, security.
- **Constraints**: deadlines, systems that must not change, dependencies on other work.

## When to stop

Stop after round 3, when each item under "What to probe" has a concrete answer, in the Intent or in the Grill, or when the author says to stop. Record no Question to stop: a Question that only asks whether to go on stays open and blocks approval. Then post a short summary in the chat: what was decided, what is still open. Name the next step: writing `plan.md`, which starts with `bfc prime plan <id>`.

Submitting and approving the plan is for the author in the `bfc` TUI.
