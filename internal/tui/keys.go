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
	SwitchDoc, Edit, Scroll, Back, Quit key.Binding
	Review                              map[ReviewAction]key.Binding
}

func newDetailKeys() detailKeys {
	return detailKeys{
		SwitchDoc: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "intent/plan")),
		Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
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
