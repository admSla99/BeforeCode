# Contributing to BeforeCode

Thank you for helping. This page describes how work moves from an issue to a merged pull request. Agents follow the same rules, written for them in [CLAUDE.md](CLAUDE.md).

## Before you start

- Read [docs/design.md](docs/design.md) before a change that affects the design.
- Use the vocabulary in [CONTEXT.md](CONTEXT.md) exactly, in code, issues and docs. If you introduce a new concept, add it there in the same pull request.
- Some decisions are settled (Go and Bubble Tea, plans in the hidden ref `refs/beforecode/plans`, the system `git` binary, GitHub only, Apache-2.0, and others). They are listed in `CLAUDE.md` with reasons in `docs/design.md`. Please do not reopen them in a pull request; open an issue to discuss instead.

## Set up

You need:

- Go 1.27 or newer
- Git
- golangci-lint v2: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`

```
git clone https://github.com/admSla99/BeforeCode.git
cd BeforeCode
go run ./cmd/bfc version
```

On Windows, `.gitattributes` keeps Go files with LF line endings so `gofmt` passes. If you cloned before that file existed, run `git add --renormalize .` once.

### Claude Code skills (optional)

The workflow below names skills from [mattpocock/skills](https://github.com/mattpocock/skills) (MIT): `grilling`, `prototype`, `tdd`, `code-review`, `domain-modeling`, `diagnosing-bugs` and `codebase-design`. They are not part of this repository. If you use Claude Code, install the plugin once for your user:

```
/plugin marketplace add mattpocock/skills
/plugin install mattpocock-skills@mattpocock
```

Last checked with plugin version 1.3.2. Update with `/plugin marketplace update mattpocock`. If you already have the plugin through another source, for example synced with your Claude account, do not install it a second time, or every skill will appear twice.

These skills are for developing BeforeCode. They are different from the skills BeforeCode itself ships in `.beforecode/skills/`.

## The workflow

1. **One issue.** All work starts from a GitHub issue. Each issue is one thin vertical slice with acceptance criteria (`AC-1`, `AC-2`, …). If an issue is unclear, ask in the issue before writing code.
2. **One branch, one small pull request.** Name the branch after the issue, for example `12-plan-list`. The pull request says `Closes #<n>` and covers only that issue.
3. **Tests first.** Write a failing test for an acceptance criterion, make it pass, then clean up. Storage and sync code is tested against temporary git repositories, with a local bare repository as the remote. Unit tests never use the network.
4. **Run the checks** before you open the pull request:

   ```
   go build ./...
   go test ./...
   golangci-lint run
   ```

   CI runs the same checks on Linux, macOS and Windows. All of them must pass before a pull request can be merged.
5. **Review your own diff against the issue.** Fill in the pull request template: which acceptance criteria are covered, how you tested them, and anything that departs from the issue.
6. **Departures are documented.** If the implementation must differ from the issue or from `docs/design.md`, say so in the pull request and update the document in the same pull request. Record a non-obvious decision as an ADR in `docs/adr/`.

## The plan check

Every pull request also runs `.github/workflows/bfc-plan-check.yml` (ADR 0014). It finds the pull request's plan from the Short ID in the branch name (`bfc start` names branches that way) or a line `Plan: bfc-…` in the description. Claude Code then compares the diff with that plan's `plan.md`. The result is stored in the Plans ref, and when the pull request departs from the plan it gets the `plan-mismatch` label and a comment. The check warns; it never blocks merging. A pull request without a plan, or from a fork, is skipped with a note in the job summary.

The check needs the `ANTHROPIC_API_KEY` repository secret; without it, a pull request that has a plan fails the job with a message saying so. To add it, a repository admin opens **Settings → Secrets and variables → Actions → New repository secret**, names it `ANTHROPIC_API_KEY` and pastes an Anthropic API key. With the GitHub CLI: `gh secret set ANTHROPIC_API_KEY --repo admSla99/BeforeCode`, which asks for the key.

The check writes `format-version` 2 results, so the Plans ref must be migrated once with `bfc migrate` and `bfc push` before the first real check.

## Working with git in code

Plans are stored in the hidden ref. Code that reads or writes plans must never touch the user's working tree, index or current branch, and must never force-push. The full rules are in the "Rules for the hidden ref" section of `CLAUDE.md`.

## License

By contributing you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
