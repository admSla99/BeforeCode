package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal UI (also what bfc without a command does)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !inTerminal(cmd) {
				return errors.New("bfc tui needs a terminal; use bfc list in scripts")
			}
			return runTUI(cmd)
		},
	}
}

// inTerminal reports whether the command reads from and writes to a
// terminal.
func inTerminal(cmd *cobra.Command) bool {
	in, ok1 := cmd.InOrStdin().(*os.File)
	out, ok2 := cmd.OutOrStdout().(*os.File)
	return ok1 && ok2 && term.IsTerminal(in.Fd()) && term.IsTerminal(out.Fd())
}

func runTUI(cmd *cobra.Command) error {
	// Warnings written while the screen is up would garble it; show them
	// after it closes.
	var warnings bytes.Buffer
	stderr := cmd.ErrOrStderr()
	cmd.SetErr(&warnings)
	defer func() {
		cmd.SetErr(stderr)
		_, _ = io.Copy(stderr, &warnings)
	}()
	_, err := tea.NewProgram(tui.NewPlanList(&tuiPlans{cmd: cmd}),
		tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.OutOrStdout())).Run()
	return err
}

// tuiPlans is the Plans adapter of the TUI on the Plans ref. It reads plans
// the way bfc list does and creates them by running bfc new.
type tuiPlans struct {
	cmd *cobra.Command
}

// Load returns every plan, newest activity first, or a *tui.NotSetUpError.
func (p *tuiPlans) Load(ctx context.Context) ([]tui.PlanRow, error) {
	s, err := openPlans(p.cmd, forReading)
	var nse *notSetUpError
	switch {
	case errors.As(err, &nse) && nse.remoteHasPlans:
		return nil, &tui.NotSetUpError{Advice: nse.Error()}
	case errors.As(err, &nse):
		return nil, &tui.NotSetUpError{}
	case err != nil:
		return nil, err
	}
	views, err := loadPlans(ctx, s)
	if err != nil {
		return nil, err
	}
	plans := make([]tui.PlanRow, len(views))
	for i, v := range views {
		plans[i] = tui.PlanRow{
			ShortID: v.ShortID, State: v.Status.State, Title: v.Title,
			Author: v.Status.Author.Email, LastActivity: v.LastActivity,
		}
	}
	return plans, nil
}

// NewPlan runs bfc new with the title, which opens the editor on intent.md
// while the screen is suspended. A failure carries the last line bfc new
// printed to stderr.
func (p *tuiPlans) NewPlan(title string) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return tui.PlanCreatedMsg{Err: err} }
	}
	var stderr bytes.Buffer
	c := exec.Command(self, "new", title)
	c.Stderr = io.MultiWriter(os.Stderr, &stderr)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
			if last := strings.TrimPrefix(lines[len(lines)-1], "Error: "); last != "" {
				err = errors.New(last)
			}
		}
		return tui.PlanCreatedMsg{Err: err}
	})
}
