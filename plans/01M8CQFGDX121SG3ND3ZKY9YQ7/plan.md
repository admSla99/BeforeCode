## Context

The Grill screen of the TUI (`internal/tui/grill.go`, adapter `cmd/bfc/tuigrill.go`, ADR 0012) runs one Claude Code turn per Question: the agent records one Question with `bfc ask` (ADR 0008), the screen shows it, the author types the whole answer, and the TUI stores it as a reply from the plan author without `agent`, like `bfc reply --verbatim`.

The author asked (Grill answer on this plan) that the Grill work like the `grill-me` / `grilling` skill of mattpocock/skills, with at most 3 rounds. That skill:

- works the decisions as a design tree, in **rounds**; a round asks the whole **frontier**, every Question whose prerequisites are already settled, and a Question that depends on another open Question of the same round belongs to a later round;
- numbers the Questions of a round and gives each one choices and the agent's **recommended answer**;
- waits for all answers of the round, then recomputes the frontier and asks the next round;
- looks up facts itself and asks the author only for decisions.

Decisions this plan takes (the author may change them before submitting):

- **Options and recommendation are stored on the Question Message** as two new optional front matter fields: `options`, a list of plain one-line strings, and `recommended`, the text of one of the options. ADR 0002 says adding an optional field needs no format bump and readers ignore unknown fields, so older binaries keep working and show no options.
- `options` is valid only on a Question (a Message with `to`): 2 to 5 options, each non-empty, one line, unique within the Question. `recommended` requires `options` and must equal one of them.
- `bfc ask` gets a repeatable `--option <text>` (repeat only, no comma splitting, because options may contain commas) and `--recommend <text>`.
- **The answer is always plain text**: picking an option stores exactly that option's text as the reply body. Nothing records which option was picked or whether it was the recommended one, so `bfc show`, the Discussion and `bfc wait` read the same for a picked or typed answer.
- The list always ends with a custom item ("Write my own answer"); choosing it opens the text line as today. The recommended option is marked `(recommended)` and selected when the Question opens. A Question without options shows only the text line, as today.
- **A round is derived, not stored.** The Grill Questions of a plan (from the AI to the plan author, as today) are taken in ULID order; a Question starts a new round when an answer to an earlier Grill Question already exists with a smaller ULID than it. Every Question an agent records before any answer is one round, in the TUI and in a Grill in Claude Code alike. No field, no format change.
- **One turn asks one round.** This replaces the rule of ADR 0012 (at most one Question per turn) with at most one round per turn; a new ADR records it. The agent reads the code and the plan itself instead of asking for facts; no sub-agents in the TUI turn.
- **At most 3 rounds**, counted per plan with the rule above. The turn prompt names the round number and the limit. Once the third round is fully answered, the screen starts no new asking turn: it shows that the round limit is reached and offers finishing (`f`, then `w`) as when the agent has nothing left to ask. The agent may stop earlier by recording no Question, as today.

## Approach

Bottom up, each step usable on its own:

