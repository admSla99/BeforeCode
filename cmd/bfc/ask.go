package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newAskCmd() *cobra.Command {
	var to []string
	var ref, text string
	var asJSON, wait bool
	var wf waitFlags
	cmd := &cobra.Command{
		Use:   "ask <id> --to <email|ai>... [--ref <ref>] [-m <text>]",
		Short: "Ask a Question on a plan",
		Long: `Ask a Question on a plan: a Message addressed to people (by email) and/or to
the AI (ai). It stays open until one of them answers with bfc reply, and an
open Question blocks bfc approve. --ref names the part of the plan it is
about: intent.md, plan.md, plan.md#<heading anchor>, plan.md#AC-<n> or
checks/<check ULID>. The
text comes from -m or, without it, from stdin. Prints the ULID of the
Question; bfc reply answers it by that ULID.

With --wait it pushes the Question and waits for the answer like bfc wait:
it prints the answer, or after --timeout (default 9m) exits with code 2.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(to) == 0 {
				return errors.New("say whom the Question is for with --to <email> or --to ai")
			}
			for i, a := range to {
				if to[i] = addressee(a); to[i] == "" {
					return fmt.Errorf("--to %q is neither an email address nor ai", a)
				}
			}
			if ref != "" {
				if err := core.CheckRef(ref); err != nil {
					return err
				}
			}
			body, err := messageText(cmd, text)
			if err != nil {
				return err
			}
			s, err := openPlans(cmd, forWriting)
			if err != nil {
				return err
			}
			v, err := resolvePlan(cmd.Context(), s, args[0])
			if err != nil {
				return err
			}
			m, err := writeMessage(cmd, s, v, "ask", core.Message{To: to, Ref: ref, Body: body}, false)
			if err != nil {
				return err
			}
			if !wait {
				return printMessage(cmd, v, m, asJSON)
			}
			set, err := syncSettings(s)
			if err != nil {
				return err
			}
			if !set.local {
				if _, err := s.Push(cmd.Context(), set.remote); err != nil {
					return fmt.Errorf("the Question %s is written but not shared; run bfc push, then bfc wait %s: %w", m.ID, m.ID, syncError(cmd, s, err))
				}
			}
			return waitForAnswer(cmd, s, v, m.ID, wf, asJSON)
		},
	}
	cmd.Flags().StringSliceVar(&to, "to", nil, "who answers: an email address or ai (repeat or separate with commas)")
	cmd.Flags().StringVar(&ref, "ref", "", "the part of the plan the Question is about")
	cmd.Flags().StringVarP(&text, "message", "m", "", "the text of the Question (default: read from stdin)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the Message as JSON for scripts and agents")
	cmd.Flags().BoolVar(&wait, "wait", false, "push the Question and wait for its answer, then print it (see bfc wait)")
	wf.add(cmd)
	return cmd
}

func newReplyCmd() *cobra.Command {
	var text string
	var verbatim, asJSON bool
	cmd := &cobra.Command{
		Use:   "reply <message-ULID> [-m <text>]",
		Short: "Reply to a Message, such as a Question",
		Long: `Reply to a Message of any plan, named by its ULID. A reply from someone a
Question is addressed to answers it. Inside an agent the reply is marked as
written by the agent; with --verbatim it is not, for an answer the agent
records word for word for the person running it, so that it answers a
Question addressed to that person. The text comes from -m or, without it,
from stdin. Prints the ULID of the reply.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ulid.ParseStrict(args[0])
			if err != nil {
				return fmt.Errorf("%q is not the ULID of a Message", args[0])
			}
			body, err := messageText(cmd, text)
			if err != nil {
				return err
			}
			s, err := openPlans(cmd, forWriting)
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
			m, err := writeMessage(cmd, s, v, "reply", core.Message{ReplyTo: id.String(), Body: body}, verbatim)
			if err != nil {
				return err
			}
			return printMessage(cmd, v, m, asJSON)
		},
	}
	cmd.Flags().StringVarP(&text, "message", "m", "", "the text of the reply (default: read from stdin)")
	cmd.Flags().BoolVar(&verbatim, "verbatim", false, "inside an agent: the text is the person's own words, not the agent's")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the Message as JSON for scripts and agents")
	return cmd
}

// addressee returns a, trimmed, when it is a bare email address, or core.AI
// for the word ai in any case. Otherwise it returns "": a Question to
// anything else could never be answered.
func addressee(a string) string {
	a = strings.TrimSpace(a)
	if strings.EqualFold(a, core.AI) {
		return core.AI
	}
	if addr, err := mail.ParseAddress(a); err == nil && addr.Name == "" && addr.Address == a {
		return a
	}
	return ""
}

