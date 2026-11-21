## Manual run with Claude Code, 2026-11-20 (issue #67, AC-5)

- Two clones (Ana, Ben) of one local bare remote, Linux, in a cloud session.
- bfc dev built from the pull request's branch; Claude Code 2.1.329 (the real one, not the fake).
- The TUI ran in tmux at 80x24 and was driven by an agent (Claude Code) typing keys for Ana and Ben, not by a person as issue #67 asks. The maintainer accepted this run for AC-5 and will not repeat it by hand.
- The automated test (`e2e/claude_test.go`) does not drive the TUI, which needs a terminal: the fake Claude Code runs the bfc commands of a Grill directly, and the answer goes through `bfc reply`, which writes the same Message as the Grill screen. This run is the one through the TUI.
- The repository held `greet.go`, a program that prints `hello`. The Intent: greet the user by name from the first argument, fall back to `hello`.

### Steps and results

| Step | Who | Result |
| --- | --- | --- |
| `bfc init`, `bfc setup claude`, commit and push `main` | Ana | ok |
| `bfc new "Greet by name"`, Intent with `bfc edit --intent` | Ana | ok, `bfc-8szj` |
| TUI: open the plan, `g`, Enter | Ana | Claude Code asked 4 Questions, one at a time; each answer was stored and listed under "answered" |
| `ctrl+f` (finish) | Ana | Summary of what was decided and what stayed open; see finding 1 |
| `w` (write plan.md) | Ana | First attempt failed, the retry wrote plan.md; see finding 2 |
| `s` (submit) | Ana | `in-review` |
| TUI: open the plan, `a` | Ben | Not offered at first (finding 1); after the fix by hand, approved 1/1 |
| `claude -p "Change greet.go …"` on `main`, no Current plan | Ana | The hook blocked the edit; Claude Code said to run `bfc start <id>` or `bfc use <id>` |
| `bfc pull`, `bfc start bfc-8szj` | Ana | Branch `bfc-8szj-greet-by-name`, the Current plan |
| `claude -p "Implement the current plan."` | Ana | Claude Code ran `bfc prime`, edited `greet.go` and added `greet_test.go` and `go.mod`; tests passed; it did not commit and did not run a Gate |

### Findings

1. **The last Question of a Grill stays open and blocks approval** (#92). When it had nothing left to ask, Claude Code still recorded a Question ("Do you want to finish the Grill and move on to writing plan.md?"). Ana pressed `ctrl+f`, so it was never answered. Ben's TUI then did not offer `a`, without saying why; `bfc approve` named the open Question. Ana had to run `bfc reply <ULID> -m "Yes, finish."`. The `grill-intent` skill or the finish turn should not leave such a Question open, and the plan detail could say why approval is not offered.
2. **A stream-json line that `bfc` cannot parse ends the turn** (#93). The first write-plan turn failed with `claude printed a line that is not JSON: "{\"type\":\"system\",\"subtype\":\"per…`. The line is JSON, but a field the adapter reads (`line` in `internal/agent/claude/claude.go`, such as `message` or `errors`) has another type in this `system` line, so `json.Unmarshal` fails. The adapter should read the `type` first and ignore line types it does not use, as its README says it does. The retry did not print that line and succeeded.
3. **Claude Code told Ana to submit an approved plan.** After implementing it said "open `bfc` and submit the plan there"; the plan was already approved. Minor; `bfc prime` could say what comes after an approved plan (a pull request).
4. Outside BeforeCode: Claude Code ignored the `permissions.allow` entries of `.claude/settings.json` in a workspace whose trust dialog was never accepted, so `-p` runs needed `--allowedTools`. The hook ran regardless.
