package main

import (
	"context"
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newAcknowledgeCmd() *cobra.Command {
	var text string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "acknowledge <check-ULID> [-m <reason>]",
		Short: "Accept the Findings of a plan check on purpose, with the reason",
		Long: `Accept the Findings of one plan check on purpose (ADR 0013 section 7): write
a Message with the reason, about the check (ref checks/<ULID>), in the plan
that holds it. Its Findings then no longer wait on anyone; a newer check of
the same pull request needs its own Acknowledgement, and the label on the
pull request goes away only with the next clean check. The reason comes from
-m or, without it, from stdin. Acknowledging is for people: it refuses to run
inside an agent. Prints the ULID of the Message.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			check, err := ulid.ParseStrict(args[0])
			if err != nil {
				return fmt.Errorf("%q is not the ULID of a plan check; bfc show <id> lists the checks of a plan", args[0])
			}
			reason, err := messageText(cmd, text)
			if err != nil {
				return err
			}
			s, err := openPlans(cmd, forWriting)
			if err != nil {
				return err
			}
			plan, err := findCheck(cmd.Context(), s, check)
			if err != nil {
				return err
			}
			v, err := loadPlan(cmd.Context(), s, plan)
			if err != nil {
				return err
			}
			m, err := writeMessage(cmd, s, v, "acknowledge", core.Message{Ref: core.CheckRefOf(check), Body: reason}, false)
			if err != nil {
				return err
			}
			return printMessage(cmd, v, m, asJSON)
		},
	}
	cmd.Flags().StringVarP(&text, "message", "m", "", "the reason (default: read from stdin)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the Message as JSON for scripts")
	return cmd
}

// findCheck returns the plan that holds the plan check with this ULID, or
// an error when no plan does.
func findCheck(ctx context.Context, s *store.Store, id ulid.ULID) (ulid.ULID, error) {
	plan, ok, err := planHolding(ctx, s, func(p ulid.ULID) string { return core.CheckPath(p, id) })
	if err == nil && !ok {
		err = fmt.Errorf("no plan check %s in any plan; bfc show <id> lists the checks of a plan", id)
	}
	return plan, err
}
