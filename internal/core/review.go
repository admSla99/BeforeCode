package core

import (
	"errors"
	"fmt"
	"strings"
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
// changes-requested and withdrawn end. An in-progress plan is approved, so
// its round is open too.
func inReviewRound(st Status) bool {
	return st.State == StateInReview || st.State == StateApproved || st.State == StateInProgress
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
	if err := CheckWritable(st); err != nil {
		return err
	}
	switch st.State {
	case StateInvalid:
		return stateError(st, "it has no valid created event to close; see bfc doctor")
	}
	return nil
}

// CheckWritable reports why nothing may be written to the plan with Status
// st any more: closed and done are final.
func CheckWritable(st Status) error {
	switch st.State {
	case StateClosed:
		return stateError(st, "closed is final")
	case StateDone:
		return stateError(st, "done is final")
	}
	return nil
}

// CheckDone reports why the plan with Status st cannot be marked done now:
// only an in-progress plan is (ADR 0013 section 4). Whether a person runs
// it is CheckPerson's.
func CheckDone(st Status) error {
	if err := CheckWritable(st); err != nil {
		return err
	}
	if st.State != StateInProgress {
		return stateError(st, "only an in-progress plan is marked done")
	}
	return nil
}

// ErrGateForPeople is the refusal of a Gate run by an agent (ADR 0011).
var ErrGateForPeople = errors.New("this action is for a person, not an agent; ask the person to run it")

// CheckPerson reports why a Gate (submit, approve, request-changes,
// withdraw, close) cannot run, or nil if it can: the Gates are for people,
// so they refuse to run inside an agent. Which harness counts as an agent
// is for the caller to decide.
func CheckPerson(insideAgent bool) error {
	if insideAgent {
		return ErrGateForPeople
	}
	return nil
}
