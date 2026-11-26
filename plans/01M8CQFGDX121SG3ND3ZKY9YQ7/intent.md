---
title: "Grill: answer a Question by picking an option or writing your own"
labels: []
---

## Problem

In the Grill screen of the TUI the agent asks a Question and the author must type the whole answer by hand. Most Grill Questions have a few likely answers that the agent already knows (for example "A, B or C?"), so typing them is slow and invites typos. Claude Code itself asks such questions with a list of options and a last choice for your own text; the Grill should feel the same.

## Who it is for

Plan authors who answer the Grill in the `bfc` TUI.

## How we know it worked

- When the agent asks a Grill Question with suggested answers, the Grill screen shows them as a list: ↑↓ select, enter answers with the selected option.
- The last item of the list is always a custom answer: choosing it opens the text line, as today.
- A Question without suggested answers works as today: only the text line.
- The stored answer is the author's words, recorded like `bfc reply --verbatim`, so `bfc show` and the plan's Discussion read the same whether the answer was picked or typed.
- The options also reach agents and scripts that read the Question with `bfc show --json`, and `bfc ask` can write them, so the Grill agent sets them through the CLI as it does today (ADR 0008).

## Out of scope

- Choosing several options at once (multi-select).
- Options for Questions outside the Grill in the TUI (such as the discussion tab of bfc-mmst); they can follow once the format exists.
- Changing how the agent decides what to ask.
