package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

// defaultWaitTimeout keeps a wait inside one call of Claude Code's Bash
// tool, which allows at most 10 minutes (docs/design.md section 3).
const defaultWaitTimeout = 9 * time.Minute

// waitFlags are the flags of bfc wait and bfc ask --wait.
type waitFlags struct {
	timeout  time.Duration
	interval time.Duration // 0: sync.interval of the config file
}

func (f *waitFlags) add(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&f.timeout, "timeout", defaultWaitTimeout, "how long to wait for the answer")
	cmd.Flags().DurationVar(&f.interval, "interval", 0, "time between pulls (default: sync.interval of the config file)")
	_ = cmd.Flags().MarkHidden("interval") // for tests
}

func newWaitCmd() *cobra.Command {
	var f waitFlags
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "wait <message-ULID> [--timeout <duration>]",
		Short: "Wait for the answer to a Question",
		Long: `Wait until the Question with this ULID is answered, pulling every
sync.interval seconds (only the local plans ref in local mode), then print
the answer; --json prints the answering Message. An answered Question returns
at once. After --timeout (default 9m) it exits with code 2 and says how to
keep waiting; any other error exits with code 1.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ulid.ParseStrict(args[0])
			if err != nil {
				return fmt.Errorf("%q is not the ULID of a Message", args[0])
			}
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			plan, err := findMessage(cmd.Context(), s, id)
			if err != nil {
				return err
			}
			v, err := loadPlan(cmd.Context(), s, plan)
			if err != nil {
				return err
			}
			msgs, err := readMessages(cmd.Context(), s, plan)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(msgs, func(m core.Message) bool { return m.ID == id && m.IsQuestion() }) {
				return fmt.Errorf("the Message %s is not a Question, so no answer comes; bfc show %s lists the open Questions", id, v.ShortID)
			}
			return waitForAnswer(cmd, s, v, id, f, asJSON)
		},
	}
	f.add(cmd)
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the answering Message as JSON")
	return cmd
}

// waitForAnswer pulls and reads until the Question q of the plan v is
// answered, then prints the answer: at once, every interval, and once more
// at the deadline. In local mode it only reads. A failed pull is tried again
// at the next interval; when the last pull before the deadline failed too,
// that error is the result (exit 1), not the timeout (exit 2).
func waitForAnswer(cmd *cobra.Command, s *store.Store, v index.Plan, q ulid.ULID, f waitFlags, asJSON bool) error {
	ctx := cmd.Context()
	set, err := syncSettings(s)
	if err != nil {
		return err
	}
	interval := f.interval
	if interval <= 0 {
		interval = set.interval
	}
	deadline := time.Now().Add(f.timeout)
	for {
		var pullErr error
		if !set.local {
			_, pullErr = s.Pull(ctx, set.remote, store.KeepNeither)
		}
		msgs, err := readMessages(ctx, s, v.ID)
		if err != nil {
			return err
		}
		if answer, ok := core.AnswerOf(msgs, q); ok {
			return printAnswer(cmd, v, answer, asJSON)
		}
		left := time.Until(deadline)
		switch {
		case left <= 0 && pullErr != nil:
			return fmt.Errorf("waiting for the answer to %s: %w", q, syncError(cmd, s, pullErr))
		case left <= 0:
			return &exitCodeError{code: 2, err: fmt.Errorf("the Question %s is not answered after %s; keep waiting with bfc wait %s", q, f.timeout, q)}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(interval, left)):
		}
	}
}

// printAnswer prints who answered and the text, or with asJSON the
// answering Message.
func printAnswer(cmd *cobra.Command, v index.Plan, m core.Message, asJSON bool) error {
	if asJSON {
		return printMessage(cmd, v, m, true)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Answer %s from %s:\n%s\n", m.ID, throughAgent(m.Author.Email, m.Agent), m.Body)
	return err
}
