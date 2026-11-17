# BeforeCode

`bfc` is a Go TUI/CLI for managing plans for AI coding agents. Read `docs/design.md` before any design-affecting change and `CONTEXT.md` for the vocabulary. Use those terms exactly in code, issues and docs.

## Decisions that are settled

Do not reopen these without the maintainer asking. Details and reasons are in `docs/design.md`.

- Go + Bubble Tea (Bubbles, Lip Gloss, Glamour, Huh). One static binary, command name `bfc`.
- Plans are plain Markdown/YAML files stored in the hidden git ref `refs/beforecode/plans`. No Dolt, no server. SQLite is a disposable local cache only.
- Only `.beforecode/config.yaml`, `.beforecode/skills/` and `.beforecode/templates/` live on normal branches.
- Git is driven through the system `git` binary (subprocess), never a Go git library.
- GitHub only, single repository. No GitLab, no multi-repo plans.
- Two documents per plan: short `intent.md` (human) and `plan.md` (for agents, with acceptance criteria `AC-1`, `AC-2`, …).
- Identity is the git commit author. No signing or extra trust rules yet.
- Apache-2.0.

## Workflow

1. Work only on a GitHub issue. Each issue is one thin vertical slice with acceptance criteria. If the issue is unclear, grill the maintainer before writing code.
2. One issue, one branch, one small pull request that says `Closes #<n>`.
3. Test first. Storage and sync code is tested against temporary git repositories, with a local bare repository as the remote. No network in unit tests.
4. Run the checks below before opening a pull request, then review the diff against the issue's acceptance criteria.
5. If the implementation must depart from the issue or from `docs/design.md`, say so in the pull request and update the document in the same pull request. Record a non-obvious decision as an ADR in `docs/adr/`.

Suggested skills (mattpocock/skills): `grilling` for unclear issues, `prototype` for TUI and state-model questions, `tdd` for implementation, `code-review` before a pull request, `domain-modeling` when touching `CONTEXT.md` or ADRs. They come from the user-installed `mattpocock-skills` plugin, not from this repository; see `CONTRIBUTING.md`. If they are missing, say so instead of guessing their content.

## Commands

Requires Go 1.27+ and golangci-lint v2 (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`).

```
go build ./...
go test ./...
golangci-lint run
go run ./cmd/bfc                                          # usage
go run ./cmd/bfc version                                  # prints "bfc dev"
go build -ldflags "-X main.version=v0.1.0" ./cmd/bfc      # inject the version
```

The CLI uses Cobra (`github.com/spf13/cobra`). Commands live in `cmd/bfc/`.

CI (`.github/workflows/ci.yml`) runs build and `go test -race` on Linux and macOS, build and `go test` on Windows, and `golangci-lint` with the linter set in `.golangci.yml` on every pull request and push to `main`. `.gitattributes` keeps LF line endings on Windows so gofmt passes there too.

## Package layout (target)

```
templates.go      default templates embedded from .beforecode/templates
cmd/bfc/          entry point
internal/config/  .beforecode/config.yaml parsing (ADR 0003, no I/O)
internal/core/    plan lifecycle, events, approval policy (no I/O)
internal/store/   hidden-ref storage through git plumbing
internal/index/   SQLite cache rebuilt from the ref
internal/agent/   Agent interface and harness adapters
internal/mcp/     MCP server
internal/tui/     Bubble Tea screens
```

`core` must not import `store`, `agent`, `tui` or `mcp`. `core` does not know which harness is running.

## TUI conventions

Set by the plan list (`internal/tui/planlist.go`, issue #16); every new screen follows them.

- Bubble Tea v2: import `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`, not the older `github.com/charmbracelet/...` paths.
- One screen per file in `internal/tui`: a value-type model with an exported constructor that takes its dependencies.
- A screen gets data through a small interface that `tui` defines (`Plans`), implemented in `cmd/bfc` (`tuiPlans` in `tui.go`) and faked in tests. `tui` imports `core` only, never `store`; all I/O runs inside a `tea.Cmd`. Anything that needs the terminal, such as the editor, runs through `tea.ExecProcess` in the adapter.
- Keys: one key map per screen in `keys.go`, built with `bubbles/key`; the help line is rendered from the bindings. Styles: shared Lip Gloss styles in `styles.go`, 16 basic ANSI colors.
- Every view fits 80x24: the help line sits on the bottom row and lines are cut with `truncate`.
- Tests drive the screen with `teatest/v2` and the fake adapter, then compare the final view, stripped of styling, with `testdata/<test name>.golden`. Review each golden file by eye; regenerate them with `go test ./internal/tui -update`.
- `bfc` without a command opens the TUI only when stdin and stdout are a terminal; otherwise it prints usage.

## Rules for the hidden ref

- Never touch the user's working tree, index, or current branch when reading or writing plans. Use `hash-object`, a temporary index (`GIT_INDEX_FILE`), `write-tree`, `commit-tree`, `update-ref`.
- Events, messages, handoffs and checks are append-only: one new file per item, named by ULID. Never rewrite them. The file formats are in `docs/adr/0002-data-format-v1.md`.
- Plan state is derived from `events/`, never stored.
- Never force-push the ref. On a rejected push: fetch, merge in memory with `git merge-tree --write-tree`, retry.

## Agent skills

### Issue tracker

GitHub Issues in admSla99/BeforeCode (in cloud sessions through the GitHub MCP tools). See `docs/agents/issue-tracker.md`.

### Triage labels

The five default roles, each label named after its role (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repository root. See `docs/agents/domain.md`.
