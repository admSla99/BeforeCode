package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// twoTasksPlan is validPlan with two Tasks.
var twoTasksPlan = strings.Replace(validPlan, "- T-1: Add the client.\n", "- T-1: Add the client.\n- T-2: Wire the flag,\n  behind the setting.\n", 1)

// approvedTwoTasks creates a plan by Ada with two Tasks in dir, approved by
// Jana, and leaves Ada as the git author.
func approvedTwoTasks(t *testing.T, dir, title string) (short string, id ulid.ULID) {
	t.Helper()
	short, id = newPlan(t, dir, title)
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(twoTasksPlan)})
	mustBfc(t, dir, "submit", short)
	as(t, dir, janaP)
	mustBfc(t, dir, "approve", short)
	as(t, dir, adaP)
	return short, id
}

// claimEvents returns the claimed Events of a plan.
func claimEvents(t *testing.T, dir string, id ulid.ULID) []core.Event {
	t.Helper()
	var out []core.Event
	for _, e := range events(t, dir, id) {
		if e.Type == core.Claimed {
			out = append(out, e)
		}
	}
	return out
}

// Issue #108 AC-1, AC-3, AC-5: bfc claim writes the claimed Event of the
// Task, also from an agent, and the plan is in-progress.
func TestClaimTakesATask(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := approvedTwoTasks(t, dir, "Payments")
	setEnv(t, agentEnv, "1")

	out := mustBfc(t, dir, "claim", short, "T-2")

	if !strings.Contains(out, "claimed  T-2 of "+short+": Wire the flag,") {
		t.Errorf("output:\n%s", out)
	}
	cl := claimEvents(t, dir, id)
	if len(cl) != 1 || cl[0].Task != "T-2" || cl[0].Agent != "claude-code" || cl[0].Author.Email != adaP.Email {
		t.Fatalf("claimed events = %+v", cl)
	}
	if got := state(t, dir, short); got != "in-progress" {
		t.Errorf("state = %s", got)
	}
}

// Issue #108 AC-5: without a Task the first open one of the plan, without
// a plan the Current plan; --json prints the Claim.
func TestClaimTakesTheNextTaskOfTheCurrentPlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := approvedTwoTasks(t, dir, "Payments")
	mustBfc(t, dir, "claim", short, "T-1")
	mustBfc(t, dir, "use", short)

	var got struct {
		Plan    string      `json:"plan"`
		ShortID string      `json:"short_id"`
		Task    string      `json:"task"`
		Text    string      `json:"text"`
		Holder  core.Person `json:"holder"`
		Event   string      `json:"event"`
		State   string      `json:"state"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "claim", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.Plan != id.String() || got.ShortID != short || got.Task != "T-2" || got.Text != "Wire the flag,\nbehind the setting." ||
		got.Holder.Email != adaP.Email || got.State != "in-progress" || len(got.Event) != 26 {
		t.Errorf("claim --json = %+v", got)
	}
	if out, err := bfcIn(t, dir, "claim"); err == nil || !strings.Contains(out, "no open Task") {
		t.Errorf("all Tasks claimed: %v\n%s", err, out)
	}
}

// Issue #108 AC-1: a Claim is refused in review, for an unknown Task and
// for a held one, and nothing is written.
func TestClaimRefusals(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := approvedTwoTasks(t, dir, "Payments")
	mustBfc(t, dir, "claim", short, "T-1")
	draft, _ := newPlan(t, dir, "Draft")
	before := planCommits(t, dir)

	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"held":         {[]string{short, "T-1"}, "T-1 of " + short + " is held by ada@example.com"},
		"no such Task": {[]string{short, "T-7"}, "T-7 is not a Task"},
		"draft":        {[]string{draft, "T-1"}, "only an approved or in-progress plan"},
		"not a Task":   {[]string{short, "AC-1"}, "AC-1"},
	} {
		out, err := bfcIn(t, dir, append([]string{"claim"}, c.args...)...)
		if err == nil || !strings.Contains(out, c.want) {
			t.Errorf("%s: err %v, output lacks %q:\n%s", name, err, c.want, out)
		}
	}
	if planCommits(t, dir) != before || len(claimEvents(t, dir, id)) != 1 {
		t.Errorf("a refused claim wrote")
	}
}

// Issue #108 AC-4: bfc ready lists the open Tasks of approved and
// in-progress plans in plan order; with an ID only that plan's.
func TestReadyListsTheOpenTasks(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	first, _ := approvedTwoTasks(t, dir, "Payments")
	second, _ := approvedTwoTasks(t, dir, "Refunds")
	mustBfc(t, dir, "claim", first, "T-1")
	newPlan(t, dir, "Draft") // not approved: no Tasks to take

	out := mustBfc(t, dir, "ready")
	for _, want := range []string{
		first + "  T-2  Wire the flag,  (Payments)\n",
		second + "  T-1  Add the client.  (Refunds)\n",
		second + "  T-2  Wire the flag,  (Refunds)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("bfc ready lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, first+"  T-1") {
		t.Errorf("a claimed Task is listed:\n%s", out)
	}
	if strings.Index(out, second+"  T-1") > strings.Index(out, second+"  T-2") {
		t.Errorf("not in plan order:\n%s", out)
	}

	var got []struct {
		ShortID string `json:"short_id"`
		Task    string `json:"task"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "ready", first, "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ShortID != first || got[0].Task != "T-2" {
		t.Errorf("ready %s --json = %+v", first, got)
	}
	mustBfc(t, dir, "claim", first, "T-2")
	if out := mustBfc(t, dir, "ready", first, "--json"); strings.TrimSpace(out) != "[]" {
		t.Errorf("no open Task: %s", out)
	}
}

