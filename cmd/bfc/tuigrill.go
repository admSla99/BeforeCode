package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/agent"
	"github.com/admSla99/BeforeCode/internal/agent/claude"
	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
	"github.com/admSla99/BeforeCode/internal/tui"
)

// tuiGrill is the Grill adapter of the TUI (ADR 0012): it reads the Grill
// from the Plans ref, runs Claude Code headless through the agent adapter
// and stores the author's answers with the code of bfc reply --verbatim.
type tuiGrill struct {
	cmd *cobra.Command
}

// Snapshot returns the plan's Grill: the Questions from the AI to the
// author with the author's answers, and every other Message.
func (g *tuiGrill) Snapshot(ctx context.Context, shortID string) (tui.GrillSnapshot, error) {
	s, err := openPlans(g.cmd, forReading)
	if err != nil {
		return tui.GrillSnapshot{}, err
	}
	v, err := resolvePlan(ctx, s, shortID)
	if err != nil {
		return tui.GrillSnapshot{}, err
	}
	msgs, err := readMessages(ctx, s, v.ID)
	if err != nil {
		return tui.GrillSnapshot{}, err
	}
	tpl, err := readTemplate(s.Root(), planTemplate)
	if err != nil {
		return tui.GrillSnapshot{}, err
	}
	snap := tui.GrillSnapshot{
		ShortID: v.ShortID, Title: v.Title, State: v.Status.State,
		Author: v.Status.Author.Name, AuthorEmail: v.Status.Author.Email,
		PlanWritten: v.Plan != nil && !sameText(v.Plan, tpl),
	}
	open := core.OpenQuestions(msgs)
	grill := map[string]bool{} // the Grill's Questions and their replies
	for _, m := range msgs {
		if !isGrillQuestion(m, v.Status.Author.Email) {
			continue
		}
		id := m.ID.String()
		grill[id] = true
		q := tui.GrillQuestion{ID: id, Text: m.Body, Open: slices.ContainsFunc(open, func(o core.Message) bool { return o.ID == m.ID })}
		for _, r := range msgs {
			if r.ReplyTo == id && r.Agent == "" && strings.EqualFold(r.Author.Email, v.Status.Author.Email) {
				q.Answer = r.Body
				break
			}
		}
		snap.Questions = append(snap.Questions, q)
	}
	for _, m := range msgs {
		answer := grill[m.ReplyTo] && m.Agent == "" && strings.EqualFold(m.Author.Email, v.Status.Author.Email)
		if grill[m.ID.String()] || answer {
			continue
		}
		who := m.Author.Name
		if who == "" {
			who = m.Author.Email
		}
		if m.Agent != "" {
			who += " (AI)"
		}
		snap.Others = append(snap.Others, tui.GrillMessage{Who: who, Text: m.Body})
	}
	return snap, nil
}

// isGrillQuestion reports whether m is a Question of the Grill: asked by an
// agent and addressed to the plan's author (ADR 0002 section 7).
func isGrillQuestion(m core.Message, author string) bool {
	return m.Agent != "" && slices.ContainsFunc(m.To, func(to string) bool { return strings.EqualFold(to, author) })
}

// Answer stores the author's answer like bfc reply --verbatim: the person's
// own words, without the agent.
func (g *tuiGrill) Answer(ctx context.Context, shortID, questionID, text string) error {
	if _, err := ulid.ParseStrict(questionID); err != nil {
		return fmt.Errorf("not the ULID of a Question: %q: %w", questionID, err)
	}
	s, err := openPlans(g.cmd, forWriting)
	if err != nil {
		return err
	}
	v, err := resolvePlan(ctx, s, shortID)
	if err != nil {
		return err
	}
	_, err = writeMessage(g.cmd, s, v, "reply", core.Message{ReplyTo: questionID, Body: text}, true)
	return err
}

// Tools a Grill turn may use: read the plan and record its Question. The
// TUI records the answer itself (ADR 0012).
var grillTools = []string{"Bash(bfc prime:*)", "Bash(bfc show:*)", "Bash(bfc ask:*)"}

// Tools the write-plan turn may use.
var writePlanTools = []string{"Bash(bfc prime:*)", "Bash(bfc show:*)", "Bash(bfc edit:*)"}

// Turn runs one turn of Claude Code for the Grill of the plan. It resumes
// the session kept in the clone's git directory and keeps the new one; when
// resuming fails before any output, it starts a new session from
// bfc prime grill, which holds the whole Grill so far.
func (g *tuiGrill) Turn(ctx context.Context, shortID string, t tui.GrillTurn) (<-chan tui.GrillEvent, error) {
	s, err := openPlans(g.cmd, forReading)
	if err != nil {
		return nil, err
	}
	v, err := resolvePlan(ctx, s, shortID)
	if err != nil {
		return nil, err
	}
	wd, err := workDir(g.cmd)
	if err != nil {
		return nil, err
	}
	loc, err := store.Locate(ctx, wd, extraEnv(g.cmd)...)
	if err != nil {
		return nil, err
	}
	command := "claude"
	if cfg, found, err := readConfig(s); err != nil {
		return nil, err
	} else if found {
		command = cfg.Command()
	}
	r := grillRun{
		agent:   claude.New(claude.Options{Command: command, Env: extraEnv(g.cmd)}),
		plan:    v,
		turn:    t,
		dir:     loc.Root,
		session: filepath.Join(loc.GitDir, "beforecode", "grill", v.ID.String()+".session"),
		planBlob: func() string {
			// The version of plan.md now; a write-plan turn compares it at the end.
			s, err := openStore(g.cmd)
			if err != nil {
				return ""
			}
			id, _ := s.ObjectID(context.Background(), core.PlanDir(v.ID)+"/plan.md")
			return id
		},
	}
	out := make(chan tui.GrillEvent)
	go r.run(ctx, out)
	return out, nil
}

