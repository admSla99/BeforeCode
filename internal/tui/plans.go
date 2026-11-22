package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// Plans is where the screens get plans from and change them. cmd/bfc
// implements it on the Plans ref with the same code as the CLI; tests use
// a fake.
type Plans interface {
	// Load returns every plan, newest activity first. It returns a
	// *NotSetUpError when the repository has no Plans ref.
	Load(ctx context.Context) ([]PlanRow, error)
	// NewPlan returns a command that creates a plan with this title, the
	// way bfc new does (the editor opens on intent.md), and then produces a
	// PlanCreatedMsg.
	NewPlan(title string) tea.Cmd
	// Detail returns one plan, named by its Short ID.
	Detail(ctx context.Context, shortID string) (PlanDetail, error)
	// Edit returns a command that opens doc of the plan in the editor, the
	// way bfc edit does, and then produces a PlanEditedMsg.
	Edit(shortID string, doc Doc) tea.Cmd
	// Review writes the event of a review action, the way the CLI command
	// of the same name does. reason is used by ActionRequestChanges,
	// ActionClose and ActionDone only; for the last two it may be empty.
	Review(ctx context.Context, shortID string, a ReviewAction, reason string) error
	// Inbox returns what waits on the current user, as bfc inbox does. It
	// returns a *NotSetUpError when the repository has no Plans ref.
	Inbox(ctx context.Context) (core.Waiting, error)
}

// PlanRow is what the plan list shows of one Plan.
type PlanRow struct {
	ShortID      string
	State        core.State
	Title        string
	Author       string // email; empty for an invalid plan
	LastActivity time.Time
}

// PlanDetail is what the plan detail shows of one Plan.
type PlanDetail struct {
	ShortID           string
	Title             string
	State             core.State
	Author            string   // email
	RequiredApprovals int      // 0 outside a review round
	Approvers         []string // emails of the valid Approvals
	Intent, Plan      []byte   // nil when missing
	// Actions are the review actions the current user may take now.
	Actions []ReviewAction
	// BlockingQuestions is the number of open Questions that alone keep the
	// current user from approving; 0 when they could not approve anyway.
	BlockingQuestions int
	// Editable is true when the documents may be edited (not closed or
	// done).
	Editable bool
	// DoneWarnings are what is unfinished when the plan is marked done:
	// Tasks not completed, Findings nobody acknowledged.
	DoneWarnings []string
	// Tasks are the Tasks of plan.md in its order, with their state.
	Tasks []TaskRow
}

// TaskRow is what the tasks tab shows of one Task.
type TaskRow struct {
	ID       string
	Text     string
	State    core.TaskState
	Holder   string // the name of the holder, or of the last one; "" while open
	Mine     bool   // the current user holds it
	Latest   string // the first line of the latest Handoff, "" without one
	Handoffs []HandoffRow
}

// HandoffRow is one Handoff of a Task, oldest first.
type HandoffRow struct {
	ID   string
	By   string // the author, with the harness when an agent wrote it
	Done bool   // completed; released otherwise
	Note string
}

// Doc names one of the two documents of a Plan.
type Doc int

// The documents of a Plan, and the tasks tab of the detail, which is no
// document.
const (
	DocPlan Doc = iota
	DocIntent
	DocTasks
)

// next is the tab the Tab key switches to: from plan.md, where the detail
// opens, to intent.md, then tasks, then back to plan.md.
func (d Doc) next() Doc {
	switch d {
	case DocPlan:
		return DocIntent
	case DocIntent:
		return DocTasks
	}
	return DocPlan
}

// Editable reports whether d is a document the editor opens; the tasks
// tab is not.
func (d Doc) Editable() bool { return d != DocTasks }

func (d Doc) String() string {
	switch d {
	case DocIntent:
		return "intent.md"
	case DocTasks:
		return "tasks"
	}
	return "plan.md"
}

// ReviewAction is a review command, named like the CLI command.
type ReviewAction string

// The review actions of the TUI, in the order the help line lists them.
const (
	ActionSubmit         ReviewAction = "submit"
	ActionApprove        ReviewAction = "approve"
	ActionRequestChanges ReviewAction = "request-changes"
	ActionClose          ReviewAction = "close"
	ActionDone           ReviewAction = "done"
)

var reviewActionOrder = []ReviewAction{ActionSubmit, ActionApprove, ActionRequestChanges, ActionClose, ActionDone}

// PlanCreatedMsg reports the end of a NewPlan command. Err is nil when a
// plan was created.
type PlanCreatedMsg struct{ Err error }

// PlanEditedMsg reports the end of an Edit command. Changed is true when a
// new version of the document was committed.
type PlanEditedMsg struct {
	Err     error
	Changed bool
}

// NotSetUpError means the repository has no Plans ref. Advice, when set, is
// the CLI's precise advice, for example that the remote already has plans.
type NotSetUpError struct{ Advice string }

func (e *NotSetUpError) Error() string {
	if e.Advice == "" {
		return "BeforeCode is not set up in this repository"
	}
	return e.Advice
}
