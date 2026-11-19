package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

// primeTopics are the topics of bfc prime <topic> [id], each printing what
// an agent needs for one task. Later issues add grill (#64) and plan (#65).
var primeTopics = map[string]func(cmd *cobra.Command, args []string) error{}

func newPrimeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prime [topic] [id]",
		Short: "Print the instructions for an agent and the Current plan",
		Long: `Print Markdown for an AI agent: how to work with bfc, what waits on the
git author of this clone, then the Current plan (or the plan given by ID)
with its open Questions and plan.md, or intent.md while there is no plan.md.
The output is capped at 8 kB. bfc prime reads only this clone and never
fails over plans: what it cannot show it says.

A topic as the first argument prints the instructions for one task instead.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				if topic, ok := primeTopics[args[0]]; ok {
					return topic(cmd, args[1:])
				}
				if len(args) == 2 {
					known := "none yet"
					if len(primeTopics) > 0 {
						known = strings.Join(slices.Sorted(maps.Keys(primeTopics)), ", ")
					}
					return fmt.Errorf("unknown topic %q; known topics: %s", args[0], known)
				}
			}
			return renderPrime(cmd.OutOrStdout(), gatherPrime(cmd, args))
		},
	}
}

// gatherPrime collects what bfc prime prints. Problems become part of the
// view instead of errors.
func gatherPrime(cmd *cobra.Command, args []string) primeView {
	ctx := cmd.Context()
	s, err := openStore(cmd)
	if err != nil {
		return primeView{Setup: err.Error()}
	}
	// Only the local ref: openPlans would ask the remote about a missing one.
	if _, err := s.Read(ctx, "format-version"); errors.Is(err, store.ErrNotInitialised) {
		return primeView{Setup: errNotInitialised.Error()}
	}
	if s, err = openPlans(cmd, forReading); err != nil {
		return primeView{Setup: err.Error()}
	}
	var v primeView
	v.Identity, _ = currentUser(ctx, s)
	views, err := loadPlans(ctx, s)
	if err != nil {
		return primeView{Setup: err.Error()}
	}
	var p index.Plan
	if len(args) == 1 {
		p, err = resolvePlan(ctx, s, args[0])
	} else {
		p, err = currentPlan(ctx, s)
		v.Current = true
	}
	switch {
	case errors.Is(err, errNoCurrentPlan):
	case err != nil:
		v.Note = err.Error()
	default:
		v.Plan = &p
	}
	for _, pv := range views {
		if pv.Status.State == core.StateClosed || pv.Status.State == core.StateInvalid {
			continue
		}
		msgs, err := readMessages(ctx, s, pv.ID)
		if err != nil {
			continue
		}
		open := core.OpenQuestions(msgs)
		if v.Plan != nil && pv.ID == v.Plan.ID {
			v.Open = open
		}
		if pv.Status.State == core.StateInReview && core.CheckApprove(pv.Status, v.Identity) == nil && len(open) == 0 {
			v.Reviews = append(v.Reviews, planRef{pv.ShortID, pv.Title})
		}
		for _, q := range open {
			if slices.ContainsFunc(q.To, func(to string) bool { return strings.EqualFold(to, v.Identity.Email) }) {
				v.Questions = append(v.Questions, waitingQuestion{pv.ShortID, q})
			}
		}
		if v.Plan == nil && pv.Status.State == core.StateApproved {
			v.Approved = append(v.Approved, planRef{pv.ShortID, pv.Title})
		}
	}
	return v
}

// primeInstructions is how an agent works with bfc. bfc prime is the only
// source of these instructions (ADR 0008); the beforecode skill only points
// to it.
const primeInstructions = "# BeforeCode\n\n" +
	"This repository plans its work with BeforeCode (`bfc`). Every change is made against a Plan: " +
	"a short `intent.md` written by a person and a `plan.md` written for agents, with acceptance criteria `AC-1`, `AC-2`, …\n\n" +
	"## How to work with bfc\n\n" +
	"- The user works in the TUI (`bfc` without arguments) and does not run the CLI. You run it, with `--json` where you read.\n" +
	"- Commands you may use:\n" +
	"  - `bfc prime [id]`: these instructions and the Current plan.\n" +
	"  - `bfc show [id] --json`: one plan with `intent.md`, `plan.md`, its acceptance criteria and open Questions.\n" +
	"  - `bfc list --json` and `bfc search <text>`: find plans.\n" +
	"  - `bfc ask <id> --to <email|ai> [--ref plan.md#AC-n] -m <text>`: ask a Question.\n" +
	"  - `bfc reply <message-ULID> -m <text>`: answer a Question; add `--verbatim` when you record the user's own words.\n" +
	"  - `bfc start <id>`: only when the user asks to work on a plan; it switches the branch, which makes the plan the Current plan.\n" +
	"- Gates are for people: `submit`, `approve`, `request-changes`, `withdraw` and `close` refuse to run inside an agent. " +
	"When a plan needs one, tell the user to open `bfc` and take it there.\n" +
	"- Edits need an approved plan: a hook blocks file edits unless the Current plan is `approved`. Do not change code before that.\n" +
	"- Implement `plan.md` as approved. When the work must depart from it, record the deviation with " +
	"`bfc ask <id> --to <plan author email> --ref plan.md#AC-n -m <what and why>` and tell the user.\n"

// primeLimit caps the output of bfc prime; a longer plan.md is cut.
const primeLimit = 8 << 10

// primeView is what bfc prime prints, gathered by runPrime.
type primeView struct {
	// Setup says how to set BeforeCode up when the clone has no plans ref;
	// then nothing else is known.
	Setup    string
	Identity core.Person
	// Reviews are the plans in review this user may approve now, and
	// Questions the open Questions addressed to this user.
	Reviews   []planRef
	Questions []waitingQuestion
	// Plan is the Current plan (Current) or the one given, with its open
	// Questions; nil when there is none.
	Plan    *index.Plan
	Current bool
	Open    []core.Message
	// Note says why the plan asked for could not be shown.
	Note string
	// Approved are the approved plans, listed when there is no plan.
	Approved []planRef
}

type planRef struct{ ShortID, Title string }

type waitingQuestion struct {
	ShortID string
	Q       core.Message
}

// renderPrime prints v as Markdown for an agent, at most primeLimit bytes
// unless the parts other than plan.md are longer.
func renderPrime(w io.Writer, v primeView) error {
	var head strings.Builder
	head.WriteString(primeInstructions)
	if v.Setup != "" {
		fmt.Fprintf(&head, "\n## Setup\n\n%s\n", v.Setup)
		_, err := io.WriteString(w, head.String())
		return err
	}
	writeWaiting(&head, v)
	if v.Plan == nil {
		writeNoPlan(&head, v)
		_, err := io.WriteString(w, head.String())
		return err
	}
	doc, name := v.Plan.Plan, "plan.md"
	if doc == nil {
		doc, name = v.Plan.Intent, "intent.md"
	}
	writePlanHeader(&head, v, name)
	body := string(doc)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	fence := fenceFor(body)
	opening, closing := fence+"markdown\n", fence+"\n"
	if room := primeLimit - head.Len() - len(opening) - len(closing); len(body) > room {
		cut := fmt.Sprintf("\n%s is cut here; run `bfc show %s` for all of it.\n", name, v.Plan.ShortID)
		room -= len(cut)
		body = body[:strings.LastIndex(body[:max(room, 0)], "\n")+1]
		closing += cut
	}
	_, err := io.WriteString(w, head.String()+opening+body+closing)
	return err
}

// writeWaiting prints what waits on the user whose git identity bfc uses.
func writeWaiting(b *strings.Builder, v primeView) {
	fmt.Fprintf(b, "\n## Waiting on you\n\nFor %s <%s>, the git author of this clone, who runs you:\n\n", orDash(v.Identity.Name), orDash(v.Identity.Email))
	if len(v.Reviews) == 0 && len(v.Questions) == 0 {
		b.WriteString("Nothing waits on you.\n")
		return
	}
	for _, r := range v.Reviews {
		fmt.Fprintf(b, "- Review %s %s: tell the user to open `bfc`, select it and press `a` to approve.\n", r.ShortID, r.Title)
	}
	for _, q := range v.Questions {
		fmt.Fprintf(b, "- Question %s in %s from %s%s: %s\n", q.Q.ID, q.ShortID, q.Q.Author.Email, refSuffix(q.Q.Ref), firstLine(q.Q.Body))
	}
	if len(v.Questions) > 0 {
		b.WriteString("\nAsk the user and record the answer with `bfc reply <ULID> --verbatim -m <their words>`.\n")
	}
}

func writeNoPlan(b *strings.Builder, v primeView) {
	b.WriteString("\n## Current plan\n\n")
	if v.Note != "" {
		b.WriteString(v.Note + "\n\n")
	} else {
		b.WriteString("There is no Current plan.\n\n")
	}
	if len(v.Approved) == 0 {
		b.WriteString("No plan is approved yet.\n")
	} else {
		b.WriteString("Approved plans:\n\n")
		for _, p := range v.Approved {
			fmt.Fprintf(b, "- %s %s\n", p.ShortID, p.Title)
		}
	}
	b.WriteString("\nTo work on a plan, the user asks you to run `bfc start <id>`, which switches to its branch.\n")
}

func writePlanHeader(b *strings.Builder, v primeView, doc string) {
	p := v.Plan
	which := "Plan"
	if v.Current {
		which = "Current plan"
	}
	fmt.Fprintf(b, "\n## %s: %s %s\n\n", which, p.ShortID, p.Title)
	fmt.Fprintf(b, "- State: %s. %s\n", p.Status.State, stateAdvice(p.Status.State))
	fmt.Fprintf(b, "- Author: %s <%s>\n", orDash(p.Status.Author.Name), orDash(p.Status.Author.Email))
	if p.Status.RequiredApprovals > 0 {
		fmt.Fprintf(b, "- Approvals: %d of %d", len(p.Status.Approvers), p.Status.RequiredApprovals)
		if len(p.Status.Approvers) > 0 {
			fmt.Fprintf(b, " (%s)", strings.Join(p.Status.Approvers, ", "))
		}
		b.WriteString("\n")
	}
	if len(v.Open) > 0 {
		b.WriteString("\n### Open Questions\n\n")
		for _, q := range v.Open {
			fmt.Fprintf(b, "- %s to %s%s: %s\n", q.ID, strings.Join(q.To, ", "), refSuffix(q.Ref), firstLine(q.Body))
		}
	}
	fmt.Fprintf(b, "\n### %s\n\n", doc)
}

// stateAdvice says what an agent can do with a plan in this state.
func stateAdvice(s core.State) string {
	switch s {
	case core.StateApproved, core.StateInProgress:
		return "You may implement it."
	case core.StateInReview:
		return "It waits for approval; do not edit code yet."
	case core.StateDraft:
		return "It is not approved; do not edit code yet."
	case core.StateDone:
		return "It is done."
	case core.StateClosed:
		return "It is closed; do not work on it."
	default:
		return "It is not a valid plan; run `bfc doctor`."
	}
}

func refSuffix(ref string) string {
	if ref == "" {
		return ""
	}
	return " about " + ref
}

// fenceFor returns a backtick fence longer than any in doc.
func fenceFor(doc string) string {
	longest, run := 0, 0
	for _, r := range doc {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}
