package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// claimedPlan is an approved plan by Ada with two Tasks, T-1 claimed by Ada.
func claimedPlan(t *testing.T) (dir, short string, id ulid.ULID) {
	t.Helper()
	dir = newLocalRepo(t)
	short, id = approvedTwoTasks(t, dir, "Payments")
	mustBfc(t, dir, "claim", short, "T-1")
	return dir, short, id
}

// handoffOf reads the Handoff the newest released or completed Event of the
// plan names.
func handoffOf(t *testing.T, dir string, id ulid.ULID) (core.Event, core.Handoff) {
	t.Helper()
	e := last(t, dir, id)
	if e.Type != core.Released && e.Type != core.Completed {
		t.Fatalf("newest event = %+v", e)
	}
	hid := ulid.MustParseStrict(e.Handoff)
	h, err := core.ParseHandoff(hid, []byte(gitIn(t, dir, "show", plansRef+":"+core.HandoffPath(id, hid))))
	if err != nil {
		t.Fatal(err)
	}
	return e, h
}

// Issue #109 AC-1, AC-3: without --done bfc handoff writes the Handoff and
// its released Event in one commit; the Task is Paused, and anyone may
// claim it again.
func TestHandoffReleasesATask(t *testing.T) {
	t.Parallel()
	dir, short, id := claimedPlan(t)
	before := gitIn(t, dir, "rev-parse", plansRef)
	setEnv(t, agentEnv, "1")

	out := mustBfc(t, dir, "handoff", short, "T-1", "-m", "The client is in place.\n\nNext: the retries.")

	if !strings.Contains(out, "released T-1 of "+short) {
		t.Errorf("output:\n%s", out)
	}
	if parent := gitIn(t, dir, "rev-parse", plansRef+"^"); parent != before {
		t.Errorf("handoff made more than one commit")
	}
	if n := len(strings.Fields(gitIn(t, dir, "diff", "--name-only", before, plansRef))); n != 2 {
		t.Errorf("handoff wrote %d files, want the Handoff and its Event", n)
	}
	e, h := handoffOf(t, dir, id)
	if e.Type != core.Released || e.Task != "T-1" || e.Agent != "claude-code" || h.Done || h.Task != "T-1" ||
		h.Agent != "claude-code" || h.Note != "The client is in place.\n\nNext: the retries." || h.Author.Email != adaP.Email {
		t.Errorf("event %+v, handoff %+v", e, h)
	}
	setEnv(t, agentEnv, "")
	as(t, dir, benP)
	if out := mustBfc(t, dir, "claim", short, "T-1"); !strings.Contains(out, "claimed  T-1") {
		t.Errorf("claim of a Paused Task:\n%s", out)
	}
}

// Issue #109 AC-1, AC-3: with --done it completes the Task, the note comes
// from stdin, and --json prints the Handoff.
func TestHandoffCompletesATask(t *testing.T) {
	t.Parallel()
	dir, short, id := claimedPlan(t)
	var got struct {
		ID    string      `json:"id"`
		Plan  string      `json:"plan"`
		Task  string      `json:"task"`
		Done  bool        `json:"done"`
		Note  string      `json:"note"`
		Event string      `json:"event"`
		By    core.Person `json:"author"`
	}
	out := mustStdin(t, dir, "Done; tests in charge_test.go.\n", "handoff", short, "T-1", "--done", "--json")
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	e, h := handoffOf(t, dir, id)
	if got.ID != h.ID.String() || got.Plan != id.String() || got.Task != "T-1" || !got.Done || got.Note != "Done; tests in charge_test.go." ||
		got.Event != e.ID.String() || e.Type != core.Completed || !h.Done {
		t.Errorf("handoff --json = %+v; event %+v", got, e)
	}
	if out, err := bfcIn(t, dir, "claim", short, "T-1"); err == nil || !strings.Contains(out, "completed") {
		t.Errorf("claim of a completed Task: %v\n%s", err, out)
	}
}

