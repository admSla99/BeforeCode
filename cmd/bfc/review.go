package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/config"
	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newSubmitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "submit <id>",
		Short: "Submit a draft plan for review",
		Long: `Submit a draft plan for review. plan.md must have the required sections and
at least one acceptance criterion. The number of Approvals the plan needs is
read from approval.required_approvals in .beforecode/config.yaml now (1
without a config file) and stays fixed for this review round.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReview(cmd, args[0], core.Submitted, "")
		},
	}
}

func newApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve the current plan.md of a plan in review",
		Long: `Approve the current version of plan.md of a plan in review. The plan author
cannot approve their own plan. An Approval counts only while plan.md stays
exactly as approved; after an edit the plan needs new Approvals.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReview(cmd, args[0], core.Approved, "")
		},
	}
}

func newRequestChangesCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "request-changes <id> -m <reason>",
		Short: "Send a plan in review back to draft, with the reason",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReview(cmd, args[0], core.ChangesRequested, reason)
		},
	}
	cmd.Flags().StringVarP(&reason, "message", "m", "", "the reason, stored as a Message of the plan (required)")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

// runReview checks that the event fits the plan and its author, writes it
// (with a Message for the reason of changes-requested) as one commit, and
// prints the new state.
func runReview(cmd *cobra.Command, query string, typ core.EventType, reason string) error {
	ctx := cmd.Context()
	s, err := openPlans(cmd, forWriting)
	if err != nil {
		return err
	}
	v, err := resolvePlan(ctx, s, query)
	if err != nil {
		return err
	}
	name, email, err := s.Author(ctx)
	if err != nil {
		return err
	}
	actor := core.Person{Name: name, Email: email}

	required := 0
	switch typ {
	case core.Submitted:
		err = core.CheckSubmit(v.Status, actor, v.Plan)
		if err == nil {
			required, err = requiredApprovals(s)
		}
	case core.Approved:
		err = core.CheckApprove(v.Status, actor)
	case core.ChangesRequested:
		err = core.CheckRequestChanges(v.Status, actor, reason)
	}
	if err != nil {
		return err
	}
	blob, err := s.ObjectID(ctx, core.PlanDir(v.ID)+"/plan.md")
	switch {
	case errors.Is(err, store.ErrNotFound) && typ == core.Approved:
		return fmt.Errorf("%s has no plan.md to approve; run bfc doctor", v.ShortID)
	case errors.Is(err, store.ErrNotFound):
		blob = ""
	case err != nil:
		return err
	}

	now := time.Now()
	entropy := ulid.Monotonic(rand.Reader, 0)
	newID := func() ulid.ULID { return ulid.MustNew(ulid.Timestamp(now), entropy) }
	dir := core.PlanDir(v.ID)
	files := map[string][]byte{}
	e := core.Event{Type: typ, Plan: v.ID, Author: actor, At: now, PlanBlob: blob, RequiredApprovals: required}
	if typ == core.ChangesRequested {
		msg := newID()
		files[dir+"/messages/"+msg.String()+".md"] = core.EncodeMessage(core.Message{Author: actor, At: now, Body: reason})
		e.Message = msg.String()
	}
	e.ID = newID()
	files[dir+"/events/"+e.ID.String()+".yaml"] = core.EncodeEvent(e)
	if _, err := s.Write(ctx, fmt.Sprintf("bfc %s %s", command(typ), v.ShortID), files); err != nil {
		return err
	}

	after, err := loadPlan(ctx, s, v.ID)
	if err != nil {
		return err
	}
	return reportReview(cmd, v.ShortID, after.Status, typ)
}

// command is the bfc command that writes an event type.
func command(typ core.EventType) string {
	switch typ {
	case core.Submitted:
		return "submit"
	case core.Approved:
		return "approve"
	default:
		return string(typ)
	}
}

func reportReview(cmd *cobra.Command, short string, st core.Status, typ core.EventType) error {
	detail := ""
	switch {
	case typ == core.ChangesRequested:
		detail = "changes requested"
	case typ == core.Submitted:
		detail = fmt.Sprintf("needs %d approval", st.RequiredApprovals)
		if st.RequiredApprovals != 1 {
			detail += "s"
		}
	case st.RequiredApprovals > 0:
		detail = fmt.Sprintf("%d of %d approvals", len(st.Approvers), st.RequiredApprovals)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s  %s (%s)\n", short, st.State, detail)
	return err
}

// requiredApprovals is the Approval policy in force now:
// approval.required_approvals from the config file, or 1 without one.
func requiredApprovals(s *store.Store) (int, error) {
	cfg, found, err := readConfig(s)
	if err != nil || !found {
		return 1, err
	}
	return cfg.RequiredApprovals, nil
}

// readConfig reads .beforecode/config.yaml from the working tree. found is
// false when there is no such file.
func readConfig(s *store.Store) (cfg config.Config, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(s.Root(), filepath.FromSlash(config.Path)))
	if errors.Is(err, fs.ErrNotExist) {
		return config.Config{}, false, nil
	}
	if err != nil {
		return config.Config{}, false, err
	}
	cfg, err = config.Parse(data)
	return cfg, err == nil, err
}
