# ADR 0012: In the TUI Grill the agent records the Questions, the TUI records the answers

Date: 2026-11-20. Status: accepted, amended 2026-11-21 (issue #92). Settled with the maintainer through the prototype in issue #62, for the Grill screen (issue #66).

In the TUI Grill each turn of Claude Code leaves at most one Question on the plan, recorded by the agent itself with `bfc ask` (the only command it may write with; it may also read the plan with `bfc prime` and `bfc show`), and the TUI then stores the author's typed answer with the same code as `bfc reply --verbatim`. A turn that ends without a recorded Question means the agent has nothing left to ask: the screen shows its text and offers to finish the Grill or to ask again. The screen never turns the agent's text into a Question on its own. This keeps one path for Grill Messages, the same as a Grill in Claude Code (`grill-intent`, ADR 0002 section 7): a Question carries `agent` and is the agent's, an answer has none and is the author's own words, so the plan holds the same record whichever way the Grill ran.

## Considered options

- **The TUI records the Question from the agent's text.** Rejected: the screen would have to guess where the question starts and ends in free text, and a Question recorded by the TUI would not be the agent's own words.
- **The agent records the answer too** (`bfc reply --verbatim` as a second tool). Rejected: the author typed the answer into the TUI, which can store it word for word without asking the agent to copy it.

## Consequences

- The agent's session lives only in `.git/beforecode/grill/<plan ULID>.session`. Because every Question and answer is already on the plan, a lost session costs nothing: the next turn starts a new one from `bfc prime grill`.
- An agent that asks in its text but forgets `bfc ask` looks like one with nothing left to ask; the author presses Enter for another Question. `grill-intent` tells the agent to record before asking.

## Amendment (issue #92)

Originally a turn without a recorded Question was an error, so an agent with nothing left to ask had to record one more Question, "do you want to finish?". The author answered it by finishing the Grill, so it stayed open, and an open Question blocks approval: the reviewer could not approve until the author answered it with `bfc reply`. Now the agent records no Question when nothing is left (the turn prompt and `grill-intent` say so), and such a turn is not an error. The plan detail says how many open Questions keep a reviewer from approving.