// Issue #109 AC-2: only the holder hands off; nothing is written otherwise.
func TestHandoffRefusals(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	before := planCommits(t, dir)
	for name, c := range map[string]struct {
		as   core.Person
		args []string
		want string
	}{
		"not the holder": {benP, []string{short, "T-1", "-m", "x"}, "held by Ada Lovelace <ada@example.com>"},
		"open Task":      {adaP, []string{short, "T-2", "-m", "x"}, "T-2 of " + short + " is open"},
		"no Task":        {adaP, []string{short, "-m", "x"}, "T-n"},
		"empty note":     {adaP, []string{short, "T-1", "-m", " "}, "empty"},
	} {
		as(t, dir, c.as)
		out, err := bfcIn(t, dir, append([]string{"handoff"}, c.args...)...)
		if err == nil || !strings.Contains(out, c.want) {
			t.Errorf("%s: err %v, output lacks %q:\n%s", name, err, c.want, out)
		}
	}
	if planCommits(t, dir) != before {
		t.Errorf("a refused handoff wrote")
	}
}

// Issue #109 AC-3: bfc ready lists the Paused Tasks after the open ones,
// with the first line of the latest Handoff.
func TestReadyListsPausedTasksAfterOpenOnes(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	mustBfc(t, dir, "handoff", short, "T-1", "-m", "Half done.\nMore detail.")

	out := mustBfc(t, dir, "ready")
	open := strings.Index(out, short+"  T-2  Wire the flag,")
	paused := strings.Index(out, short+"  T-1  Add the client.  (Payments)  paused: Half done.")
	if open < 0 || paused < 0 || paused < open {
		t.Errorf("bfc ready:\n%s", out)
	}
	var got []struct {
		Task    string `json:"task"`
		State   string `json:"state"`
		Handoff string `json:"handoff"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "ready", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Task != "T-2" || got[0].State != "open" || got[1].Task != "T-1" || got[1].State != "paused" || got[1].Handoff == "" {
		t.Errorf("ready --json = %+v", got)
	}
	// Without a Task, bfc claim takes the open one first.
	if out := mustBfc(t, dir, "claim", short); !strings.Contains(out, "claimed  T-2") {
		t.Errorf("claim:\n%s", out)
	}
}

// Issue #109 AC-5: bfc show prints the Handoffs under their Task, --json
// has them, and a Handoff without its Event is a note that changes nothing.
func TestShowPrintsTheHandoffs(t *testing.T) {
	t.Parallel()
	dir, short, id := claimedPlan(t)
	mustBfc(t, dir, "handoff", short, "T-1", "-m", "Half done.\nMore detail.")
	stray := ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C9H")
	writeRef(t, dir, "stray", map[string][]byte{core.HandoffPath(id, stray): core.EncodeHandoff(core.Handoff{
		ID: stray, Author: benP, At: time.Now(), Task: "T-2", Note: "A note without its Event.",
	})})

	out := mustBfc(t, dir, "show", short)
	for _, want := range []string{
		"\n--- Tasks ---\n",
		"T-1  paused  Ada Lovelace  Add the client.",
		"  Handoff ", "by Ada Lovelace:", "    Half done.\n    More detail.",
		"T-2  open  -  Wire the flag,",
		"note without its Event", "A note without its Event.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("bfc show lacks %q:\n%s", want, out)
		}
	}
	var got struct {
		Tasks []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"tasks"`
		Handoffs []struct {
			ID    string `json:"id"`
			Task  string `json:"task"`
			Done  bool   `json:"done"`
			Note  string `json:"note"`
			Event string `json:"event"`
		} `json:"handoffs"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "show", short, "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Handoffs) != 2 || got.Handoffs[0].ID != stray.String() || got.Handoffs[0].Event != "" ||
		got.Handoffs[1].Task != "T-1" || got.Handoffs[1].Event == "" || got.Handoffs[1].Note != "Half done.\nMore detail." {
		t.Errorf("handoffs = %+v", got.Handoffs)
	}
	if got.Tasks[1].State != "open" {
		t.Errorf("the stray Handoff changed T-2: %+v", got.Tasks)
	}
}

// Issue #109 AC-5: bfc doctor warns about a Handoff file it cannot read.
func TestDoctorWarnsAboutAnUnreadableHandoff(t *testing.T) {
	t.Parallel()
	dir, short, id := claimedPlan(t)
	mustBfc(t, dir, "handoff", short, "T-1", "-m", "Half done.")
	bad := core.HandoffPath(id, ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C9H"))
	writeRef(t, dir, "broken handoff", map[string][]byte{bad: []byte("no front matter\n")})

	out, _ := bfcIn(t, dir, "doctor")
	wantDoctor(t, out, "warn", "handoff files", "1 of 2 cannot be read")
	if !strings.Contains(out, bad) {
		t.Errorf("doctor does not name %s:\n%s", bad, out)
	}
}

// Issue #109 AC-4: Prime shows the Current plan's Tasks with state and
// holder, the latest Handoff of a claimed or Paused Task in full, and tells
// the agent to hand off and sync before it stops.
func TestPrimeShowsTheTasksAndTheirHandoffs(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	mustBfc(t, dir, "handoff", short, "T-1", "-m", "Half done.\nNext: the retries.")
	mustBfc(t, dir, "claim", short, "T-2")
	mustBfc(t, dir, "use", short)

	out := mustBfc(t, dir, "prime")
	for _, want := range []string{
		"### Tasks",
		"- T-1 (paused, last held by ada@example.com): Add the client.",
		"Half done.\n  Next: the retries.",
		"- T-2 (claimed by ada@example.com): Wire the flag,",
		"bfc handoff",
		"bfc push",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prime lacks %q:\n%s", want, out)
		}
	}
}

// Issue #112 AC-1: the Task line of bfc show has the first line of its
// latest Handoff.
func TestShowTaskLinesHaveTheLatestHandoff(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	mustBfc(t, dir, "handoff", short, "T-1", "-m", "Half done.\nMore detail.")
	if out := mustBfc(t, dir, "show", short); !strings.Contains(out, "T-1  paused  Ada Lovelace  Add the client.  · Half done.\n") {
		t.Errorf("bfc show:\n%s", out)
	}
}

// Issue #112 AC-2: the TUI adapter gives the Tasks with state, holder, my
// own Claims, the latest Handoff and every Handoff of each Task.
func TestTUIAdapterGivesTheTasks(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	mustBfc(t, dir, "handoff", short, "T-1", "-m", "Half done.\nMore detail.")
	as(t, dir, benP)
	mustBfc(t, dir, "claim", short, "T-1")
	as(t, dir, adaP)
	mustBfc(t, dir, "claim", short, "T-2")

	d, err := adapterIn(t, dir).Detail(t.Context(), short)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Tasks) != 2 {
		t.Fatalf("tasks = %+v", d.Tasks)
	}
	t1, t2 := d.Tasks[0], d.Tasks[1]
	if t1.ID != "T-1" || t1.State != core.TaskClaimed || t1.Holder != "Ben Bitdiddle" || t1.Mine || t1.Latest != "Half done." ||
		len(t1.Handoffs) != 1 || t1.Handoffs[0].By != "Ada Lovelace" || t1.Handoffs[0].Note != "Half done.\nMore detail." || t1.Handoffs[0].Done {
		t.Errorf("T-1 = %+v", t1)
	}
	if t2.ID != "T-2" || t2.State != core.TaskClaimed || !t2.Mine || t2.Latest != "" || len(t2.Handoffs) != 0 {
		t.Errorf("T-2 = %+v", t2)
	}
}
