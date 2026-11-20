# ADR 0011: Gates are for people; a hook blocks edits without an approved plan

Date: 2026-11-18. Status: accepted. Settled with the maintainer in the phase 2 grilling (#20), recorded for issue #55.

Two controls from the AI-native SDLC playbook (`docs/design.md` section 1): humans decide at the Gates, and nothing is implemented without an accepted plan. A skill only advises; these are enforced by `bfc` and by a Claude Code hook.

## Decision

### Gates are for people

The Gates are `submit`, `approve`, `request-changes`, `withdraw` and `close`. They refuse to run inside an agent: they exit non-zero with a message that the action is for a person, and write nothing. Every other command works inside an agent.

- **Detection.** Claude Code sets `CLAUDECODE` for the processes it starts. One place in `bfc` names the Gate commands and the variables that mark an agent; a later harness (phase 4) adds its variable there. `core` stays harness-agnostic.
- **The TUI** hides the Gate actions when it runs inside an agent, through the same `core.Check*` path as every other action rule. The adapter in `cmd/bfc` detects the agent and passes `core` a plain "inside an agent" flag, so `core` never knows which harness is running.
- **Second layer.** `bfc setup claude` adds `permissions.deny` for the Gate commands (`Bash(bfc approve:*)` and the others) to `.claude/settings.json` (ADR 0010), so Claude Code refuses them before `bfc` runs.

Why: an agent runs `bfc` under the developer's git identity. Without this check it could approve a colleague's plan in the developer's name, and the Event would look like the developer's.

**This guards against mistakes, not against a hostile agent.** An agent that unsets `CLAUDECODE`, or writes to the ref with plain git, gets past it. Identity stays the git commit author with no signing (a settled decision); the ref history shows who wrote each Event.

### The PreToolUse hook

`bfc hook pre-tool-use` reads the Claude Code PreToolUse JSON on stdin.

- `Edit`, `Write`, `MultiEdit` and `NotebookEdit` are allowed only when the Current plan (ADR 0009) is `approved`. Otherwise the call is blocked with a reason that says what to do: `bfc start <id>`, or get the plan approved (after a `bfc pull` if it was approved elsewhere).
- Files under `.beforecode/` and `.claude/` are always allowed, compared after making the path relative to the repository root. Files outside the repository are allowed too: the hook guards this repository's code, not the user's other files (issue #61).
- It answers in Claude Code's PreToolUse JSON on stdout, exit code 0: a block is `permissionDecision: "deny"` with the reason, which Claude Code shows the agent. An allowed call gets no output, so Claude Code's own permission rules still apply; the hook never answers `allow`, which would skip them.
- Every other tool is allowed. **Bash is not blocked.**
- **On an internal error** (not a git repository, no Plans ref, damaged ref) it blocks, with the exact fix: `bfc bootstrap` or `bfc doctor`.
- It reads only the local Plans ref and the Index, never the network.
- **It remembers an approved Current plan** in `beforecode/hook-approved` of the worktree's own git directory (`.git/beforecode/hook-approved` in a plain clone), a disposable cache like the Index. The file holds what the decision depended on: the bfc version, the Plans ref tip, the current branch and the plan set with `bfc use`. While they are the same, an edit needs two git starts (`rev-parse` and `symbolic-ref`) instead of ten to twenty, which keeps the hook under 200 ms where starting git is slow (Windows, about 950 ms per edit without it, issue #61). Any change of them, such as an approval, a pull or a switch of branch, makes the hook do the full check again. Only an allowed edit is remembered; a block is always the result of the full check.
- `claude.enforce: false` in `.beforecode/config.yaml` turns it off for the repository.

## Considered

- **Blocking Bash too.** Rejected: what a shell command changes cannot be known before it runs, and blocking it would also block `git`, the tests and `bfc` itself. The hook stops the normal path; the Plan check (phase 3) catches changes outside the plan at pull request time.
- **Allowing on an internal error (fail open).** Rejected: a broken setup would silently remove the control. Blocking with the fix makes the problem visible and quick to solve.
- **Fetching before deciding.** Rejected: the hook runs before every edit, must be fast and must work offline. The block reason mentions `bfc pull` instead.
- **Only a skill or instructions in `bfc prime`.** Rejected as the only control: the playbook calls a skill advisory and a hook deterministic. `bfc prime` still explains the rule so the agent rarely hits the hook.
- **Signing Gate Events** so an agent cannot forge one. Rejected for now: identity is the git commit author, without signing (settled).

## Consequences

- When Claims arrive in phase 3, the hook must also allow edits while the Current plan is `in-progress`.
- Because `.beforecode/` and `.claude/` are exempt, an agent could also turn the hook off (`claude.enforce: false`) or remove it from `.claude/settings.json`. Like the Gate check, the hook guards against mistakes, not against a hostile agent; such a change is a diff on a normal branch that review sees.
- An agent can still change files through Bash without an approved plan. That is a known gap, closed at pull request time by the Plan check, not by the hook.
- A new harness in phase 4 needs its own detection variable and, if it has hooks, its own hook command.
