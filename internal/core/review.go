package core

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// The review gate and the end of a plan (ADR 0002 section 4): who may write
// submitted, withdrawn, approved, changes-requested and closed, and in which
// Plan state. These checks run when bfc
// writes an event; Derive applies the same rules when it reads them.

// stateError names the current state of a plan that a command does not fit.
func stateError(st Status, what string) error {
	return fmt.Errorf("%s is %s; %s", st.ShortID, st.State, what)
}

// inReviewRound reports whether the plan has an open review round, which
// changes-requested and withdrawn end.
func inReviewRound(st Status) bool {
	return st.State == StateInReview || st.State == StateApproved
}

func samePerson(a, b Person) bool {
	return strings.EqualFold(a.Email, b.Email)
}

// CheckSubmit reports why actor cannot submit the plan with Status st whose
// plan.md is plan (nil when there is none), or nil if they can.
func CheckSubmit(st Status, actor Person, plan []byte) error {
	if st.State != StateDraft {
		return stateError(st, "only a draft can be submitted")
	}
	if !samePerson(actor, st.Author) {
		return fmt.Errorf("only the plan author (%s) can submit %s", st.Author.Email, st.ShortID)
	}
	if plan == nil {
		return fmt.Errorf("%s has no plan.md yet; write it with bfc edit %s", st.ShortID, st.ShortID)
	}
	if problems := ValidatePlan(plan); len(problems) > 0 {
		msgs := make([]string, len(problems))
		for i, p := range problems {
			msgs[i] = "  plan.md " + p.String()
		}
		return fmt.Errorf("%s cannot be submitted; fix plan.md with bfc edit %s:\n%s", st.ShortID, st.ShortID, strings.Join(msgs, "\n"))
	}
	return nil
}

// CheckApprove reports why actor cannot approve the current plan.md of the
// plan with Status st, or nil if they can.
func CheckApprove(st Status, actor Person) error {
	switch st.State {
	case StateInReview:
	case StateApproved:
		return stateError(st, "it has all the Approvals it needs")
	default:
		return stateError(st, "only a plan in review can be approved")
	}
	if samePerson(actor, st.Author) {
		return fmt.Errorf("%s is your own plan; someone other than the plan author must approve it", st.ShortID)
	}
	for _, email := range st.Approvers {
		if strings.EqualFold(email, actor.Email) {
			return fmt.Errorf("you already approved this version of %s", st.ShortID)
		}
	}
	return nil
}

// CheckRequestChanges reports why actor cannot request changes to the plan
// with Status st for the given reason, or nil if they can.
func CheckRequestChanges(st Status, actor Person, reason string) error {
	if !inReviewRound(st) {
		return stateError(st, "changes can be requested only for a plan in review or approved")
	}
	if samePerson(actor, st.Author) {
		return fmt.Errorf("%s is your own plan; to take it back for changes use bfc withdraw", st.ShortID)
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("give the reason for the changes")
	}
	return nil
}

// CheckWithdraw reports why actor cannot withdraw the plan with Status st
// from review, or nil if they can.
func CheckWithdraw(st Status, actor Person) error {
	if !inReviewRound(st) {
		return stateError(st, "only a plan in review or approved can be withdrawn")
	}
	if !samePerson(actor, st.Author) {
		return fmt.Errorf("only the plan author (%s) can withdraw %s; to send it back use bfc request-changes", st.Author.Email, st.ShortID)
	}
	return nil
}

// CheckClose reports why actor cannot close the plan with Status st, or nil
// if they can. Anyone may close a plan in any state (ADR 0002 section 4);
// closed is final.
func CheckClose(st Status, _ Person) error {
	switch st.State {
	case StateClosed:
		return stateError(st, "closed is final")
	case StateInvalid:
		return stateError(st, "it has no valid created event to close; see bfc doctor")
	}
	return nil
}

// Message is one post in the discussion of a Plan, stored as
// plans/<plan ULID>/messages/<message ULID>.md (ADR 0002 section 7). Only
// the fields bfc writes so far are here.
type Message struct {
	Author Person
	At     time.Time
	Body   string
}

type messageFile struct {
	Author Person    `yaml:"author"`
	At     time.Time `yaml:"at"`
}

// EncodeMessage returns the file content of m, with LF line endings.
func EncodeMessage(m Message) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(messageFile{Author: m.Author, At: m.At.UTC().Truncate(time.Second)}); err != nil {
		panic(err) // plain strings always encode
	}
	body := strings.TrimSpace(strings.ReplaceAll(m.Body, "\r\n", "\n"))
	b.WriteString("---\n\n" + body + "\n")
	return b.Bytes()
}