1. `core.Message` gets `Options []string` and `Recommended string`, encoded as `options` and `recommended` in the front matter (omitted when empty) and parsed back. A validation function in `core` enforces the rules above.
2. `bfc ask --option` and `--recommend` fill them and refuse invalid ones with the core error. `messageJSON` (the `bfc ask --json` shape, also in `bfc show --json` `open_questions` and `messages`, and `bfc wait --json`) gets `options` (always a list, `[]` when empty) and `recommended` (`null` when unset). `bfc show` human form lists the options under a Question, the recommended one marked.
3. Rounds in `core`: a pure function that takes the Grill Questions and their answers and returns the rounds in order (each a list of Questions), with `MaxGrillRounds = 3`. `bfc prime grill` shows the Grill so far grouped by round, with the current round number and the limit.
4. The Grill screen: `tui.GrillSnapshot` carries the rounds and `tui.GrillQuestion` the options and recommendation, filled by `tuiGrill.Snapshot`. While a round has open Questions, the screen shows the first open one with "Q<i>/<n>" and "round <r>/3" in the header; with options it shows the list (recommended marked and selected, custom item last; ↑↓ move, enter answers through the existing `Grill.Answer`; enter on the custom item opens the text line, esc returns to the list). Each answer is stored at once and the next open Question of the round follows. When the round has no open Question, the next turn starts with all answers of the round in the prompt, unless it was round 3: then the screen offers finishing. Questions answered elsewhere (CLI, another clone) drop out of the round on sync, as today. Keys live in `keys.go`; the help line is rendered from them; the view fits 80x24 and cuts long options with `truncate`.
5. The agent: the Grill turn prompt in `tuigrill.go` and the `grill-intent` skill (the copy embedded in the binary follows it) describe the rounds: ask the whole frontier in one turn, each Question with `bfc ask --option … --recommend …` when it has likely answers, never an option that only means "something else" (the custom item is always there), at most 3 rounds, look up facts yourself. In the chat (Claude Code without the TUI) the skill tells the agent to present a round in the `grilling` format: numbered Questions, choices, `➡️` recommended answer, then record each answer verbatim.
6. Docs: ADR 0017 (one round per turn, rounds derived, at most 3, amends ADR 0012); ADR 0002 section 7 gets the `options` and `recommended` fields (dated note, no format change); `docs/design.md` describes the flags, the JSON fields and the Grill screen; `CONTEXT.md` gets **Grill round** and **Recommended answer**.

## Affected areas

- `internal/core/message.go` (+ tests): `Options`, `Recommended`, encode, parse, validation.
- New `internal/core/grill.go` (+ tests): round derivation, `MaxGrillRounds`.
- `cmd/bfc/ask.go`, `cmd/bfc/show.go`, `cmd/bfc/wait.go` (+ tests): flags, JSON, human output.
- `cmd/bfc/prime.go` / `cmd/bfc/grill.go`: Grill so far grouped by round.
- `cmd/bfc/tuigrill.go`: snapshot with rounds and options, turn prompt.
- `internal/tui/grill.go`, `internal/tui/keys.go`, `internal/tui/grill_test.go`, new golden files in `internal/tui/testdata`.
- `.beforecode/skills/grill-intent/SKILL.md`.
- `docs/adr/0017-grill-rounds.md` (new), `docs/adr/0012-grill-turns-in-the-tui.md` (status: amended by 0017), `docs/adr/0002-data-format-v1.md`, `docs/design.md`, `CONTEXT.md`.

## Out of scope

- Choosing several options at once (multi-select).
- Options for Questions outside the Grill in the TUI, such as the Discussion tab of bfc-mmst; the format allows them later.
- Sub-agents that look up facts while the author answers.
- A configurable round limit; 3 is a constant.
- A data format version bump or `bfc migrate` changes.
- Recording which option was picked anywhere else than the answer text.

## Tasks

- T-1: Add `Options` and `Recommended` to `core.Message`: the `options` and `recommended` front matter fields (encode, parse, omitted when empty) and their validation (Questions only, 2 to 5 options, non-empty, one line, unique; `recommended` one of the options), with tests.
- T-2: Add `bfc ask --option` (repeatable) and `--recommend`, and `options` and `recommended` to the Message JSON of `bfc ask`, `bfc show` and `bfc wait`; list the options in the human `bfc show` output with the recommended one marked; tests against temporary repositories.
- T-3: Show the options of the open Question in the Grill screen as a selectable list, the recommended option marked and selected, the custom item last; enter on an option answers with its text, enter on the custom item opens the text line, esc goes back to the list; keys in `keys.go`; teatest tests with golden files.
- T-4: Derive Grill rounds in `core` (new `internal/core/grill.go`, `MaxGrillRounds = 3`) with table tests, and show the Grill so far grouped by round, with the current round and the limit, in `bfc prime grill`.
- T-5: Make the Grill screen work in rounds: take the open Questions of the current round one by one with "Q<i>/<n>" and "round <r>/3", store each answer at once, start the next turn when the round is answered, and after round 3 offer finishing instead of a new turn; handle Questions answered elsewhere; teatest tests with golden files.
- T-6: Tell the agent about rounds, options and the recommendation in the Grill turn prompt (`tuigrill.go`) and in the `grill-intent` skill, including the chat format of the `grilling` skill and the limit of 3 rounds.
- T-7: Write ADR 0017, mark ADR 0012 as amended, add the fields to ADR 0002 section 7, and update `docs/design.md` and `CONTEXT.md`.

