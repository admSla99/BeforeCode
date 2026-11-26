## Context

Issue #115, stage 2. Stage 1 prototyped three layouts; the author chose **A: a `discussion` tab** (Question 01M8CGS9YK5GJ35WTNBCN1VXPZ, answered in this plan). The throwaway prototype is on the branch `prototype-115-discussion` (`go run ./internal/tui/prototype_discussion tab`); use it as the visual reference, not as code to copy.

The Discussion is already read and written by the CLI: `core.Discussion` orders Messages into Threads with depth, `core.OpenQuestions` decides open and answered, `writeDiscussion` in `cmd/bfc/show.go` prints it for `bfc show`, and `writeMessage` in `cmd/bfc/ask.go` writes a Message for `bfc ask` and `bfc reply`. The Grill screen (`cmd/bfc/tuigrill.go`) already writes replies from the TUI through `writeMessage`.

`core.CheckRef` checks only the form of a `ref`; nothing yet says whether the heading or AC it names still exists in `plan.md`.

Follow the TUI conventions in `CLAUDE.md`: one key map per screen in `keys.go`, shared styles, 80x24, `tui` imports only `core`, I/O inside a `tea.Cmd`, writes reuse the CLI code, golden files with `teatest/v2` and `waitFor`.

## Approach

Add a fourth tab to the plan detail, after `tasks`: `discussion`, with the open Questions counted in its name (`discussion (2 open)`). Tab cycles plan.md → intent.md → tasks → discussion → plan.md.

The tab is one list built from `core.Discussion`: each Message shows a header line (author, `(through <agent>)` when set, time, `to …`, `ref`, `open`/`answered` for a Question) and its body below, replies indented by depth. A `ref` to a heading or AC of `plan.md` that no longer exists gets ` (stale)`. The list scrolls so the selected Message stays visible; long lines are cut with `truncate`, bodies wrap to the width.

Keys on the tab:
- `↑↓`: select a Message.
- `r`: reply to the selected Message; the text is typed on the line above the status bar, as the review reason is.
- `n`: start a Thread. First a picker of the targets (the plan, `intent.md`, each heading of `plan.md`, each AC), then the text. It is a Message without addressees.
- `q`: ask a Question: the same picker, then the addressees (emails or `ai`, comma separated, checked like `bfc ask --to`), then the text.

Writes go through new `Plans` methods implemented in `tuiPlans` with `writeMessage`, so a Message from the TUI is the same file `bfc ask` / `bfc reply` writes. After a write the screen returns a result message, the app syncs, and the detail reloads.

When a sync brings new Messages to the open plan, the detail shows a notice in the status line (`2 new Messages`) and the tab's open count updates on reload.

Stale detection lives in `core` (no I/O): a function that, given `plan.md`, says whether a `ref` names an existing heading anchor or AC, using the same heading slugs and AC parsing the rest of `core` uses.

## Affected areas

- `internal/core/message.go` (or a new file next to it): stale `ref` detection and the list of refable targets of `plan.md` (headings and ACs).
- `internal/tui/plans.go`: `DocDiscussion`, `PlanDetail.Messages` (the Discussion rows) and `OpenQuestions`, new `Plans` methods for writing a Message, and their result message.
- `internal/tui/detail.go`, or a new `internal/tui/discussion.go` for the tab's list and input: rendering, selection, the picker and the text input.
- `internal/tui/keys.go`: the discussion tab's key map.
- `internal/tui/app.go`: the notice when a sync brings new Messages to the open plan.
- `cmd/bfc/tui.go`: `tuiPlans.Detail` fills the Messages; the new write methods use `writeMessage` from `cmd/bfc/ask.go`.
- `internal/tui/testdata/*.golden` and the fake `Plans` in the tests.
- `docs/design.md`: the TUI section describes the discussion tab.

## Out of scope

- The AI answering Questions addressed to `ai` (#116).
- Editing, deleting or resolving Messages; they are append-only.
- Starting a Thread from a heading or AC inside the rendered document (layout C); the picker covers it.
- Changes to `bfc show`, `bfc ask` or `bfc reply` beyond sharing their code.

## Tasks

- T-1: In `core`, add stale `ref` detection against `plan.md` and the list of refable targets (the plan, `intent.md`, each heading anchor and each AC of `plan.md`), with table tests.
- T-2: Add the read-only `discussion` tab: `DocDiscussion` in the Tab cycle, the open count in its name, `PlanDetail.Messages` filled by `tuiPlans.Detail`, the list with indented replies, the `(stale)` marker and selection with `↑↓`; golden tests with the fake `Plans`.
- T-3: Add the writes on the tab: `r` reply, `n` start a Thread and `q` ask a Question with the target picker and the addressee input, through new `Plans` methods that `tuiPlans` implements with `writeMessage`; teatest tests for each, and a `cmd/bfc` test that a Message written through `tuiPlans` reads back in `bfc show --json` like one from `bfc ask`.
- T-4: Show a notice when a sync brings new Messages to the open plan and reload the open count; a test with the fake `Syncer`. Update the TUI section of `docs/design.md`.

## Acceptance criteria

- AC-1: The plan detail has a fourth tab `discussion` after `tasks`; Tab cycles plan.md → intent.md → tasks → discussion → plan.md, and the tab's name shows the number of open Questions (`discussion (2 open)`), or just `discussion` when none are open.
- AC-2: The tab lists every Thread in the order of its first Message, each reply indented under the Message it replies to, and each Message shows author (with `(through <agent>)` when `agent` is set), time, `to`, `ref` and, for a Question, `open` or `answered`, with the same values `bfc show` prints.
- AC-3: A Message whose `ref` names a heading anchor or AC that is not in the current `plan.md` is shown with ` (stale)` after the `ref`; `intent.md`, `plan.md` and refs that exist are not marked.
- AC-4: With a Message selected, `r` and a typed text write a reply to it; `bfc show --json` then shows the reply with `reply_to` set to that Message, identical in form to one written with `bfc reply`.
- AC-5: `n` offers the plan, `intent.md`, every heading of `plan.md` and every AC as targets; choosing one and typing a text writes a Message without addressees whose `ref` is that target.
- AC-6: `q` with a target, addressees (emails and/or `ai`) and a text writes a Question that `bfc show` lists as open; an addressee that is neither an email nor `ai` is refused with the same message as `bfc ask --to`, and nothing is written.
- AC-7: Esc at any step of `r`, `n` or `q` cancels and writes nothing.
- AC-8: When a sync brings new Messages to the plan open in the detail, the status line shows a notice with their number, and the open Questions count in the tab name matches the new state.
- AC-9: Every view of the tab fits 80x24 with the help line on the bottom row, built from the tab's key map; golden files cover an empty Discussion, Threads with replies, open and answered Questions, a stale `ref`, and the picker, and the TUI tests pass with `-race`.
