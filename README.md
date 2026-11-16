# BeforeCode

**Plan before code.** BeforeCode (`bfc`) is an open source terminal app where a team writes, challenges, approves and shares plans for AI coding agents. Agents then implement only against an approved plan.

Status: early development. Only `bfc version` works so far. Start with [docs/design.md](docs/design.md).

- Plans live in Git, in a hidden ref (`refs/beforecode/plans`), so they are shared with a plain `git push` and never touch your code branches.
- Claude Code is the first supported agent harness. The design keeps the core independent of any one harness.
- Written in Go with Bubble Tea. Licensed under Apache-2.0.

## How we work on this repository

Work is tracked as GitHub issues, one thin vertical slice per issue, one small pull request per issue. See [CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and [CONTEXT.md](CONTEXT.md) for the vocabulary. Agents follow [CLAUDE.md](CLAUDE.md).

## License

BeforeCode is licensed under the [Apache License 2.0](LICENSE).