// messageText is the text of -m when given, otherwise stdin. A terminal on
// stdin is refused rather than waited on.
func messageText(cmd *cobra.Command, text string) (string, error) {
	if !cmd.Flags().Changed("message") {
		if in, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(in.Fd()) {
			return "", errors.New("give the text with -m or pipe it on stdin")
		}
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		text = string(data)
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("the text is empty; give it with -m or on stdin")
	}
	return text, nil
}

// writeMessage completes m with the current user, the time, a new ULID and,
// inside an agent, the agent (unless verbatim: the text is the person's own
// words), and writes it as one new file in one commit.
func writeMessage(cmd *cobra.Command, s *store.Store, v index.Plan, command string, m core.Message, verbatim bool) (core.Message, error) {
	ctx := cmd.Context()
	short := v.ShortID
	if err := core.CheckWritable(v.Status); err != nil {
		return core.Message{}, err
	}
	actor, err := currentUser(ctx, s)
	if err != nil {
		return core.Message{}, err
	}
	m.Author, m.At = actor, time.Now()
	if !verbatim {
		m.Agent = insideAgent(cmd)
	}
	m.ID = ulid.MustNew(ulid.Timestamp(m.At), rand.Reader)
	files := map[string][]byte{core.MessagePath(v.ID, m.ID): core.EncodeMessage(m)}
	if _, err := s.Write(ctx, "bfc "+command+" "+short, files); err != nil {
		return core.Message{}, err
	}
	// As stored: whole seconds, trimmed body.
	return core.ParseMessage(m.ID, core.EncodeMessage(m))
}

// findMessage returns the plan that holds the Message with this ULID, or
// an error when no plan does.
func findMessage(ctx context.Context, s *store.Store, id ulid.ULID) (ulid.ULID, error) {
	plan, ok, err := planHolding(ctx, s, func(p ulid.ULID) string { return core.MessagePath(p, id) })
	if err == nil && !ok {
		err = fmt.Errorf("no Message %s in any plan; bfc show <id> lists the open Questions of a plan", id)
	}
	return plan, err
}

// planHolding returns the plan whose file path(plan) exists at the tip of
// the plans ref; ok is false when no plan has it. A file named by its own
// ULID is in at most one plan.
func planHolding(ctx context.Context, s *store.Store, path func(plan ulid.ULID) string) (plan ulid.ULID, ok bool, err error) {
	tip, err := s.Tip(ctx)
	if err != nil {
		return ulid.ULID{}, false, err
	}
	plans, err := listPlanIDs(ctx, s)
	if err != nil {
		return ulid.ULID{}, false, err
	}
	paths := make([]string, len(plans))
	for i, p := range plans {
		paths[i] = path(p)
	}
	found, err := s.Existing(ctx, tip, paths)
	if err != nil || len(found) == 0 {
		return ulid.ULID{}, false, err
	}
	dir := strings.TrimPrefix(found[0], core.PlansDir+"/")
	return ulid.MustParseStrict(dir[:strings.Index(dir, "/")]), true, nil
}

// readMessages returns the Messages of a plan at the tip of the plans ref,
// in ULID order. A file that cannot be read as a Message is skipped.
func readMessages(ctx context.Context, s *store.Store, plan ulid.ULID) ([]core.Message, error) {
	files, err := readPlanFiles(ctx, s, plan, "messages", ".md")
	if err != nil {
		return nil, err
	}
	var msgs []core.Message
	for _, f := range files {
		if m, err := core.ParseMessage(f.id, f.data); err == nil {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

// messageJSON is the --json shape of a Message, documented in
// docs/design.md.
type messageJSON struct {
	ID      string      `json:"id"`
	Plan    string      `json:"plan"`
	ShortID string      `json:"short_id"`
	Author  core.Person `json:"author"`
	At      string      `json:"at"`
	Agent   string      `json:"agent,omitempty"`
	To      []string    `json:"to,omitempty"`
	ReplyTo string      `json:"reply_to,omitempty"`
	Ref     string      `json:"ref,omitempty"`
	Body    string      `json:"body"`
}

// printMessage prints the ULID of m, or with asJSON the whole Message.
func printMessage(cmd *cobra.Command, v index.Plan, m core.Message, asJSON bool) error {
	if !asJSON {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), m.ID)
		return err
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(toMessageJSON(v, m))
}

// toMessageJSON is m of the plan v in its --json shape.
func toMessageJSON(v index.Plan, m core.Message) messageJSON {
	return messageJSON{
		ID: m.ID.String(), Plan: v.ID.String(), ShortID: v.ShortID, Author: m.Author,
		At: m.At.UTC().Format(time.RFC3339), Agent: m.Agent, To: m.To, ReplyTo: m.ReplyTo, Ref: m.Ref, Body: m.Body,
	}
}
