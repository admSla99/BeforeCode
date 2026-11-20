// Package agent defines the Agent interface that every harness adapter
// implements (docs/design.md, section 6). The adapters live in
// subpackages, Claude Code first in agent/claude. core does not import this
// package.
//
// Task and Event here are the harness's turn and its output, not the Tasks
// of plan.md or the Events of the Plans ref (CONTEXT.md). A session is the
// harness's own conversation id.
package agent

import (
	"context"
	"encoding/json"
)

// Agent runs one harness headless.
type Agent interface {
	// Name is the harness, such as "claude-code".
	Name() string
	// Capabilities says what the harness supports.
	Capabilities() Caps
	// Run starts one turn of the agent on t and returns its output. The
	// channel closes when the harness has exited. A failure after the
	// start, including a cancelled ctx, arrives as an Error; the error
	// return is only for a harness that cannot be started.
	Run(ctx context.Context, t Task) (<-chan Event, error)
}

// Caps are the capabilities of a harness.
type Caps struct {
	Hooks    bool // it runs bfc hooks, such as PreToolUse
	Resume   bool // a Task can continue an earlier Session
	Headless bool // it runs without a terminal
}

// Task is one turn of an agent.
type Task struct {
	Prompt string
	// Session continues an earlier session of the harness: the Session of
	// the Result of the previous turn. Empty starts a new one.
	Session string
	// AllowedTools are the tools the agent may use without asking, in the
	// harness's own notation (Claude Code: "Read", "Bash(bfc ask:*)").
	AllowedTools []string
	// Dir is the working directory of the agent.
	Dir string
}

// Kind is the type of an Event.
type Kind string

// The kinds of Event.
const (
	Text    Kind = "text"     // the agent wrote text
	ToolUse Kind = "tool_use" // the agent calls a tool
	Result  Kind = "result"   // the turn ended; it carries the Session
	Error   Kind = "error"    // the turn or the harness failed
)

// Event is one item of a harness's output, not a plan Event.
type Event struct {
	Kind Kind
	// Text is the text of a Text Event, the final answer of a Result, and
	// the message of an Error.
	Text string
	// Tool, ToolID and Input describe a ToolUse: the tool's name, the id
	// of the call and its input as JSON.
	Tool   string
	ToolID string
	Input  json.RawMessage
	// Session is the conversation the turn belongs to. A Result always
	// carries it; pass it as Task.Session to continue.
	Session string
	// Err is the cause of an Error.
	Err error
}
