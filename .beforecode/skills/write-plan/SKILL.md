---
name: write-plan
description: Write a plan's plan.md for agents from its Intent and its Grill, with verifiable acceptance criteria, and store it with bfc edit --stdin.
---

# Write plan.md

You turn a plan's Intent and the answers from its Grill into `plan.md`: the document agents implement and people approve. Work from the output of `bfc prime plan <id>`: this skill, then the plan, the `plan.md` template, the Intent and the Grill's Questions and answers. Run it first if you have not.

## Steps

1. **Read.** Read the Intent and every answered Question. The author's answers are decisions; an open Question is still undecided. Done when you can say, for each section of the template, what goes in it and which answer it comes from.
2. **Ask what is missing.** When a section cannot be written from the Intent and the answers, ask the author in the chat, and record the Question and answer like a Grill round (`bfc ask`, then `bfc reply --verbatim`). Done when no section rests on your guess.
3. **Write.** Fill in every section of the template, keeping its headings:
   - **Context**: what an agent must know before starting, including the decisions from the Grill.
   - **Approach**: how the change is made, and why this way.
   - **Affected areas**: the packages, files, APIs or data that change.
   - **Out of scope**: from the Intent and the Grill, stated plainly.
   - **Tasks**: units of work, in order.
   - **Acceptance criteria**: one item each, `- AC-1: <text>`, `- AC-2: <text>`, … Each is a single observable result that a test or a person can check: an input and the expected outcome, a number, a state. Together they cover the Intent's "how we know it worked" and every decision from the Grill that changes behaviour.
4. **Check it** with `bfc edit <id> --stdin --check`, the document on stdin:

   ```
   bfc edit <id> --stdin --check <<'PLAN'
   ## Context
   …
   PLAN
   ```

   It commits nothing and prints a `warning:` line for each problem. Fix each one and check again. Done when it prints `plan.md has no problems`.
5. **Store it once** with the same command without `--check`: `bfc edit <id> --stdin`. Done when it prints `saved plan.md`.
6. **Hand over.** Tell the author `plan.md` is ready, list the acceptance criteria, and name any open Questions. The author reviews it and submits it in the `bfc` TUI.

## When plan.md is already written

`bfc edit --stdin` stores `plan.md` only while it is still the template. When `plan.md` already has content, `bfc prime plan` shows it, and changing it is the author's call:

1. Ask the author whether to revise it, and wait for a yes. If the plan is approved, say that a new version makes its Approvals stale and returns it to review.
2. Build on that text. Keep every existing ID: `AC-3` stays `AC-3`, a removed criterion leaves a gap, and a new one takes the next unused number.
3. Check the revision with `--check` as above, then store it with `bfc edit <id> --stdin --force`.

Submitting, approving and the other Gates are for the author in the `bfc` TUI.
