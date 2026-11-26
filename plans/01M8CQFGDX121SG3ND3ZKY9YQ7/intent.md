---
title: "Grill: answer a Question by picking an option or writing your own"
labels: []
---

## Problem

In the Grill screen of the TUI the agent asks a Question and the author must type the whole answer by hand. Most Grill Questions have a few likely answers that the agent already knows (for example "A, B or C?"), so typing them is slow and invites typos. The agent also asks one Question per turn, so a Grill is a long drip of turns, each waiting on Claude Code.

The Grill should work like the `grill-me` / `grilling` skill of mattpocock/skills: the agent asks in rounds, each round is every Question it can ask now (the frontier), and each Question comes with options and the agent's recommended answer. Claude Code itself asks such questions with a list of options and a last choice for your own text; the Grill should feel the same.

## Who it is for

Plan authors who answer the Grill in the `bfc` TUI.

## How we know it worked

- In one turn the agent asks a whole round: every Question whose prerequisites are settled, numbered, each recorded with `bfc ask`.
- When a Grill Question has suggested answers, the Grill screen shows them as a list with the AI's recommended answer marked and selected: ↑↓ select, enter answers with the selected option.
- The last item of the list is always a custom answer: choosing it opens the text line, as today.
- A Question without suggested answers works as today: only the text line.
- The next round starts when every Question of the round is answered. A Grill has at most 3 rounds; after the third, the screen goes on to finishing the Grill.
- The stored answer is the author's words, recorded like `bfc reply --verbatim`, so `bfc show` and the plan's Discussion read the same whether the answer was picked or typed.
- The options and the recommendation also reach agents and scripts that read the Question with `bfc show --json`, and `bfc ask` can write them, so the Grill agent sets them through the CLI as it does today (ADR 0008).
- A Grill in Claude Code without the TUI (`bfc prime grill`) asks the same way: rounds, options, a recommendation, at most 3 rounds.

## Out of scope

- Choosing several options at once (multi-select).
- Options for Questions outside the Grill in the TUI (such as the discussion tab of bfc-mmst); they can follow once the format exists.
- Sub-agents that look up facts while the author answers; the agent looks them up itself before it asks.
