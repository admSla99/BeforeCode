## Context

The Grill screen of the TUI (`internal/tui/grill.go`, adapter `cmd/bfc/tuigrill.go`, ADR 0012) shows one open Question at a time and an answer line where the author types the whole answer. The agent asks through `bfc ask` (ADR 0008) and the TUI records the answer itself as a reply from the plan author without `agent`, like `bfc reply --verbatim`.

Grill Questions often have a few likely answers that the agent already knows. This plan lets the agent attach them to the Question, and lets the author pick one with the arrow keys or write their own, as Claude Code does.

Decisions this plan takes (no Grill was held; the author may change them before submitting):

- Options are stored on the Question Message as a new optional front matter field `options`: a list of plain one-line strings. ADR 0002 says adding an optional field needs no format bump and readers ignore unknown fields, so older binaries keep working and simply show no options.
- `options` is only valid on a Question (a Message with `to`). Each option is non-empty, one line, unique within the Question; at most 9 options.
- `bfc ask` gets a repeatable `--option <text>` flag (repeat only, no comma splitting, because options may contain commas).
- The answer is always plain text: picking an option stores exactly that option's text as the reply body. Nothing records which option was picked, so `bfc show`, the Discussion and `bfc wait` read the same for a picked or typed answer.
- The list always ends with a custom item ("Write my own answer"); choosing it opens the text line as today. A Question without options shows only the text line, as today.

## Approach

Bottom up, each step usable on its own:

1. `core.Message` gets `Options []string`, encoded as `options` in the front matter (omitted when empty) and parsed back. A validation function in `core` rejects options on a non-Question, empty or multi-line options, duplicates and more than 9.
2. `bfc ask --option` fills `Options` and refuses invalid ones with the core error. `messageJSON` (the `bfc ask --json` shape, also used by `bfc show --json` `open_questions` and `messages`, and `bfc wait --json`) gets `options`, always a list, `[]` when empty. `bfc show` human form lists the options of a Question under its text.
3. The Grill: `tui.GrillQuestion` gets `Options`, filled by `tuiGrill.Snapshot`. While an open Question with options awaits an answer, the screen shows the options as a list with a cursor, plus the custom item last. ↑↓ move, enter answers with the selected option through the existing `Grill.Answer`; enter on the custom item opens the text line, where esc returns to the list. Keys live in `keys.go` and the help line is rendered from them. The view still fits 80x24; long options are cut with `truncate`.
4. The agent is told about options: the Grill turn prompt in `tuigrill.go` and the `grill-intent` skill say to pass the likely answers with `--option` when there are some, and never an option that only means "something else" (the custom item is always there). In the chat (Claude Code without the TUI) the skill tells the agent to offer the same options to the author.
5. Docs: ADR 0002 section 7 gets the `options` field (dated note, no format change), `docs/design.md` describes the flag, the JSON field and the Grill list, `CONTEXT.md` if a new term is needed.

## Affected areas

- `internal/core/message.go` (+ tests): `Options`, encode, parse, validation.
- `cmd/bfc/ask.go`, `cmd/bfc/show.go` (+ tests): `--option`, `options` in JSON, human output.
- `cmd/bfc/tuigrill.go`: snapshot options, turn prompt.
- `internal/tui/grill.go`, `internal/tui/keys.go`, `internal/tui/grill_test.go`, new golden files in `internal/tui/testdata`.
- `.beforecode/skills/grill-intent/SKILL.md` (the copy embedded in the binary follows it).
- `docs/adr/0002-data-format-v1.md`, `docs/design.md`.

## Out of scope

- Choosing several options at once (multi-select).
- Options for Questions outside the Grill in the TUI, such as the Discussion tab of bfc-mmst; the format allows them later.
- Changing how the agent decides what to ask.
- A data format version bump or `bfc migrate` changes.
- Recording which option was picked anywhere else than the answer text.

## Tasks

- T-1: Add `Options` to `core.Message`: the `options` front matter field (encode, parse, omitted when empty) and its validation (Questions only, non-empty, one line, unique, at most 9), with tests.
- T-2: Add `bfc ask --option` (repeatable) and `options` to the Message JSON of `bfc ask`, `bfc show` and `bfc wait`; list the options in the human `bfc show` output; tests against temporary repositories.
- T-3: Show the options in the Grill screen as a selectable list ending with the custom item; enter on an option answers with its text, enter on the custom item opens the text line, esc goes back to the list; keys in `keys.go`; teatest tests with golden files.
- T-4: Tell the agent about options in the Grill turn prompt (`tuigrill.go`) and in the `grill-intent` skill, and update ADR 0002 section 7 and `docs/design.md`.

## Acceptance criteria

- AC-1: `bfc ask <id> --to <author> --option A --option "B, with comma" -m "Q?"` writes a Message whose front matter has `options: [A, "B, with comma"]`; reading it back gives the same two options in the same order.
- AC-2: `bfc ask` refuses, writing nothing, when `--option` is given with no `--to`, when an option is empty or contains a newline, when two options are equal, or when there are more than 9.
- AC-3: `bfc ask --json`, `bfc show --json` (in `open_questions` and `messages`) and `bfc wait --json` include `options` for every Message: the list for a Question with options, `[]` otherwise, never `null`.
- AC-4: A Message file without `options` parses as before, and a binary without this change reads a Message with `options` without error (the field is ignored).
- AC-5: In the Grill screen, an open Question with options A and B shows a list "A", "B" and a last custom item; ↓ then enter stores a reply from the plan author without `agent` whose body is exactly "B", and the Grill continues with the next turn.
- AC-6: In the Grill screen, choosing the custom item opens the answer line; typing text and enter stores that text as the answer, as today; esc in the answer line returns to the list without storing anything.
- AC-7: An open Question without options shows only the answer line, and the existing Grill golden files are unchanged.
- AC-8: Every Grill view with options fits 80x24, with options longer than the width cut, checked by golden files.
- AC-9: `bfc show` (human form) and the Discussion show an answer picked from the options exactly like a typed answer with the same text.
- AC-10: The Grill turn prompt and the `grill-intent` skill tell the agent to pass likely answers with `--option`, and ADR 0002 section 7 and `docs/design.md` document the `options` field, the flag and the Grill list.
