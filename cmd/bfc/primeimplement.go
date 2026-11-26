package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	beforecode "github.com/admSla99/BeforeCode"
	"github.com/admSla99/BeforeCode/internal/core"
)

// implementSkill is the skill bfc prime implement hands the agent.
const implementSkill = beforecode.SkillsDir + "/implement-task/SKILL.md"

// implementView is what bfc prime implement prints: the topic, the git
// author of this clone and the Task asked for.
type implementView struct {
	topicView
	Me core.Person
	// Task is the Task named on the command line, "" for the one bfc
	// chooses (chooseTask).
	Task string
	// Handoffs are the plan's Handoffs that have their Event, oldest first.
	Handoffs []core.Handoff
	// Unread says why the Handoffs cannot be read.
	Unread string
}

// runPrimeImplement is bfc prime implement [id] [T-n]: the implement-task
// skill, then the Task to work on with its acceptance criteria, plan.md and
// its Handoffs. Like bfc prime it reads only this clone, claims nothing,
// never fails over plans and stays within primeLimit.
func runPrimeImplement(cmd *cobra.Command, args []string) error {
	if len(args) > 2 {
		return errors.New("bfc prime implement takes at most a plan ID and a Task ID")
	}
	query, task, err := claimArgs(args)
	if err != nil {
		return err
	}
	var planArgs []string
	if query != "" {
		planArgs = []string{query}
	}
	v := implementView{topicView: gatherTopic(cmd, planArgs, implementSkill), Task: task}
	if v.Plan == nil {
		return renderImplementTopic(cmd.OutOrStdout(), v)
	}
	s, setup := openPrime(cmd)
	if setup != "" {
		v.Unread = setup
		return renderImplementTopic(cmd.OutOrStdout(), v)
	}
	ctx := cmd.Context()
	v.Me, _ = currentUser(ctx, s)
	hs, events, err := planHandoffs(ctx, s, *v.Plan)
	if err != nil {
		v.Unread = err.Error()
	}
	for _, h := range hs {
		if events[h.ID] != "" {
			v.Handoffs = append(v.Handoffs, h)
		}
	}
	return renderImplementTopic(cmd.OutOrStdout(), v)
}

// chooseTask returns the Task to work on: the one named (task), else the
// one me holds, else the one bfc claim without a Task would take. why says
// which rule chose it; ok is false when there is none.
func chooseTask(tasks []core.TaskView, me core.Person, task string) (t core.TaskView, why string, ok bool) {
	if task != "" {
		for _, t := range tasks {
			if t.ID == task {
				return t, "you named it", true
			}
		}
		return core.TaskView{}, "", false
	}
	for _, t := range tasks {
		if t.State == core.TaskClaimed && me.Email != "" && strings.EqualFold(t.Holder.Email, me.Email) {
			return t, "you hold it", true
		}
	}
	for _, want := range []core.TaskState{core.TaskOpen, core.TaskPaused} {
		for _, t := range tasks {
			if t.State == want {
				return t, "you hold no Task of this plan, so it is the next one `bfc claim` would take (first open, then first Paused)", true
			}
		}
	}
	return core.TaskView{}, "", false
}

