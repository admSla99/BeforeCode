package core

import "fmt"

// TaskState is the state of a Task (ADR 0013 section 4). Paused and
// completed come with Handoffs (#109).
type TaskState string

// Task states.
const (
	TaskOpen    TaskState = "open"
	TaskClaimed TaskState = "claimed"
)

// TaskView is a Task of plan.md with its state and, while claimed, its
// holder.
type TaskView struct {
	Task
	State  TaskState
	Holder Person // set while claimed
	Agent  string // the harness, when an agent holds it
}

// TaskViews returns the Tasks of plan.md in its order, each with its state
// from st. A Claim on a Task that plan.md no longer has is left out.
func TaskViews(st Status, plan []byte) []TaskView {
	var out []TaskView
	for _, t := range Tasks(plan) {
		v := TaskView{Task: t, State: TaskOpen}
		if c, ok := st.Claims[t.ID]; ok {
			v.State, v.Holder, v.Agent = TaskClaimed, c.Holder, c.Agent
		}
		out = append(out, v)
	}
	return out
}

// NextTask returns the first open Task of plan.md: Tasks have no
// dependencies, so the order of the list is the order of the work.
func NextTask(st Status, plan []byte) (TaskView, bool) {
	for _, v := range TaskViews(st, plan) {
		if v.State == TaskOpen {
			return v, true
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
// in-progress, the Task must be in plan.md, and nobody may hold it.
func CheckClaim(st Status, plan []byte, task string) error {
	if err := CheckHasWork(st); err != nil {
		return err
	}
	for _, v := range TaskViews(st, plan) {
		if v.ID != task {
			continue
		}
		if v.State != TaskOpen {
			return fmt.Errorf("%s of %s is held by %s; bfc ready lists the open Tasks", task, st.ShortID, v.Holder.Email)
		}
		return nil
	}
	return fmt.Errorf("%s is not a Task of plan.md of %s; bfc show %s lists its Tasks", task, st.ShortID, st.ShortID)
}
