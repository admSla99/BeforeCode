---
title: "TUI: the Discussion in the plan detail, with Messages on sections"
labels: [phase-3]
---

## Problem

The Discussion of a plan (Threads, Questions and replies) can only be read with `bfc show` and joined with `bfc ask` / `bfc reply`. The user works in the TUI and does not run the CLI, so in the plan detail they see no Messages, cannot answer a Question that waits on them and cannot ask about a section or an acceptance criterion while they review the plan. Part of #21, issue #115.

## Who it is for

People who write and review plans in the TUI: the plan author answering Questions from reviewers and agents, and reviewers who want to ask about a heading or an AC before they approve.

## How we know it worked

- The plan detail shows the Discussion: every Thread in the order of its first Message, replies indented under the Message they reply to, with author (and the harness when `agent` is set), time, `to`, `ref` and open or answered, as `bfc show` does. A `ref` whose heading or AC no longer exists is shown as stale.
- From the TUI the user starts a Thread on the plan, a heading or an AC, asks a Question (`to` people or `ai`) and replies to a Message, written with the same code as `bfc ask` / `bfc reply`.
- Messages that background sync brings appear with a notice, and the open Questions count on the detail updates.
- The screen follows the TUI conventions in `CLAUDE.md`: fits 80x24, one key map, golden files with `teatest/v2`.

## Layout

The prototype in issue #115 (branch `prototype-115-discussion`) compared a `discussion` tab, a side pane and markers in the document. The maintainer chose the `discussion` tab (A), with a picker of headings and ACs when a Thread is started.

## Out of scope

- The AI answering Questions sent `to: ai` (#116).
- Markers in the rendered document and starting a Thread from the cursor line (possible later, layout C).

