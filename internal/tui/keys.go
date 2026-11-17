package tui

import "charm.land/bubbles/v2/key"

// listKeys are the keys of the plan list. Every screen keeps its keys in one
// such map; the help line is generated from it.
type listKeys struct {
	Up, Down, Search, ClearSearch, StateFilter, New, Quit key.Binding
}

func newListKeys() listKeys {
	return listKeys{
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Search:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		ClearSearch: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear search")),
		StateFilter: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "state")),
		New:         key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q/ctrl+c", "quit")),
	}
}

// ShortHelp lists the keys shown in the help line.
func (k listKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Search, k.StateFilter, k.New, k.Quit}
}

// FullHelp is the same as ShortHelp; the list has no long help.
func (k listKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }
