# ADR 0008: Agents use the `bfc` CLI, not MCP, and there is no daemon

Date: 2026-11-18. Status: accepted. Settled with the maintainer in the phase 2 grilling (#20), recorded for issue #55.

## Decision

**One interface for people, scripts and agents.** An agent works with plans through the same `bfc` commands a person types in a shell. Commands that agents use take `--json` and print a stable JSON shape, documented in `docs/design.md` (`bfc list --json`, `bfc show --json` and `bfc ask --json`). Any harness that can run a shell command can use `bfc` without an integration, which is what phase 4 needs.

**Instructions for agents come from the binary.** `bfc prime` prints Markdown for an agent: how to work with `bfc` (the commands an agent may use, that Gates are for people, that edits need an approved plan), then the Current plan (ADR 0009). Claude Code reaches it through one skill, `beforecode`, which `bfc setup claude` writes (ADR 0010): Claude Code loads the skill whenever the user talks about plans, and the skill only says to run `bfc prime` and follow it. The user does not run the CLI; the agent does, and people take the Gates in the TUI. Because the installed binary prints them, the instructions always name commands that exist in that version. A file such as `AGENTS.md` may point to `bfc prime`; it does not copy the rules.

**No daemon.** Every `bfc` command is a short process that ends when it has answered. Nothing holds state between commands:

- Plan state is derived from `events/` in the Plans ref (ADR 0002), so there is nothing to keep in memory.
- Lifecycle rules are checked by `core.Check*` when a command writes, so an agent cannot reach a state a person could not.
- Concurrent writers in one clone (a person in the TUI, one or more agents) are safe because every write ends with `git update-ref refs/beforecode/plans <new> <old>`: the second writer fails instead of losing a commit, and retries.

**MCP stays an option for phase 4.** If a harness cannot run shell commands but speaks MCP, a thin MCP server over the same commands can be added then. It is not part of phase 2, and `internal/mcp/` leaves the package layout.

`bfc` still starts agents itself, in the other direction: the Agent adapter runs Claude Code headless for the TUI Grill (issue #59). That adapter is not how agents read or write plans.

## Considered

- **An MCP server as the agent interface** (the phase 2 plan before the grilling). Rejected: it is a second interface that must be kept equal to the CLI, with its own schemas, tests and per-harness configuration. Claude Code permissions already match shell commands (`Bash(bfc approve:*)`), so the CLI can be allowed and denied command by command, which MCP tools would need again. Every harness in phase 4 has a shell; not every one has MCP.
- **A daemon** (a local server that holds the Index and serialises writes). Rejected: state already lives in the Plans ref and `update-ref <new> <old>` already serialises writes. A daemon adds start-up, a socket or port, stale state after a crash, and one more thing for `bfc doctor` to check.
- **A `SessionStart` hook that runs `bfc prime`, and slash commands `/bfc-grill` and `/bfc-plan`** (the first plan after the grilling). Rejected (#55, decision 9): the hook puts the plan into every session, related or not, and slash commands need the user to know them. A skill loads on demand from what the user says, and the `PreToolUse` hook's block message sends the agent to `bfc prime`.
- **Instructions written into the repository** by `bfc setup claude` (a block in `CLAUDE.md` or `AGENTS.md`). Rejected: the text goes stale when `bfc` is updated, and teammates with different versions would read rules for commands they do not have.

## Consequences

- The `--json` output of every command an agent uses is a public interface. Changing its shape needs the same care as the data format.
- Commands an agent uses must work without a terminal: no prompts, text on stdin where a body is needed (`bfc edit --stdin`, `bfc ask` with stdin).
- Handoffs (phase 3) become a CLI command, not an MCP tool.
- `bfc prime` must never fail a session start: without a Plans ref it prints how to fix it and exits 0.
