package core

import "fmt"

// TaskState is the state of a Task (ADR 0013 section 4): open, claimed,
// Paused (released with a Handoff; anyone may claim it again) or completed.
type TaskState string

// Task states.
const (
	TaskOpen      TaskState = "open"
	TaskClaimed   TaskState = "claimed"
	TaskPaused    TaskState = "paused"
	TaskCompleted TaskState = "completed"
)

// Free reports whether a Task in state s may be claimed: open or Paused.
func (s TaskState) Free() bool { return s == TaskOpen || s == TaskPaused }

// TaskView is a Task of plan.md with its state, its holder (the last one
// once Paused or completed) and the ULID of its latest Handoff.
type TaskView struct {
	Task
	State   TaskState
	Holder  Person // the zero Person while open
	Agent   string // the harness, when an agent claimed it
	Handoff string // "" without a Handoff
}

// TaskViews returns the Tasks of plan.md in its order, each with its state
// from st. A Task that plan.md no longer has is left out.
func TaskViews(st Status, plan []byte) []TaskView {
	var out []TaskView
	for _, t := range Tasks(plan) {
		v := TaskView{Task: t, State: TaskOpen}
		if ts, ok := st.Tasks[t.ID]; ok {
			v.State, v.Holder, v.Agent, v.Handoff = ts.State, ts.Claim.Holder, ts.Claim.Agent, ts.Handoff
		}
		out = append(out, v)
	}
	return out
}

// NextTask returns the Task to take next: the first open Task of plan.md,
// else the first Paused one. Tasks have no dependencies, so the order of the
// list is the order of the work.
func NextTask(st Status, plan []byte) (TaskView, bool) {
	views := TaskViews(st, plan)
	for _, want := range []TaskState{TaskOpen, TaskPaused} {
		for _, v := range views {
			if v.State == want {
				return v, true
			}
		}
	}
	return TaskView{}, false
}

// HasWork reports whether a plan in state st is where Tasks are taken:
// approved or in-progress (ADR 0013 section 4).
func HasWork(st State) bool {
	return st == StateApproved || st == StateInProgress
}

// CheckHasWork reports why a plan with Status st has no Tasks to take now.
func CheckHasWork(st Status) error {
	if !HasWork(st.State) {
		return stateError(st, "only an approved or in-progress plan has Tasks to claim")
	}
	return nil
}

// CheckClaim reports why task of the plan with Status st and plan.md plan
// cannot be claimed now (ADR 0013 section 4): the plan must be approved or
// in-progress, the Task must be in plan.md, and open or Paused.
func CheckClaim(st Status, plan []byte, task string) error {
	if err := CheckHasWork(st); err != nil {
		return err
	}
	v, err := findTask(st, plan, task)
	switch {
	case err != nil:
		return err
	case v.State == TaskCompleted:
		return fmt.Errorf("%s of %s is completed; bfc ready lists the open Tasks", task, st.ShortID)
	case !v.State.Free():
		return fmt.Errorf("%s of %s is held by %s; bfc ready lists the open Tasks", task, st.ShortID, v.Holder.Email)
	}
	return nil
}

// CheckHandoff reports why actor cannot hand off task (ADR 0013 section
// 4): only its holder can, while it is claimed.
func CheckHandoff(st Status, plan []byte, task string, actor Person) error {
	v, err := findTask(st, plan, task)
	switch {
	case err != nil:
		return err
	case v.State != TaskClaimed:
		return fmt.Errorf("%s of %s is %s; only a claimed Task is handed off, by its holder", task, st.ShortID, v.State)
	case !samePerson(actor, v.Holder):
		return fmt.Errorf("%s of %s is held by %s <%s>; only the holder hands it off", task, st.ShortID, v.Holder.Name, v.Holder.Email)
	}
	return nil
}

// findTask returns the Task of plan.md with this ID and its state.
func findTask(st Status, plan []byte, task string) (TaskView, error) {
	for _, v := range TaskViews(st, plan) {
		if v.ID == task {
			return v, nil
		}
	}
	return TaskView{}, fmt.Errorf("%s is not a Task of plan.md of %s; bfc show %s lists its Tasks", task, st.ShortID, st.ShortID)
}
