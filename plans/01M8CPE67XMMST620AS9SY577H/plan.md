## Context

Issue #115 (part of #21). The plan detail (`internal/tui/detail.go`) has three tabs, `plan.md`, `intent.md` and `tasks`; the Discussion of a plan is only visible in `bfc show` (`writeDiscussion` in `cmd/bfc/show.go`) and can only be joined with `bfc ask` / `bfc reply`.

Decided in the Grill: the maintainer chose layout **A** from the prototype in #115 (branch `prototype-115-discussion`, throwaway, not merged): a fourth tab `discussion` after `tasks`, with the open Questions counted in its name (`discussion (2 open)`). The whole Discussion is one list: Threads in the order of their first Message, replies indented, ↑↓ select a Message. A Thread on a heading or an AC is started from a picker of the headings and ACs of `plan.md`, not from the cursor in the document (that was layout C).

What exists and must be reused:

- `core.Discussion` orders the Messages as Threads with their depth and answered flag; `core.OpenQuestions` gives the open ones; `core.CheckRef` checks the form of a `ref`, but nothing checks yet that the heading or AC still exists.
- `writeMessage` in `cmd/bfc/ask.go` writes a Message for `bfc ask` and `bfc reply` (`core.CheckWritable`, author, agent, ULID, one commit). `addressee` validates a `--to` value.
- `core.AcceptanceCriteria(plan)` returns the AC IDs of `plan.md`; there is no function yet for the heading anchors.
- The TUI conventions in `CLAUDE.md`: data through the `Plans` interface, implemented by `tuiPlans` in `cmd/bfc/tui.go` and faked in tests; all I/O in a `tea.Cmd`; writes reuse the CLI's code; the screen returns a result message and the app syncs after it; one key map in `keys.go`; 80x24; golden files with `teatest/v2`, `newTestModel` and `waitFor`.

Keys on the discussion tab: the prototype used `r` to reply and `q` to ask, but on the detail `r` is request changes and `q` quits. So `enter` replies to the selected Message and `n` writes a new Message (a Thread or a Question); every other key keeps its meaning.

## Approach

1. **Core: which refs are stale.** Add to `internal/core` a function that returns the headings of `plan.md` with their GitHub-style anchors (lower case, spaces to `-`, punctuation dropped, the alphabet of `refAnchor`; duplicates get `-1`, `-2`), and `StaleRef(ref string, plan []byte) bool`: true for `plan.md#<anchor>` whose heading is gone and for `plan.md#AC-n` whose criterion is gone (`AcceptanceCriteria`). `intent.md`, `plan.md` and `checks/<ULID>` are never stale here. Pure functions, table tests, no I/O.
2. **Read the Discussion in the TUI.** `PlanDetail` gets `Messages []MessageRow` in `core.Discussion` order (ID, Depth, By with the harness through `throughAgent`, At, To, Ref, Stale, Question, Answered, Body), `OpenQuestions int`, and `Refs []RefChoice` (label and ref: the whole plan, `intent.md`, each heading, each AC). `tuiPlans.Detail` fills them from `readMessages`. `Doc` gets `DocDiscussion` after `DocTasks`; Tab cycles through four tabs; it is not editable. The tab renders like prototype A: the first line of a Message is `author · MM-DD HH:MM · to … · open|answered · ref`, `(stale)` after a stale ref, the body below, indented two spaces per depth, a blank line between Threads, the selected Message marked `▶`; the view follows the selection like the tasks tab.
3. **Write from the TUI.** `Plans` gets `Ask(ctx, shortID string, to []string, ref, body string) error` and `Reply(ctx, messageID, body string) error`; `tuiPlans` implements them with `writeMessage` (and `addressee`, `core.CheckRef`), never a copy. `enter` on a Message types a reply on one line above the status bar (`editLine`, like the request-changes reason). `n` opens the ref picker (↑↓, enter), then a line for `to` (emails or `ai`, comma-separated; empty means a Thread with no Question), then the text. Esc cancels at every step. A successful write returns `MessageWrittenMsg`; the app syncs after it, like after `reviewDoneMsg`, and the detail reloads. Writing is offered only while the plan is writable (`PlanDetail.Editable`); refusals are shown in the status line.
4. **Sync notice.** When the detail reloads after a sync, it compares the Message IDs with the ones it had; new ones give a status line such as `2 new Messages in the discussion`, and the open count in the tab name follows the reload. The app already reloads the detail when a sync changed something (`syncDoneMsg`).

## Affected areas

