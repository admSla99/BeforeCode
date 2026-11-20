package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/config"
	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
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

func newWithdrawCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "withdraw <id>",
		Short: "Take your plan back out of review, to draft",
		Long: `Take a plan in review or approved back to draft, to change it without
waiting for a reviewer. Only the plan author can withdraw. The review round
ends: its Approvals no longer count, and the next bfc submit needs new ones.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReview(cmd, args[0], core.Withdrawn, "")
		},
	}
}

func newCloseCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "close <id> [-m <reason>]",
		Short: "Close a plan for good",
		Long: `Close a plan that will not be done, or that was finished outside BeforeCode.
Anyone may close a plan, in any state. Closed is final: the plan can no
longer be edited, submitted or reviewed, and bfc list hides it unless asked
with --state closed or --all. The reason, if given, is stored as a Message
of the plan.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReview(cmd, args[0], core.Closed, reason)
		},
	}
	cmd.Flags().StringVarP(&reason, "message", "m", "", "the reason, stored as a Message of the plan")
	return cmd
}

// reviewAction is one review command: the event type it writes, how it is
// checked, and how its result is reported.
type reviewAction struct {
	command string // the bfc command, also used in the commit message
	// planBlob says whether the event records the plan_blob of plan.md
	// (ADR 0002 section 4).
	planBlob bool
	check    func(v index.Plan, actor core.Person, reason string) error
	// detail describes the plan after the event; nil prints the
	// Approvals.
	detail func(st core.Status) string
}

var reviewActions = map[core.EventType]reviewAction{
	core.Submitted: {
		command: "submit", planBlob: true,
		check: func(v index.Plan, actor core.Person, _ string) error {
			return core.CheckSubmit(v.Status, actor, v.Plan)
		},
		detail: func(st core.Status) string {
			if st.RequiredApprovals == 1 {
				return "needs 1 approval"
			}
			return fmt.Sprintf("needs %d approvals", st.RequiredApprovals)
		},
	},
	core.Approved: {
		command: "approve", planBlob: true,
		check: func(v index.Plan, actor core.Person, _ string) error { return core.CheckApprove(v.Status, actor) },
	},
	core.ChangesRequested: {
		command: "request-changes", planBlob: true,
		check: func(v index.Plan, actor core.Person, reason string) error {
			return core.CheckRequestChanges(v.Status, actor, reason)
		},
		detail: func(core.Status) string { return "changes requested" },
	},
	core.Withdrawn: {
		command: "withdraw",
		check:   func(v index.Plan, actor core.Person, _ string) error { return core.CheckWithdraw(v.Status, actor) },
		detail:  func(core.Status) string { return "withdrawn" },
	},
	core.Closed: {
		command: "close",
		check:   func(v index.Plan, actor core.Person, _ string) error { return core.CheckClose(v.Status, actor) },
		detail:  func(core.Status) string { return "final" },
	},
}

// runReview checks that the event fits the plan and its author, writes it
// (with the reason as a Message for changes-requested, and for closed when
// one is given) as one commit, and prints the new state.
func runReview(cmd *cobra.Command, query string, typ core.EventType, reason string) error {
	s, err := openPlans(cmd, forWriting)
	if err != nil {
		return err
	}
	after, err := writeReview(cmd.Context(), s, query, typ, reason)
	if err != nil {
		return err
	}
	detail := fmt.Sprintf("%d of %d approvals", len(after.Status.Approvers), after.Status.RequiredApprovals)
	if action := reviewActions[typ]; action.detail != nil {
		detail = action.detail(after.Status)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s (%s)\n", after.ShortID, after.Status.State, detail)
	return err
}

// writeReview checks that the event fits the plan and the current user,
// writes it (with the reason as a Message for changes-requested, and for
// closed when one is given) as one commit, and returns the plan afterwards. It prints nothing, so the CLI
// and the TUI share it.
func writeReview(ctx context.Context, s *store.Store, query string, typ core.EventType, reason string) (index.Plan, error) {
	action := reviewActions[typ]
	v, err := resolvePlan(ctx, s, query)
	if err != nil {
		return index.Plan{}, err
	}
	actor, err := currentUser(ctx, s)
	if err != nil {
		return index.Plan{}, err
	}
	// An invalid plan has no stored Short ID; name it in refusals by the
	// one bfc list shows.
	if v.Status.ShortID == "" {
		v.Status.ShortID = v.ShortID
	}
	if err := action.check(v, actor, reason); err != nil {
		return index.Plan{}, err
	}
	if typ == core.Approved {
		// An open Question blocks approval (ADR 0002 section 4).
		msgs, err := readMessages(ctx, s, v.ID)
		if err != nil {
			return index.Plan{}, err
		}
		if err := core.CheckOpenQuestions(v.Status, core.OpenQuestions(msgs)); err != nil {
			return index.Plan{}, err
		}
	}

	now := time.Now()
	e := core.Event{Type: typ, Plan: v.ID, Author: actor, At: now}
	if typ == core.Submitted {
		if e.RequiredApprovals, err = requiredApprovals(s); err != nil {
			return index.Plan{}, err
		}
	}
	if action.planBlob {
		e.PlanBlob, err = s.ObjectID(ctx, core.PlanDir(v.ID)+"/plan.md")
		switch {
		case errors.Is(err, store.ErrNotFound) && typ == core.Approved:
			return index.Plan{}, fmt.Errorf("%s has no plan.md to approve; run bfc doctor", v.ShortID)
		case errors.Is(err, store.ErrNotFound):
			e.PlanBlob = ""
		case err != nil:
			return index.Plan{}, err
		}
	}

	entropy := ulid.Monotonic(rand.Reader, 0)
	newID := func() ulid.ULID { return ulid.MustNew(ulid.Timestamp(now), entropy) }
	dir := core.PlanDir(v.ID)
	files := map[string][]byte{}
	// The reason is a Message: always for changes-requested, and for
	// closed when one is given.
	if typ == core.ChangesRequested || typ == core.Closed && strings.TrimSpace(reason) != "" {
		msg := newID()
		files[core.MessagePath(v.ID, msg)] = core.EncodeMessage(core.Message{Author: actor, At: now, Body: reason})
		e.Message = msg.String()
	}
	e.ID = newID()
	files[dir+"/events/"+e.ID.String()+".yaml"] = core.EncodeEvent(e)
	if _, err := s.Write(ctx, "bfc "+action.command+" "+v.ShortID, files); err != nil {
		return index.Plan{}, err
	}

	after, err := loadPlan(ctx, s, v.ID)
	after.ShortID = v.ShortID
	return after, err
}

// currentUser is the person git names as the commit author.
func currentUser(ctx context.Context, s *store.Store) (core.Person, error) {
	name, email, err := s.Author(ctx)
	return core.Person{Name: name, Email: email}, err
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
	return readConfigIn(s.Root())
}

// readConfigIn reads .beforecode/config.yaml of the repository at root.
func readConfigIn(root string) (cfg config.Config, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(config.Path)))
	if errors.Is(err, fs.ErrNotExist) {
		return config.Config{}, false, nil
	}
	if err != nil {
		return config.Config{}, false, err
	}
	cfg, err = config.Parse(data)
	return cfg, err == nil, err
}
