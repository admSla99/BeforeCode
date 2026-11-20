---
name: grill-intent
description: Grill the author of a plan about its Intent, one question per round, and record every Question and answer on the plan, before plan.md is written.
---

# Grill an Intent

You challenge the plan's author about its Intent until it is sharp enough to write `plan.md` from. The Intent, the author and the Grill so far are printed below this skill by `bfc prime grill <id>`. Continue where the Grill stopped: every answered Question there is settled.

## Each round

A round is one question, its answer and their record on the plan.

1. Pick the weakest point of the Intent and the answers so far: a vague word, a missing number, an unstated assumption, a case nobody decided, a goal that cannot be checked.
2. Record the Question before you ask it:
   `bfc ask <id> --to <author email> --ref intent.md --json -m "<question>"`
   Keep the ULID it prints.
3. Ask the author that one question in the chat, in the same words, and wait for their answer.
4. Record the answer as their own words, exactly as they wrote them:
   `bfc reply <ULID> --verbatim -m "<their words>"`
   Your summary or interpretation goes in the chat, never into the reply.

A round is done when `bfc reply` has stored the answer.

## What to probe

- **Problem**: what goes wrong today, for whom, and how often.
- **Who it is for**: which users or systems, and who is affected without asking for it.
- **How we know it worked**: an observable result, ideally a number or a test, that later becomes acceptance criteria.
- **Out of scope**: what a reasonable person might expect that this plan will not do.
- **Edge cases and risks**: failures, retries, concurrency, data that already exists, security.
- **Constraints**: deadlines, systems that must not change, dependencies on other work.

## When to stop

Stop when each section above has a concrete answer, either in the Intent or in the Grill, or when the author says to stop. Then post a short summary in the chat: what was decided, what is still open. Name the next step: writing `plan.md`, which starts with `bfc prime plan <id>`.

Submitting and approving the plan is for the author in the `bfc` TUI.
