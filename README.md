# BeforeCode

**Plan before code.** BeforeCode (`bfc`) is an open source terminal app where a team writes, challenges, approves and shares plans for AI coding agents. Agents then implement only against an approved plan.

Status: early development (phase 1 of [docs/design.md](docs/design.md)). Plans can be written, shared, reviewed and approved from the command line and the TUI; the AI parts come in phase 2.

- Plans live in Git, in a hidden ref (`refs/beforecode/plans`), so they are shared with a plain `git push` and never touch your code branches.
- Claude Code is the first supported agent harness. The design keeps the core independent of any one harness.
- Written in Go with Bubble Tea. Licensed under Apache-2.0.

## Getting started

You need Git 2.38 or newer.

**Install.** Download the archive for your system from the [latest release](https://github.com/admSla99/BeforeCode/releases/latest) (Linux, macOS and Windows, amd64 and arm64), unpack it and put `bfc` on your `PATH`. `checksums.txt` in the release lets you verify the download. `bfc version` prints the installed version.

With Go 1.27 or newer you can instead build it yourself:

```
go install github.com/admSla99/BeforeCode/cmd/bfc@latest
```

**Start using BeforeCode in a repository.** This writes `.beforecode/config.yaml` and the templates, which you commit like any other change, and creates the plans ref. Nothing is pushed until you push.

```
bfc init
git add .beforecode && git commit -m "Set up BeforeCode" && git push
bfc new "Payments via Stripe"   # writes intent.md in your editor
bfc edit bfc-5v7x               # writes plan.md, with acceptance criteria AC-1, AC-2, …
bfc submit bfc-5v7x             # asks for review
bfc push                        # shares the plans ref
```

**Join a repository that already uses it.** In your clone:

```
bfc bootstrap                   # fetches the plans ref and sets up the fetch refspec
bfc list                        # newest activity first
bfc show bfc-5v7x
bfc approve bfc-5v7x            # someone other than the plan author approves
bfc push
```

`bfc pull`, `bfc push` and `bfc sync` share plans; concurrent changes are merged, never force-pushed. `bfc` alone opens the TUI, which syncs in the background. `bfc doctor` checks the setup and says how to fix what is wrong; `bfc doctor --fix` applies the safe fixes.

Plans live in the hidden ref `refs/beforecode/plans`: bfc never touches your working tree, index or branches.

## How we work on this repository

Work is tracked as GitHub issues, one thin vertical slice per issue, one small pull request per issue. See [CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and [CONTEXT.md](CONTEXT.md) for the vocabulary. Agents follow [CLAUDE.md](CLAUDE.md).

## License

BeforeCode is licensed under the [Apache License 2.0](LICENSE).
