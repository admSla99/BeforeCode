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
	// of the same name does. reason is used by ActionRequestChanges and
	// ActionClose only; for ActionClose it may be empty.
	Review(ctx context.Context, shortID string, a ReviewAction, reason string) error
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
	// Editable is true when the documents may be edited (not closed).
	Editable bool
}

// Doc names one of the two documents of a Plan.
type Doc int

// The documents of a Plan.
const (
	DocPlan Doc = iota
	DocIntent
)

// other is the document the Tab key switches to.
func (d Doc) other() Doc {
	if d == DocIntent {
		return DocPlan
	}
	return DocIntent
}

func (d Doc) String() string {
	if d == DocIntent {
		return "intent.md"
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
)

var reviewActionOrder = []ReviewAction{ActionSubmit, ActionApprove, ActionRequestChanges, ActionClose}

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
