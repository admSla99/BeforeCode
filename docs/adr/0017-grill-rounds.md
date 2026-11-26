# ADR 0017: The Grill asks in rounds, at most 3, with options and a recommended answer

Date: 2026-11-26. Status: accepted. Decided by the maintainer in plan bfc-121s. Amends ADR 0012 (one Question per turn) and adds two optional fields to the Messages of ADR 0002 section 7.

## Context

The Grill asked one Question per turn of Claude Code, and the author typed every answer by hand. Most Grill Questions have a few likely answers the agent already knows, and a Grill of one Question per turn is a long drip of turns, each waiting on Claude Code. The maintainer asked that the Grill work like the `grill-me` / `grilling` skill of mattpocock/skills: in rounds, each round the whole frontier of Questions, each with choices and the agent's recommended answer, and at most 3 rounds.

## Decision

1. **Options and the Recommended answer are stored on the Question.** A Question Message may have `options`, a list of 2 to 5 plain one-line strings, unique within the Question, and `recommended`, the text of one of them. Both are optional; ADR 0002 lets a format add an optional field without a version bump, and older binaries ignore them. `bfc ask` writes them with a repeatable `--option` (no comma splitting) and `--recommend`, and refuses invalid ones before writing.
2. **An answer is always plain text.** Picking an option stores exactly its text as the reply body, recorded like `bfc reply --verbatim`. Nothing records which option was picked or whether it was the recommended one, so `bfc show`, the Discussion and `bfc wait` read the same for a picked or a typed answer.
3. **A Grill round is derived, not stored.** The Grill Questions of a plan (by an agent, to the plan author) are taken in ULID order. A Question starts a new round when the answer to an earlier Grill Question (its first direct reply by the author without `agent`) was written after the Grill Question before it. So every Question the agent records before the next answer is one round, in the TUI and in a Grill in Claude Code alike, and a Question answered late stays in the round it was asked in. `core.GrillRounds` holds the rule.
4. **One turn asks one round.** This replaces the rule of ADR 0012, at most one Question per turn, with at most one round per turn. The agent asks every Question whose prerequisites are settled; a Question that depends on another Question of the same round waits for the next round. It looks up facts itself and asks the author only for decisions; the TUI turn uses no sub-agents.
5. **At most 3 rounds** (`core.MaxGrillRounds`), counted per plan with the rule above. The turn prompt names the round and the limit. Once round 3 is answered the Grill screen starts no asking turn: it says the limit is reached and offers finishing (`f`, then `w`). The agent may stop earlier by recording no Question, as before.
6. **The Grill screen** shows the open Questions of the current round one at a time ("round r/3 · Qi/n"), each answer stored at once. A Question with options is a list: the recommended option marked and selected, a last item "Write my own answer" that opens the answer line; esc in the answer line returns to the list. A Question without options shows the answer line alone. When the round has no open Question, the next turn starts with the round's answers in its prompt.

## Considered options

- **Store the round on the Question** (a `round` field). Rejected: a derived round needs no format change, and a Grill in Claude Code, where the agent writes the Messages itself, gets the same rounds.
- **Record which option was picked** (a field on the answer). Rejected: the answer is the author's words; a second field would make a picked and a typed answer read differently.
- **A configurable round limit.** Rejected for now: 3 is a constant until someone needs another number.
- **A comma-separated `--option a,b`.** Rejected: options may contain commas.

## Consequences

- A turn writes several Questions, so a cancelled turn can leave a partial round. Its Questions wait for their answers like any others, and the next turn starts once they are answered.
- `bfc prime grill` shows the Grill grouped by round, with each Question's options, and says which round is next or that the limit is reached.
- The Discussion tab of another plan (bfc-mmst) can later show options for any Question; the format allows it.