// renderImplementTopic prints v as Markdown for an agent, at most
// primeLimit bytes (see fitDoc): plan.md is cut to fit, and the Task's
// Handoffs always end the output.
func renderImplementTopic(w io.Writer, v implementView) error {
	var b strings.Builder
	if v.Plan == nil && v.Note == "There is no Current plan." {
		v.Note += " Run `bfc start <id>` only when the user asks to work on that plan; it switches the branch."
	}
	if !writeTopicHead(&b, v.topicView, "implement") {
		_, err := io.WriteString(w, b.String())
		return err
	}
	p := v.Plan
	fmt.Fprintf(&b, "- Author: %s <%s>\n", orDash(p.Status.Author.Name), orDash(p.Status.Author.Email))
	note := func(text string) error {
		b.WriteString("\n" + text + "\n")
		_, err := io.WriteString(w, b.String())
		return err
	}
	if !core.HasWork(p.Status.State) {
		return note(fmt.Sprintf("%s is %s, not approved or in-progress: do not implement it. It needs a Gate, which the user takes in the `bfc` TUI.", p.ShortID, p.Status.State))
	}
	tasks := core.TaskViews(p.Status, p.Plan)
	if len(tasks) == 0 {
		return note(fmt.Sprintf("%s has no Tasks in plan.md, so there is nothing to claim. Tell the user; adding Tasks is the author's change to plan.md.", p.ShortID))
	}
	done := true
	for _, t := range tasks {
		done = done && t.State == core.TaskCompleted
	}
	if done {
		return note(fmt.Sprintf("Every Task of %s is completed. Follow \"After the last Task\" in the skill above: tell the user and offer the pull request.", p.ShortID))
	}
	t, why, ok := chooseTask(tasks, v.Me, v.Task)
	if !ok && v.Task != "" {
		var list strings.Builder
		for _, t := range tasks {
			fmt.Fprintf(&list, "- %s: %s\n", t.ID, firstLine(t.Text))
		}
		return note(fmt.Sprintf("%s is not a Task of %s. Its Tasks:\n\n%s", v.Task, p.ShortID, strings.TrimSuffix(list.String(), "\n")))
	}
	if !ok {
		return note(fmt.Sprintf("Every Task of %s that is not completed is held by someone else; `bfc ready %s` lists the free ones.", p.ShortID, p.ShortID))
	}
	writeImplementTask(&b, p.ShortID, t, why)
	fmt.Fprintf(&b, "\n### Commands for %s\n\n", t.ID)
	fmt.Fprintf(&b, "- Claim it: `bfc claim %s %s --json`\n", p.ShortID, t.ID)
	fmt.Fprintf(&b, "- Hand it off: `bfc handoff %s %s --done -m <note>` when it is finished, without `--done` when it is not; then `bfc push`\n", p.ShortID, t.ID)
	fmt.Fprintf(&b, "- Ask the author: `bfc ask %s --to %s --ref plan.md#AC-n -m <question>`\n", p.ShortID, authorAddress(p))
	b.WriteString("\n### Acceptance criteria\n\n")
	criteria := core.Criteria(p.Plan)
	if len(criteria) == 0 {
		b.WriteString("plan.md has no well-formed acceptance criteria.\n")
	}
	for _, c := range criteria {
		fmt.Fprintf(&b, "- %s: %s\n", c.ID, strings.ReplaceAll(c.Text, "\n", "\n  "))
	}
	b.WriteString("\n### plan.md\n\n")
	_, err := io.WriteString(w, fitDoc(b.String(), p.Plan, taskHandoffs(v, t.ID), "plan.md", p.ShortID))
	return err
}

// writeImplementTask writes the chosen Task: its text, state, holder and
// why it was chosen.
func writeImplementTask(b *strings.Builder, short string, t core.TaskView, why string) {
	fmt.Fprintf(b, "\n## Task %s\n\n", t.ID)
	fmt.Fprintf(b, "- Chosen because %s.\n", why)
	switch t.State {
	case core.TaskOpen:
		b.WriteString("- State: open\n")
	case core.TaskClaimed:
		fmt.Fprintf(b, "- State: claimed by %s\n", t.Holder.Email)
	default:
		fmt.Fprintf(b, "- State: %s, last held by %s\n", t.State, t.Holder.Email)
	}
	if t.State == core.TaskCompleted {
		fmt.Fprintf(b, "- It is completed; `bfc ready %s` lists the free Tasks.\n", short)
	}
	b.WriteString("\n" + t.Text + "\n")
}

// taskHandoffs returns the section with every Handoff of task in full,
// oldest first.
func taskHandoffs(v implementView, task string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n### Handoffs of %s\n\n", task)
	if v.Unread != "" {
		b.WriteString("The Handoffs cannot be read: " + v.Unread + "\n")
	}
	n := 0
	for _, h := range v.Handoffs {
		if h.Task != task {
			continue
		}
		n++
		what := "released"
		if h.Done {
			what = "completed"
		}
		fmt.Fprintf(&b, "#### Handoff %s, %s by %s <%s>, %s\n\n", h.ID, what, orDash(h.Author.Name), orDash(h.Author.Email), h.At.UTC().Format("2006-01-02 15:04"))
		b.WriteString(strings.TrimRight(h.Note, "\n") + "\n\n")
	}
	if n == 0 {
		b.WriteString("None yet: the Task starts from plan.md.\n")
	}
	return b.String()
}
