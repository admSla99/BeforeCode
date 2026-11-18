# ADR 0010: Files written by `bfc setup <harness>` live on normal branches

Date: 2026-11-18. Status: accepted. Settled with the maintainer in the phase 2 grilling (#20), recorded for issue #55. Widens ADR 0001.

## Decision

`bfc setup <harness>` writes the project files a harness reads. For Claude Code (`bfc setup claude`) these are `.claude/settings.json` (the `SessionStart` and `PreToolUse` hooks, `permissions.allow` for the commands agents use and `permissions.deny` for the Gates) and `.claude/commands/` (the slash commands `/bfc-grill` and `/bfc-plan`).

These files are committed to normal branches and changed through pull requests, like `.beforecode/config.yaml`. `bfc setup` merges into an existing `.claude/settings.json` and keeps every other key, hook and permission; running it twice changes nothing. It never commits and never touches the Plans ref; it tells the user to commit `.claude/` through a pull request.

**Why on branches.** A hook that only applies to some teammates is no control. Committed project settings reach everyone who runs Claude Code in the repository, and a change to them is reviewed like a change to the approval policy.

This widens the rule of ADR 0001. On normal branches live `.beforecode/config.yaml`, `.beforecode/skills/`, `.beforecode/templates/`, and the files written by `bfc setup <harness>`. Plans, Events, Messages, handoffs and checks stay in the Plans ref only.

## Considered

- **`.claude/settings.local.json`.** Rejected: it is per user and not committed, so each teammate would opt in.
- **The user's `~/.claude/settings.json`.** Rejected: per machine, and it would apply the hook to every repository of that user, including ones without `bfc`.
- **Harness files in the Plans ref.** Rejected: the harness reads them from the working tree, not from a hidden ref.

## Consequences

- A teammate who runs Claude Code in the repository without `bfc` on `PATH` gets a hook that cannot run. `bfc doctor` warns about this (issue #61).
- Turning the hook off is a pull request too: `claude.enforce: false` in `.beforecode/config.yaml` (config version 3, issue #59).
- `bfc setup` for other harnesses (phase 4) follows the same rule for their project files.
