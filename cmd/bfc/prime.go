package main

import (
	"context"
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
// an agent needs for one task.
var primeTopics = map[string]func(cmd *cobra.Command, args []string) error{
	"grill": runPrimeGrill,
	"plan":  runPrimePlan,
	"check": runPrimeCheck,
}

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

// openPrime opens the plans of this clone for bfc prime and its topics,
// reading only the local Plans ref. setup says why it cannot, "" when it
// can.
func openPrime(cmd *cobra.Command) (s *store.Store, setup string) {
	s, err := openStore(cmd)
	if err != nil {
		return nil, err.Error()
	}
	// Only the local ref: openPlans would ask the remote about a missing one.
	if _, err := s.Read(cmd.Context(), "format-version"); errors.Is(err, store.ErrNotInitialised) {
		return nil, errNotInitialised.Error()
	}
	if s, err = openPlans(cmd, forReading); err != nil {
		return nil, err.Error()
	}
	return s, ""
}

// primePlan returns the plan named by args, or the Current plan without
// one; current says which. Without a Current plan it fails with
// errNoCurrentPlan.
func primePlan(ctx context.Context, s *store.Store, args []string) (p index.Plan, current bool, err error) {
	if len(args) == 1 {
		p, err = resolvePlan(ctx, s, args[0])
		return p, false, err
	}
	p, err = currentPlan(ctx, s)
	return p, true, err
}

// gatherPrime collects what bfc prime prints. Problems become part of the
// view instead of errors.
func gatherPrime(cmd *cobra.Command, args []string) primeView {
	ctx := cmd.Context()
	s, setup := openPrime(cmd)
	if setup != "" {
		return primeView{Setup: setup}
	}
	var v primeView
	v.Identity, _ = currentUser(ctx, s)
	views, err := loadPlans(ctx, s)
	if err != nil {
		return primeView{Setup: err.Error()}
	}
	p, current, err := primePlan(ctx, s, args)
	v.Current = current
	switch {
	case errors.Is(err, errNoCurrentPlan):
	case err != nil:
		v.Note = err.Error()
	default:
		v.Plan = &p
	}
	if v.Plan != nil {
		if msgs, err := readMessages(ctx, s, v.Plan.ID); err == nil {
			v.Open = core.OpenQuestions(msgs)
		}
	}
	v.Waiting = gatherWaiting(ctx, s, v.Identity, views)
	for _, pv := range views {
		if v.Plan == nil && pv.Status.State == core.StateApproved {
			v.Approved = append(v.Approved, core.PlanRef{ID: pv.ID, ShortID: pv.ShortID, Title: pv.Title})
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
	"  - `bfc show [id] --json`: one plan with `intent.md`, `plan.md`, its acceptance criteria, open Questions and every Message (`messages`).\n" +
	"  - `bfc list --json` and `bfc search <text>`: find plans.\n" +
	"  - `bfc ask <id> --to <email|ai> [--ref plan.md#AC-n] -m <text>`: ask a Question.\n" +
	"  - `bfc reply <message-ULID> -m <text>`: answer a Question; add `--verbatim` when you record the user's own words.\n" +
	"  - `bfc start <id>`: only when the user asks to work on a plan; it switches the branch, which makes the plan the Current plan.\n" +
	"  - `bfc ready [id] --json` and `bfc claim [id] [T-n] --json`: list the open Tasks of approved plans, and take one before you work on it; the first Claim wins, so when it fails, take another.\n" +
	"- Gates are for people: `submit`, `approve`, `request-changes`, `withdraw` and `close` refuse to run inside an agent, and so does `acknowledge`. " +
	"When a plan needs one, tell the user to open `bfc` and take it there.\n" +
	"- Edit code only for an `approved` or `in-progress` Current plan, after you take a Task with `bfc claim`: a hook blocks file edits in every other state.\n" +
	"- Implement `plan.md` as approved. When the work must depart from it, record the deviation with " +
	"`bfc ask <id> --to <plan author email> --ref plan.md#AC-n -m <what and why>` and tell the user.\n"

// primeLimit caps the output of bfc prime; a longer plan.md is cut.
const primeLimit = 8 << 10

// primeListLimit caps each list of bfc prime, so that the lists alone stay
// well within primeLimit.
const primeListLimit = 10

// writeList writes the lines of the first primeListLimit items, then how
// many more there are and where to see them.
func writeList[T any](b *strings.Builder, items []T, line func(T) string, more string) {
	for i, it := range items {
		if i == primeListLimit {
			fmt.Fprintf(b, "- … and %d more: %s\n", len(items)-i, more)
			return
		}
		b.WriteString(line(it) + "\n")
	}
}

// primeView is what bfc prime prints, gathered by gatherPrime.
type primeView struct {
	// Setup says how to set BeforeCode up when the clone has no plans ref;
	// then nothing else is known.
	Setup    string
	Identity core.Person
	// Waiting is what waits on this user (core.WaitingOn).
	Waiting core.Waiting
	// Plan is the Current plan (Current) or the one given, with its open
	// Questions; nil when there is none.
	Plan    *index.Plan
	Current bool
	Open    []core.Message
	// Note says why the plan asked for could not be shown.
	Note string
	// Approved are the approved plans, listed when there is no plan.
	Approved []core.PlanRef
}

// renderPrime prints v as Markdown for an agent, at most primeLimit bytes:
// lists stop after primeListLimit items, and plan.md is cut to fit.
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
	_, err := io.WriteString(w, fitDoc(head.String(), doc, "", name, v.Plan.ShortID))
	return err
}

// fitDoc returns head, then doc in a Markdown fence, then tail, within
// primeLimit bytes. doc is cut at a line first, with a note naming it (name)
// and bfc show <short>; when head and tail alone do not fit, head is cut
// at a line too and doc is left out. tail is never cut.
func fitDoc(head string, doc []byte, tail, name, short string) string {
	body := string(doc)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	fence := fenceFor(body)
	opening, closing := fence+"markdown\n", fence+"\n"
	room := primeLimit - len(head) - len(opening) - len(closing) - len(tail)
	if len(body) <= room {
		return head + opening + body + closing + tail
	}
	cut := fmt.Sprintf("\n%s is cut here; run `bfc show %s` for all of it.\n", name, short)
	if room-len(cut) > 0 {
		return head + opening + cutAtLine(body, room-len(cut)) + closing + cut + tail
	}
	cut = fmt.Sprintf("\nThe rest is cut here; run `bfc show %s` for all of it.\n", short)
	return cutAtLine(head, primeLimit-len(tail)-len(cut)) + cut + tail
}

// cutAtLine returns the longest prefix of s of at most n bytes that ends
// at a line end.
func cutAtLine(s string, n int) string {
	if n >= len(s) {
		return s
	}
	return s[:strings.LastIndex(s[:max(n, 0)], "\n")+1]
}

// writeWaiting prints what waits on the user whose git identity bfc uses.
func writeWaiting(b *strings.Builder, v primeView) {
	fmt.Fprintf(b, "\n## Waiting on you\n\nFor %s <%s>, the git author of this clone, who runs you:\n\n", orDash(v.Identity.Name), orDash(v.Identity.Email))
	w := v.Waiting
	if w.Empty() {
		b.WriteString("Nothing waits on you.\n")
		return
	}
	writeList(b, w.Findings, func(f core.WaitingFinding) string {
		return fmt.Sprintf("- Finding in %s %s, pull request #%d: %s (check %s).", f.ShortID, f.Title, f.Finding.PR, f.Finding.Text(), f.Finding.Check)
	}, "`bfc inbox --json`")
	if len(w.Findings) > 0 {
		b.WriteString("\nA Finding is where a pull request departs from its plan. Tell the user: they fix the code, update the plan, or accept it on purpose with `bfc acknowledge <check> -m <reason>`, which is for people.\n\n")
	}
	writeList(b, w.Reviews, func(r core.PlanRef) string {
		return fmt.Sprintf("- Review %s %s: tell the user to open `bfc`, select it and press `a` to approve.", r.ShortID, r.Title)
	}, "`bfc list --state in-review --json`")
	writeList(b, w.Questions, func(q core.WaitingQuestion) string {
		return fmt.Sprintf("- Question %s in %s from %s%s: %s", q.Message.ID, q.ShortID, q.Message.Author.Email, refSuffix(q.Message.Ref), firstLine(q.Message.Body))
	}, "`bfc show <id> --json` lists the open Questions of a plan")
	if len(w.Questions) > 0 {
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
		writeList(b, v.Approved, func(p core.PlanRef) string {
			return fmt.Sprintf("- %s %s", p.ShortID, p.Title)
		}, "`bfc list --state approved --json`")
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
		writeList(b, v.Open, func(q core.Message) string {
			return fmt.Sprintf("- %s to %s%s: %s", q.ID, strings.Join(q.To, ", "), refSuffix(q.Ref), firstLine(q.Body))
		}, fmt.Sprintf("`bfc show %s --json`", p.ShortID))
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
