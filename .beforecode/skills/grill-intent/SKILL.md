---
name: grill-intent
description: Grill the author of a plan about its Intent, one Question per Grill round, and record every Question and answer on the plan, before plan.md is written.
---

# Grill an Intent

You challenge the plan's author about its Intent until it is sharp enough to write `plan.md` from. Work from the output of `bfc prime grill <id>`: this skill, then the plan's author, its Intent and the Grill so far. Run it first if you have not. Continue where the Grill stopped: every answered Question there is settled.

## Each Grill round

A Grill round is one Question, its answer and their record on the plan.

1. Pick the weakest point of the Intent and the answers so far: a vague word, a missing number, an unstated assumption, a case nobody decided, a goal that cannot be checked.
2. Record the Question before you ask it:
   `bfc ask <id> --to <author email> --ref intent.md --json -m "<question>"`
   Keep the `id` field of the JSON it prints.
3. Ask the author that one Question in the chat, in the same words, and wait for their answer.
4. Record the answer as their own words, exactly as they wrote them:
   `bfc reply <Question id> --verbatim -m "<their words>"`
   The reply holds only their words; your summary or interpretation goes in the chat.

The Grill round is done when `bfc reply` has stored the answer.

## What to probe

- **Problem**: what goes wrong today, for whom, and how often.
- **Who it is for**: which users or systems, and who is affected without asking for it.
- **How we know it worked**: an observable result, ideally a number or a test, that later becomes acceptance criteria.
- **Out of scope**: what a reasonable person might expect that this plan will not do.
- **Edge cases and risks**: failures, retries, concurrency, data that already exists, security.
- **Constraints**: deadlines, systems that must not change, dependencies on other work.

## When to stop

Stop when each item under "What to probe" has a concrete answer, in the Intent or in the Grill, or when the author says to stop. Record no Question to stop: a Question that only asks whether to go on stays open and blocks approval. Then post a short summary in the chat: what was decided, what is still open. Name the next step: writing `plan.md`, which starts with `bfc prime plan <id>`.

Submitting and approving the plan is for the author in the `bfc` TUI.
