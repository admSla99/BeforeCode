package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/config"
	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
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
	opts := tuiOptions(cmd)
	opts.Grill = &tuiGrill{cmd: cmd}
	app := tui.NewApp(&tuiPlans{cmd: cmd}, &tuiSyncer{cmd: cmd}, opts)
	_, err := tea.NewProgram(app, tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.OutOrStdout())).Run()
	return err
}

// tuiOptions reads the sync interval and local mode from the config file.
// Without a repository or a readable config file, the defaults apply.
func tuiOptions(cmd *cobra.Command) tui.AppOptions {
	opts := tui.AppOptions{Interval: config.DefaultSyncInterval}
	if s, err := openStore(cmd); err == nil {
		if set, err := syncSettings(s); err == nil {
			opts.Interval, opts.Local = set.interval, set.local
		}
	}
	return opts
}

// tuiPlans is the Plans adapter of the TUI on the Plans ref. It reads and
// writes plans with the code of the CLI commands, and runs bfc new and
// bfc edit for anything that needs the editor.
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
	return p.runSelf([]string{"new", title}, func(err error) tea.Msg { return tui.PlanCreatedMsg{Err: err} })
}

// Edit runs bfc edit on the document, which opens the editor while the
// screen is suspended.
// Edit reports Changed when the Plans ref moved, so that an editor closed
// without a change does not count as a local change.
func (p *tuiPlans) Edit(shortID string, doc tui.Doc) tea.Cmd {
	args := []string{"edit", shortID}
	if doc == tui.DocIntent {
		args = append(args, "--intent")
	}
	before := p.tip()
	return p.runSelf(args, func(err error) tea.Msg {
		return tui.PlanEditedMsg{Err: err, Changed: err == nil && p.tip() != before}
	})
}

// tip is the commit of the Plans ref, or "" when it cannot be read.
func (p *tuiPlans) tip() string {
	s, err := openStore(p.cmd)
	if err != nil {
		return ""
	}
	tip, _ := s.Tip(context.Background())
	return tip
}

// runSelf runs this bfc with args while the screen is suspended. A failure
// carries the last line the command printed to stderr.
// It runs in the place of the TUI's command.
func (p *tuiPlans) runSelf(args []string, done func(error) tea.Msg) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return done(err) }
	}
	var stderr bytes.Buffer
	c := exec.Command(self, args...)
	if pl := placeOf(p.cmd); pl.dir != "" || len(pl.env) > 0 {
		c.Dir = pl.dir
		c.Env = append(os.Environ(), pl.env...)
	}
	c.Stderr = io.MultiWriter(os.Stderr, &stderr)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
			if last := strings.TrimPrefix(lines[len(lines)-1], "Error: "); last != "" {
				err = errors.New(last)
			}
		}
		return done(err)
	})
}

// reviewEvents maps the TUI's review actions to their events.
var reviewEvents = map[tui.ReviewAction]core.EventType{
	tui.ActionSubmit:         core.Submitted,
	tui.ActionApprove:        core.Approved,
	tui.ActionRequestChanges: core.ChangesRequested,
	tui.ActionClose:          core.Closed,
}

// Detail returns one plan with the review actions the current user may
// take, checked with the same rules as the CLI commands.
func (p *tuiPlans) Detail(ctx context.Context, shortID string) (tui.PlanDetail, error) {
	s, err := openPlans(p.cmd, forReading)
	if err != nil {
		return tui.PlanDetail{}, err
	}
	v, err := resolvePlan(ctx, s, shortID)
	if err != nil {
		return tui.PlanDetail{}, err
	}
	actor, err := currentUser(ctx, s)
	if err != nil {
		return tui.PlanDetail{}, err
	}
	d := tui.PlanDetail{
		ShortID: v.ShortID, Title: v.Title, State: v.Status.State, Author: v.Status.Author.Email,
		RequiredApprovals: v.Status.RequiredApprovals, Approvers: v.Status.Approvers,
		Intent: v.Intent, Plan: v.Plan, Editable: v.Status.State != core.StateClosed,
	}
	// The review actions are Gates, for people only (ADR 0011).
	if checkPerson(p.cmd) != nil {
		return d, nil
	}
	if core.CheckSubmit(v.Status, actor, v.Plan) == nil {
		d.Actions = append(d.Actions, tui.ActionSubmit)
	}
	if core.CheckApprove(v.Status, actor) == nil {
		// An open Question blocks approval, as in writeReview.
		msgs, err := readMessages(ctx, s, v.ID)
		if err != nil {
			return tui.PlanDetail{}, err
		}
		if open := core.OpenQuestions(msgs); core.CheckOpenQuestions(v.Status, open) == nil {
			d.Actions = append(d.Actions, tui.ActionApprove)
		} else {
			d.ApprovalWaitsOn = len(open)
		}
	}
	if core.CheckRequestChanges(v.Status, actor, "a reason") == nil {
		d.Actions = append(d.Actions, tui.ActionRequestChanges)
	}
	if core.CheckClose(v.Status, actor) == nil {
		d.Actions = append(d.Actions, tui.ActionClose)
	}
	return d, nil
}

// Review writes a review event the way the CLI command does.
func (p *tuiPlans) Review(ctx context.Context, shortID string, a tui.ReviewAction, reason string) error {
	if err := checkPerson(p.cmd); err != nil {
		return err
	}
	s, err := openPlans(p.cmd, forWriting)
	if err != nil {
		return err
	}
	_, err = writeReview(ctx, s, shortID, reviewEvents[a], reason)
	return err
}

// tuiSyncer is the Syncer of the TUI: bfc sync without printing, on the
// remote of the config file.
type tuiSyncer struct {
	cmd *cobra.Command
}

// Sync pulls, then pushes, and reports how it went.
func (y *tuiSyncer) Sync(ctx context.Context) tui.SyncResult {
	offline := func(err error) tui.SyncResult {
		return tui.SyncResult{Outcome: tui.SyncOffline, Message: firstLine(err.Error())}
	}
	s, err := openStore(y.cmd)
	if err != nil {
		return offline(err)
	}
	set, err := syncSettings(s)
	switch {
	case err != nil:
		return offline(err)
	case set.local:
		return tui.SyncResult{Outcome: tui.SyncLocalOnly}
	}
	pulled, _, err := s.Sync(ctx, set.remote, store.KeepNeither)
	var ce *store.ConflictError
	var re *store.RemoteError
	switch {
	case errors.As(err, &ce):
		var files []string
		for _, c := range ce.Conflicts {
			if name := path.Base(c.Path); !slices.Contains(files, name) {
				files = append(files, name)
			}
		}
		return tui.SyncResult{Outcome: tui.SyncConflict, Message: strings.Join(files, ", ")}
	case errors.As(err, &re):
		return tui.SyncResult{Outcome: tui.SyncOffline, Message: strings.TrimPrefix(firstLine(re.Message), "fatal: ")}
	case err != nil:
		return offline(err)
	}
	changed := pulled.Outcome == store.PullCreated || pulled.Outcome == store.PullFastForward || pulled.Outcome == store.PullMerged
	return tui.SyncResult{Outcome: tui.SyncOK, Changed: changed}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
