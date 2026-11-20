package tui

import "charm.land/bubbles/v2/key"

// listKeys are the keys of the plan list. Every screen keeps its keys in one
// such map; the help line is generated from it.
type listKeys struct {
	Up, Down, Open, Search, ClearSearch, StateFilter, New, Quit key.Binding
}

func newListKeys() listKeys {
	return listKeys{
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Open:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Search:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		ClearSearch: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear search")),
		StateFilter: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "state")),
		New:         key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q/ctrl+c", "quit")),
	}
}

// ShortHelp lists the keys shown in the help line.
func (k listKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Open, k.Search, k.StateFilter, k.New, k.Quit}
}

// FullHelp is the same as ShortHelp; the list has no long help.
func (k listKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// detailKeys are the keys of the plan detail. The review keys are shown and
// accepted only when the plan allows the action for the current user.
type detailKeys struct {
	SwitchDoc, Edit, Grill, Scroll, Back, Quit key.Binding
	Review                                     map[ReviewAction]key.Binding
}

func newDetailKeys() detailKeys {
	return detailKeys{
		SwitchDoc: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "intent/plan")),
		Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Grill:     key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "grill")),
		// The viewport handles scrolling; this binding only shows in help.
		Scroll: key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "scroll")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Review: map[ReviewAction]key.Binding{
			ActionSubmit:         key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "submit")),
			ActionApprove:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve")),
			ActionRequestChanges: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "changes")),
			ActionClose:          key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "close")),
		},
	}
}

// grillKeys are the keys of the Grill. Enter has one binding with several
// help texts, one for each state. While an answer is typed, letters are
// text, so finishing takes ctrl+f there.
type grillKeys struct {
	Send, Start, Continue, Retry, SendAnswer key.Binding
	Finish, FinishTyping, Write, Yes, No     key.Binding
	Cancel, CancelTurn, Quit                 key.Binding
}

func newGrillKeys() grillKeys {
	return grillKeys{
		Send:         key.NewBinding(key.WithKeys("enter")),
		Start:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "start the Grill")),
		Continue:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "continue")),
		Retry:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "retry")),
		SendAnswer:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send answer")),
		Finish:       key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "finish")),
		FinishTyping: key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "finish")),
		Write:        key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "write plan.md")),
		Yes:          key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "replace plan.md")),
		No:           key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n", "keep it")),
		Cancel:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		CancelTurn:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel turn")),
		Quit:         key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
}
