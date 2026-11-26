---
title: "TUI: show changes made outside it without a restart"
labels: []
---

## Problem

When `bfc` is open and Claude Code (or the CLI in another terminal) creates a plan, writes `plan.md`, asks a Question, claims a Task or leaves a Handoff, the change is written to the local plans ref but the TUI does not show it. The TUI reloads its screens only when background sync brings changes from the remote; a write by another process on the same clone is not noticed, and in local mode nothing is reloaded at all. The user has to quit and start `bfc` again to see what the agent just did.

## Who it is for

Developers who keep `bfc` open next to Claude Code (or another agent) and watch the plans, Questions and Tasks change while the agent works.

## How we know it worked

- With `bfc` open, a change written by another process to the local plans ref (a new plan, a new version of intent.md or plan.md, a Message, an Event such as a Claim, Handoff or state change, a plan check) appears in the open screen (Inbox, plan list, plan detail, Grill) within a few seconds, without a key press and without a restart.
- It works in local mode (no remote) as well as with a remote.
- The screen keeps its place: the selected plan, the open tab and the scroll position stay where they were, and a text being typed is not lost.
- Checking for changes is cheap: when nothing changed, the TUI does not reload the screens or rebuild the cache.
- A change that sync brings from the remote still reloads the screens as today.

## Out of scope

- Changes in other clones that have not been pushed; they arrive by sync as today.
- Watching the working tree or the normal branches.
- A daemon or a server (ADR 0008).