- `internal/core/message.go` (or a new `internal/core/anchor.go`) and tests: heading anchors, `StaleRef`.
- `internal/tui/plans.go`: `MessageRow`, `RefChoice`, new `PlanDetail` fields, `DocDiscussion`, `Ask` and `Reply` on `Plans`, `MessageWrittenMsg`.
- `internal/tui/detail.go`: the discussion tab, selection, reply line, ref picker and `to` line, sync notice.
- `internal/tui/keys.go`: Reply, NewMessage and picker bindings on `detailKeys`.
- `internal/tui/app.go`: sync after `MessageWrittenMsg`.
- `internal/tui` tests: the fake `Plans` gains Messages, `Ask` and `Reply`; new golden files in `internal/tui/testdata`.
- `cmd/bfc/tui.go` and `cmd/bfc/tui_test.go`: `Detail` fills the Discussion; `Ask` and `Reply` through `writeMessage`.
- `docs/design.md`: the description of the plan detail lists the discussion tab, if it lists the tabs.

## Out of scope

- The AI answering Questions sent `to: ai` (#116).
- Markers in the rendered document and starting a Thread from the cursor line (layout C), and a side pane (layout B).
- Multi-line Messages from the TUI (the editor); a Message is one line typed in the TUI. Longer text still goes through `bfc ask` / `bfc reply`.
- Marking stale refs in `bfc show`, and stale refs to Plan checks.
- Answering a Question from the Inbox screen.
- Any change to the data format of Messages.

## Tasks

- T-1: Core: heading anchors of plan.md and `StaleRef`, with table tests (headings with punctuation, duplicates, a removed heading, a removed AC, intent.md, checks/).
- T-2: Read the Discussion: `MessageRow`, `OpenQuestions` and `RefChoice` on `PlanDetail`, filled by `tuiPlans.Detail`; the `discussion (n open)` tab with Threads, indented replies, open/answered, ref and stale, ↑↓ selection; fake adapter, teatest golden files at 80x24.
- T-3: Write from the TUI: `Ask` and `Reply` on `Plans`, implemented with `writeMessage`; `enter` replies, `n` with the ref picker and the `to` line; `MessageWrittenMsg` makes the app sync; tests in `internal/tui` with the fake and in `cmd/bfc` against a temporary repository.
- T-4: Sync notice: new Messages after a sync reload show a status line and update the open count; teatest with a fake that adds a Message between loads. Update `docs/design.md` if it lists the tabs of the plan detail.

## Acceptance criteria

- AC-1: `StaleRef` returns true for `plan.md#affected-areas` when plan.md has no `## Affected areas` heading and for `plan.md#AC-3` when plan.md has no AC-3, and false for an existing heading or AC, for `plan.md`, `intent.md` and `checks/<ULID>`; covered by table tests in `internal/core`.
- AC-2: The plan detail has a fourth tab after `tasks`, reached with Tab, named `discussion (n open)` where n is the number of open Questions, and `discussion` when there are none.
- AC-3: The discussion tab lists every Message in `core.Discussion` order: Threads in the order of their first Message, each reply indented two spaces per depth under the Message it replies to, a first line with the author (with `(through <harness>)` when `agent` is set), the time, `to …` and `open` or `answered` for a Question, and the ref followed by `(stale)` when `StaleRef` is true. A golden file at 80x24 shows two Threads with replies, an open Question to a person, an answered Question to `ai` and a stale ref.
- AC-4: A plan without Messages shows a line saying there is no Discussion yet and that `n` starts one.
- AC-5: On a writable plan, `n`, a choice in the picker (the whole plan, intent.md, a heading or an AC of plan.md), an empty `to` and a text write one Message with that ref and no `to`; with `to` set to an email or `ai` it writes a Question. The Message is written by `writeMessage`, as `bfc ask` writes it, and `bfc show --json` lists it.
- AC-6: `enter` on a selected Message and a text write a reply with `reply_to` set to that Message, as `bfc reply` does; a reply by an addressee of an open Question shows it as `answered` after the reload.
- AC-7: An invalid `to` value, an empty text or Esc writes nothing; an invalid `to` shows the error in the status line. On a closed or done plan `n` and `enter` do nothing and are not in the help line.
- AC-8: After a successful write from the discussion tab the app starts a sync, as after a review action.
- AC-9: When a sync reload brings Messages the detail did not have, the status line says how many are new and the tab name shows the new open count; a teatest with a fake that adds a Message between loads checks it.
- AC-10: Every state of the discussion tab (list, reply line, ref picker, `to` line, notice) fits 80x24 with the help line on the bottom row; the keys come from `detailKeys` in `keys.go`, and the help line shows `enter reply` and `n new` only on the discussion tab of a writable plan.
