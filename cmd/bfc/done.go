package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
)

func newDoneCmd() *cobra.Command {
	var note string
	var yes bool
	cmd := &cobra.Command{
		Use:   "done <id> [-m <note>] [--yes]",
		Short: "Mark an in-progress plan done, for good",
		Long: `Mark an in-progress plan done (ADR 0013): the work is finished. Done is
final like closed: the plan can no longer be edited, claimed or reviewed,
and bfc list hides it unless asked with --state done or --all. The note, if
given, is stored as a Message of the plan.

bfc done warns, without refusing, when a Task is not completed or a plan
check of a pull request has Findings that nobody acknowledged, and on a
terminal it asks before it writes (--yes does not ask). It is a Gate: a
person runs it, not an agent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			warnings, err := doneWarnings(cmd, args[0])
			if err != nil {
				return err
			}
			w := cmd.ErrOrStderr()
			for _, warning := range warnings {
				_, _ = fmt.Fprintln(w, "warning: "+warning)
			}
			if !yes && stdinIsTerminal(cmd) {
				ok, err := confirm(cmd, fmt.Sprintf("Mark %s done? Done is final. [y/N] ", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("not marked done")
				}
			}
			return runReview(cmd, args[0], core.Done, note)
		},
	}
	cmd.Flags().StringVarP(&note, "message", "m", "", "a note, stored as a Message of the plan")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask before marking it done")
	return cmd
}

// doneWarnings says what is unfinished in the plan: Tasks that are not
// completed and Findings of the newest checks that wait. Refusals are left
// to runReview, so a plan that cannot be done gets no warnings.
func doneWarnings(cmd *cobra.Command, query string) ([]string, error) {
	ctx := cmd.Context()
	s, err := openPlans(cmd, forReading)
	if err != nil {
		return nil, err
	}
	v, err := resolvePlan(ctx, s, query)
	if err != nil || core.CheckDone(v.Status) != nil {
		return nil, nil
	}
	var out []string
	for _, t := range core.TaskViews(v.Status, v.Plan) {
		if t.State != core.TaskCompleted {
			out = append(out, fmt.Sprintf("%s is %s, not completed", t.ID, t.State))
		}
	}
	msgs, err := readMessages(ctx, s, v.ID)
	if err != nil {
		return nil, err
	}
	checks, err := planChecks(ctx, s, v.ID, msgs)
	if err != nil {
		return nil, err
	}
	for _, p := range checks {
		n := 0
		for _, f := range p.Findings {
			if !f.Acknowledged {
				n++
			}
		}
		if n > 0 {
			out = append(out, fmt.Sprintf("pull request #%d has %d Findings that nobody acknowledged", p.Check.PR, n))
		}
	}
	return out, nil
}

// stdinIsTerminal reports whether the command reads from a terminal.
func stdinIsTerminal(cmd *cobra.Command) bool {
	in, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(in.Fd())
}

// confirm asks a yes-or-no question on the terminal.
func confirm(cmd *cobra.Command, question string) (bool, error) {
	_, _ = fmt.Fprint(cmd.ErrOrStderr(), question)
	answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && answer == "" {
		return false, err
	}
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes", nil
}
