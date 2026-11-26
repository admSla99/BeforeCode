---
title: "TUI: the Discussion in the plan detail"
labels: [phase-3, tui]
---

## Problem

The Discussion of a plan (Threads, replies, Questions to people or to `ai`) exists only in the CLI: `bfc show` prints it and `bfc ask` / `bfc reply` write it. People who review plans in the TUI cannot see what was asked, which Questions are still open, or answer them without leaving the TUI. Issue #115, stage 2; the layouts were prototyped in stage 1.

## Who it is for

Plan authors and reviewers who work in the `bfc` TUI, as the design intends: people use the TUI, agents use the CLI.

## How we know it worked

- In the plan detail a person sees every Thread with its replies, author (and harness), time, `to`, `ref` and whether it is open or answered, as `bfc show` shows them; a `ref` whose heading or AC is gone is marked stale.
- From the TUI a person starts a Thread on the plan, a heading or an AC, asks a Question to a person or to `ai`, and replies to a Message; `bfc show` then shows the same Messages as if they were written with `bfc ask` / `bfc reply`.
- Messages that a sync brings appear with a notice, and the open Questions count on the detail updates.
- The screen fits 80x24 and follows the TUI conventions in `CLAUDE.md`.

## Out of scope

- The AI answering Questions addressed to `ai` (#116).
- Editing or deleting Messages (they are append-only).