## Acceptance criteria

- AC-1: `bfc ask <id> --to <author> --option A --option "B, with comma" --recommend A -m "Q?"` writes a Message whose front matter has `options: [A, "B, with comma"]` and `recommended: A`; reading it back gives the same options in the same order and the same recommendation.
- AC-2: `bfc ask` refuses, writing nothing, when `--option` is given with no `--to`, when there is 1 option or more than 5, when an option is empty or contains a newline, when two options are equal, when `--recommend` is given without `--option`, or when `--recommend` matches no option.
- AC-3: `bfc ask --json`, `bfc show --json` (in `open_questions` and `messages`) and `bfc wait --json` include `options` and `recommended` for every Message: the list and the text for a Question that has them, `[]` and `null` otherwise, never `options: null`.
- AC-4: A Message file without `options` or `recommended` parses as before, and a binary without this change reads a Message with them without error (the fields are ignored).
- AC-5: In the Grill screen, an open Question with options A, B and recommendation B shows "A", "B (recommended)" and a last custom item, with B selected; enter alone stores a reply from the plan author without `agent` whose body is exactly "B"; ↑ then enter stores "A".
- AC-6: In the Grill screen, choosing the custom item opens the answer line; typing text and enter stores that text as the answer, as today; esc in the answer line returns to the list without storing anything.
- AC-7: An open Question without options shows only the answer line, and the existing Grill golden files of a single Question without options are unchanged apart from the round and Question counters.
- AC-8: Every Grill view with options fits 80x24, with options longer than the width cut, checked by golden files.
- AC-9: `bfc show` (human form) and the Discussion show an answer picked from the options exactly like a typed answer with the same text.
- AC-10: ADR 0017 exists and ADR 0012 points to it; ADR 0002 section 7, `docs/design.md` and `CONTEXT.md` document `options`, `recommended`, the flags, Grill rounds and the limit of 3.
- AC-11: The round derivation groups Grill Questions Q1, Q2, Q3 (asked before any answer), then Q4, Q5 (asked after answers to Q1 to Q3) into rounds [Q1 Q2 Q3] and [Q4 Q5]; Messages that are not Grill Questions are ignored; a Question answered late still belongs to the round it was asked in.
- AC-12: When one agent turn records three Questions, the Grill screen shows "Q1/3" and "round 1/3"; answering Q1 stores the reply at once and shows Q2 without starting a turn; after Q3 is answered the next turn starts, and its prompt contains all three answers.
- AC-13: After every Question of round 3 is answered, the screen starts no turn on its own, says the limit of 3 rounds is reached, and `f` asks for the summary and `w` writes `plan.md` as today.
- AC-14: Leaving the Grill (esc) or quitting in the middle of a round keeps the stored answers; opening the Grill again shows the remaining open Questions of the same round, with the right "Q<i>/<n>". A Question of the round answered in another clone drops out after sync.
- AC-15: The Grill turn prompt names the current round number and the limit, and with `grill-intent` tells the agent to ask the whole frontier in one turn, give likely answers with `--option` and its recommendation with `--recommend`, and stop after round 3; tests check the prompt text for round 1 and round 3.
- AC-16: `bfc prime grill <id>` shows the Grill so far grouped by round, with the options and recommendation of each Question and the round count out of 3.