// grillRun is one Grill turn in progress.
type grillRun struct {
	agent   agent.Agent
	plan    index.Plan
	turn    tui.GrillTurn
	dir     string
	session string // the file that keeps the session id
	// planBlob returns the object of plan.md in the Plans ref, "" when
	// there is none.
	planBlob func() string
}

func (r grillRun) run(ctx context.Context, out chan<- tui.GrillEvent) {
	defer close(out)
	send := func(e tui.GrillEvent) bool {
		select {
		case out <- e:
			return true
		case <-ctx.Done():
			return false
		}
	}
	data, _ := os.ReadFile(r.session)
	session := strings.TrimSpace(string(data))
	before := ""
	if r.turn.Kind == tui.TurnWritePlan {
		before = r.planBlob()
	}
	resumeFailed, err := r.attempt(ctx, session, send)
	if resumeFailed {
		_ = os.Remove(r.session)
		_, err = r.attempt(ctx, "", send)
	}
	if r.turn.Kind == tui.TurnWritePlan {
		// The Grill is over only once plan.md was stored, whatever the
		// agent says.
		if after := r.planBlob(); after != "" && after != before {
			_ = os.Remove(r.session)
			if !send(tui.GrillEvent{PlanSaved: true}) {
				return
			}
		}
	}
	if err != nil {
		send(tui.GrillEvent{Err: err})
	}
}

// attempt runs the turn once, resuming session when it is set. It reports
// resumeFailed when resuming failed before any text, so that the caller
// starts a new session.
func (r grillRun) attempt(ctx context.Context, session string, send func(tui.GrillEvent) bool) (resumeFailed bool, err error) {
	tools := grillTools
	if r.turn.Kind == tui.TurnWritePlan {
		tools = writePlanTools
	}
	events, err := r.agent.Run(ctx, agent.Task{Prompt: r.prompt(session != ""), Session: session, AllowedTools: tools, Dir: r.dir})
	if err != nil {
		return false, err
	}
	// On an early return the agent's events are drained, so that it never
	// blocks on a send.
	defer func() {
		go func() {
			for range events {
			}
		}()
	}()
	wrote, acted := false, false
	for e := range events {
		switch e.Kind {
		case agent.Text:
			text := e.Text
			if wrote {
				text = "\n\n" + text
			}
			wrote = true
			if !send(tui.GrillEvent{Text: text}) {
				return false, ctx.Err()
			}
		case agent.ToolUse:
			acted = true
			if cmdline := toolCommand(e); cmdline != "" && !send(tui.GrillEvent{Tool: cmdline}) {
				return false, ctx.Err()
			}
		case agent.Result:
			if err := r.keep(e.Session); err != nil {
				return false, err
			}
		case agent.Error:
			// Only a resume that failed before the agent did anything
			// starts a new session; otherwise a Question could be asked
			// twice.
			if session != "" && !wrote && !acted && ctx.Err() == nil {
				return true, nil
			}
			return false, errors.New(firstLine(e.Text))
		}
	}
	return false, nil
}

// keep saves the session id for the next turn.
func (r grillRun) keep(session string) error {
	if session == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.session), 0o755); err != nil {
		return err
	}
	return os.WriteFile(r.session, []byte(session+"\n"), 0o600)
}

// prompt is what the agent is told in this turn; resumed says whether it
// continues its session.
func (r grillRun) prompt(resumed bool) string {
	id := r.plan.ShortID
	rules := "Ask exactly one Question in this turn: record it first with `bfc ask " + id + " --to " + r.plan.Status.Author.Email +
		" --ref intent.md -m \"<question>\"`, then end your turn with the same Question as your reply. " +
		"The bfc TUI shows it to the author and records their answer itself, so do not run `bfc reply`. " +
		"When nothing is left to ask, record and ask whether the author wants to finish the Grill."
	switch r.turn.Kind {
	case tui.TurnSummary:
		p := "Finish the Grill of " + id + " now: write a short summary of what was decided and what is still open. Ask no new Question."
		if !resumed {
			p = "Read the Grill of plan " + id + " with `bfc prime grill " + id + "`. " + p
		}
		return p
	case tui.TurnWritePlan:
		p := "The Grill of " + id + " is finished. Write its plan.md now: run `bfc prime plan " + id + "` and follow it, without asking the author anything in this turn. " +
			"Check the document with `bfc edit " + id + " --stdin --check` until it has no problems, then store it once with `bfc edit " + id + " --stdin`."
		if r.turn.Force {
			p += " plan.md is already written and the author agreed to replace it, so store it with `bfc edit " + id + " --stdin --force`."
		}
		return p + " End with one line saying plan.md is saved."
	}
	switch {
	case !resumed && r.turn.Answer != "":
		return "You are grilling the Intent of plan " + id + " with its author in the bfc TUI. Run `bfc prime grill " + id + "` and follow it; it shows the Grill so far, including the author's latest answer. " + rules
	case !resumed:
		return "You are grilling the Intent of plan " + id + " with its author in the bfc TUI. Run `bfc prime grill " + id + "` and follow it. " + rules
	case r.turn.Answer != "":
		return "The author answered: " + r.turn.Answer + "\n\nThe answer is recorded on the plan. Ask your next Question. " + rules
	}
	return "Continue the Grill of " + id + ": ask your next Question. " + rules
}

// toolCommand returns the first line of the command of a Bash tool call,
// or the tool's name for any other tool.
func toolCommand(e agent.Event) string {
	var in struct {
		Command string `json:"command"`
	}
	if e.Tool == "Bash" && json.Unmarshal(e.Input, &in) == nil && in.Command != "" {
		return firstLine(in.Command)
	}
	return e.Tool
}
