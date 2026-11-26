---
title: "Acknowledge a Finding in the TUI"
labels: []
---

## Problem

The plan author sees a Finding of a plan check in the Inbox of `bfc`, but cannot act on it there. The Inbox only moves the selection, opens the plan detail (`enter`) and switches to the plan list (`tab`); the plan detail has no key for it either. Accepting a difference on purpose exists only as the CLI command `bfc acknowledge <check-ULID> -m <reason>`, which the user must type themselves, with a check ULID they have to copy from somewhere, because it refuses to run inside an agent.

Everywhere else, the user is told that they work in the TUI and do not run the CLI, and that Gates and `acknowledge` are taken there. For `acknowledge` that is not true today: the agent can only tell the user to run a command by hand (seen in bfc-nxpr, PR #146, check 01M8CY3VP1Q467N8T15FP2FXRW).

## Who it is for

- Plan authors who get a Finding in their Inbox and decide that the difference is fine.
- Agents, which must be able to send the user to the TUI for `acknowledge`, as they do for the Gates, instead of handing them a CLI command.

## How we know it worked

- From a Finding in the Inbox (or the plan's Checks in the plan detail), the plan author can acknowledge that check in the TUI, writing the reason there, without leaving `bfc` or knowing the check's ULID.
- The result is the same Acknowledgement `bfc acknowledge` writes today (a Message with `ref: checks/<ULID>`), written by the same code, and synced like other local changes.
- Afterwards the Finding no longer waits in the Inbox, in this clone and, after sync, in others.
- The action is shown only to people who may take it now, and never offered when there is nothing unacknowledged.
- `bfc prime` and the docs tell the agent to send the user to the TUI for `acknowledge`.

## Out of scope

- Changing what an Acknowledgement is or its data format.
- Letting an agent acknowledge.
- Writing anything to the pull request on GitHub.
- Fixing the code or the plan from the TUI in answer to a Finding.
