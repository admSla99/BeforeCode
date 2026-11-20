# ADR 0012: In the TUI Grill the agent records the Questions, the TUI records the answers

Date: 2026-11-20. Status: accepted. Settled with the maintainer through the prototype in issue #62, for the Grill screen (issue #66).

In the TUI Grill each turn of Claude Code must leave exactly one Question on the plan, recorded by the agent itself with `bfc ask` (its only allowed tool), and the TUI then stores the author's typed answer with the same code as `bfc reply --verbatim`. A turn that ends without a recorded Question is an error the author retries; the screen never turns the agent's text into a Question on its own. This keeps one path for Grill Messages, the same as a Grill in Claude Code (`grill-intent`, ADR 0002 section 7): a Question carries `agent` and is the agent's, an answer has none and is the author's own words, so the plan holds the same record whichever way the Grill ran.

## Considered options

- **The TUI records the Question from the agent's text.** Rejected: the screen would have to guess where the question starts and ends in free text, and a Question recorded by the TUI would not be the agent's own words.
- **The agent records the answer too** (`bfc reply --verbatim` as a second tool). Rejected: the author typed the answer into the TUI, which can store it word for word without asking the agent to copy it.

## Consequences

- The agent's session lives only in `.git/beforecode/grill/<plan ULID>.session`. Because every Question and answer is already on the plan, a lost session costs nothing: the next turn starts a new one from `bfc prime grill`.
- An agent that forgets `bfc ask` stops the Grill with an error until a retry records the Question; `grill-intent` tells the agent to record before asking.
