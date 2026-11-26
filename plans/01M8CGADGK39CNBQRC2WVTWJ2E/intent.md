---
title: "bfc setup codex: rules for Codex in AGENTS.md"
labels: [phase-4]
---

## Problem

Only Claude Code can work with BeforeCode today: `bfc setup claude` installs the skill, the edit hook and the permissions. Codex has no plugin or skill to point it at `bfc`, so it does not know that plans exist, that it should run `bfc prime`, or that Gates are for people. Phase 4 (#22) needs a second harness to implement the same plan without changing it.

## Who it is for

Developers on a team that uses BeforeCode who run Codex (or another harness that reads `AGENTS.md`) instead of Claude Code.

## How we know it worked

- `bfc setup codex` writes a marked BeforeCode section to `AGENTS.md` at the repository root that sends the agent to `bfc prime`; text outside the section is kept, and a second run changes nothing.
- Codex started in a repository set up this way runs `bfc prime` when asked about a plan, claims a Task with `bfc claim` and leaves a Handoff, without the user explaining bfc.
- `bfc doctor` reports a missing or outdated BeforeCode section in `AGENTS.md` when Codex is set up.

## Out of scope

- An edit-blocking hook for Codex (Codex has no PreToolUse hook equivalent yet); the rule is only written down.
- Adapters for OpenCode, Gemini CLI and ACP, and the Build screen.
- An `internal/agent/codex` adapter that runs Codex headless.