// Issue #108 AC-6: list --json and show --json carry each Task's state and
// holder.
func TestListAndShowJSONCarryTheTasks(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := approvedTwoTasks(t, dir, "Payments")
	mustBfc(t, dir, "claim", short, "T-1")

	type task struct {
		ID     string       `json:"id"`
		Text   string       `json:"text"`
		State  string       `json:"state"`
		Holder *core.Person `json:"holder"`
	}
	var show struct {
		Tasks []task `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "show", short, "--json")), &show); err != nil {
		t.Fatal(err)
	}
	var list []struct {
		ShortID string `json:"short_id"`
		State   string `json:"state"`
		Tasks   []task `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "list", "--json")), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].State != "in-progress" {
		t.Fatalf("list = %+v", list)
	}
	for name, tasks := range map[string][]task{"show": show.Tasks, "list": list[0].Tasks} {
		if len(tasks) != 2 || tasks[0].State != "claimed" || tasks[0].Holder == nil || tasks[0].Holder.Email != adaP.Email ||
			tasks[1].State != "open" || tasks[1].Holder != nil {
			t.Errorf("%s tasks = %+v", name, tasks)
		}
	}
}

// Issue #108 AC-5, AC-7: claims on one Task from two clones end with the
// same holder in both after sync: the earlier ULID wins. bfc claim pulls
// first, so it refuses a Task the remote already gave away.
func TestClaimsFromTwoClonesAgreeAfterSync(t *testing.T) {
	t.Parallel()
	ana, ben, _ := twoClones(t)
	short, id := approvedTwoTasks(t, ana, "Payments")
	mustBfc(t, ana, "push")
	mustBfc(t, ben, "pull")

	// Ben claims T-1 first, in his clone only, as if his push were slow.
	addEvent(t, ben, id, core.Event{Type: core.Claimed, Author: benP, Task: "T-1"})
	// Ana's claim pulls, writes a later Claim and pushes.
	mustBfc(t, ana, "claim", short, "T-1")
	// Ben's sync brings Ana's Claim; his earlier one wins in his clone…
	mustBfc(t, ben, "sync")
	// …and, once Ana syncs, in hers.
	mustBfc(t, ana, "sync")

	for name, dir := range map[string]string{"ana": ana, "ben": ben} {
		var got struct {
			State string `json:"state"`
			Tasks []struct {
				ID     string       `json:"id"`
				Holder *core.Person `json:"holder"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal([]byte(mustBfc(t, dir, "show", short, "--json")), &got); err != nil {
			t.Fatal(err)
		}
		if got.State != "in-progress" || got.Tasks[0].Holder == nil || got.Tasks[0].Holder.Email != benP.Email {
			t.Errorf("%s sees %+v", name, got)
		}
	}
	// The later Claim is skipped, and bfc doctor lists it.
	out, _ := bfcIn(t, ana, "doctor")
	wantDoctor(t, out, "warn", "skipped events", "1 not allowed or out of order")
	if !strings.Contains(out, "(claimed by ada@example.com)") {
		t.Errorf("doctor does not list Ana's Claim:\n%s", out)
	}
	// A third claim on T-1 pulls first and is refused.
	if out, err := bfcIn(t, ana, "claim", short, "T-1"); err == nil || !strings.Contains(out, "held by ben@example.com") {
		t.Errorf("claim of a held Task: %v\n%s", err, out)
	}
}

// Issue #108 AC-5: a Claim that loses to an earlier one says who holds the
// Task.
func TestClaimOutcome(t *testing.T) {
	t.Parallel()
	ours := ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4F")
	theirs := ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4E")
	st := core.Status{ShortID: "bfc-5v7x", Tasks: map[string]core.TaskStatus{"T-1": {State: core.TaskClaimed, Claim: core.Claim{Event: theirs, Holder: benP}}}}
	err := claimOutcome(st, "T-1", ours)
	if err == nil || !strings.Contains(err.Error(), "T-1 of bfc-5v7x went to ben@example.com, whose Claim came first") {
		t.Errorf("err = %v", err)
	}
	st.Tasks["T-1"] = core.TaskStatus{State: core.TaskClaimed, Claim: core.Claim{Event: ours, Holder: adaP}}
	if err := claimOutcome(st, "T-1", ours); err != nil {
		t.Errorf("our Claim won: %v", err)
	}
}
