package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// clock sets the commit time of the next plans-ref writes.
func clock(t *testing.T, minute int) {
	t.Helper()
	ts := time.Date(2026, 11, 16, 9, minute, 0, 0, time.UTC).Format(time.RFC3339)
	setEnv(t, "GIT_COMMITTER_DATE", ts)
	setEnv(t, "GIT_AUTHOR_DATE", ts)
}

// newPlan runs bfc new --no-edit and returns the plan's Short ID and ULID.
func newPlan(t *testing.T, dir, title string) (string, ulid.ULID) {
	t.Helper()
	out, err := bfcIn(t, dir, "new", "--no-edit", title)
	if err != nil {
		t.Fatalf("bfc new: %v\n%s", err, out)
	}
	short := strings.Fields(out)[0]
	for _, d := range planDirs(t, dir) {
		events := gitIn(t, dir, "ls-tree", "--name-only", plansRef+":plans/"+d+"/events")
		data := gitIn(t, dir, "show", plansRef+":plans/"+d+"/events/"+strings.Fields(events)[0])
		if strings.Contains(data+"\n", "short_id: "+short+"\n") {
			return short, ulid.MustParseStrict(d)
		}
	}
	t.Fatalf("plan %s not found", short)
	return "", ulid.ULID{}
}

var eventSeq uint64

// addEvent writes an event for plan straight into the plans ref, as the
// commands of later issues will.
func addEvent(t *testing.T, dir string, plan ulid.ULID, e core.Event) {
	t.Helper()
	eventSeq++
	e.ID = ulid.MustNew(ulid.Now()+eventSeq, strings.NewReader(strings.Repeat("\x01", 16)))
	e.Plan = plan
	if e.At.IsZero() {
		e.At = time.Now()
	}
	writeRef(t, dir, string(e.Type), map[string][]byte{
		core.PlanDir(plan) + "/events/" + e.ID.String() + ".yaml": core.EncodeEvent(e),
	})
}

func writeRef(t *testing.T, dir, msg string, files map[string][]byte) {
	t.Helper()
	s, err := store.Open(dir, envOf(t)...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(context.Background(), msg, files); err != nil {
		t.Fatal(err)
	}
}

// planBlob returns the object ID of a plan's plan.md.
func planBlob(t *testing.T, dir string, plan ulid.ULID) string {
	t.Helper()
	return gitIn(t, dir, "rev-parse", plansRef+":"+core.PlanDir(plan)+"/plan.md")
}

var (
	adaP  = core.Person{Name: "Ada Lovelace", Email: "ada@example.com"}
	janaP = core.Person{Name: "Jana Nováková", Email: "jana@example.com"}
)

// threePlans sets up: "Old draft" (09:01), "Approved one" (activity 09:05)
// and "In review" (activity 09:04).
func threePlans(t *testing.T) (dir string, ids map[string]ulid.ULID, shorts map[string]string) {
	t.Helper()
	dir = newLocalRepo(t)
	ids, shorts = map[string]ulid.ULID{}, map[string]string{}
	for i, title := range []string{"Old draft", "Approved one", "In review"} {
		clock(t, i+1)
		shorts[title], ids[title] = newPlan(t, dir, title)
	}
	clock(t, 4)
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(ids["In review"]) + "/plan.md": []byte("## Context\n")})
	addEvent(t, dir, ids["In review"], core.Event{Type: core.Submitted, Author: adaP, PlanBlob: planBlob(t, dir, ids["In review"]), RequiredApprovals: 2})
	clock(t, 5)
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(ids["Approved one"]) + "/plan.md": []byte("## Context\n\nThe plan.\n")})
	blob := planBlob(t, dir, ids["Approved one"])
	addEvent(t, dir, ids["Approved one"], core.Event{Type: core.Submitted, Author: adaP, PlanBlob: blob, RequiredApprovals: 1})
	addEvent(t, dir, ids["Approved one"], core.Event{Type: core.Approved, Author: janaP, PlanBlob: blob})
	return dir, ids, shorts
}

var listLine = regexp.MustCompile(`^(bfc-\S+)\s+(\S+)\s+(.+?)\s+(\S+@\S+)\s+(\d{4}-\d\d-\d\d \d\d:\d\d)$`)

func TestListShowsOneLinePerPlanNewestActivityFirst(t *testing.T) {
	t.Parallel()
	dir, _, shorts := threePlans(t)

	out, err := bfcIn(t, dir, "list")
	if err != nil {
		t.Fatalf("bfc list: %v\n%s", err, out)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	want := [][3]string{
		{shorts["Approved one"], "approved", "Approved one"},
		{shorts["In review"], "in-review", "In review"},
		{shorts["Old draft"], "draft", "Old draft"},
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(want), out)
	}
	for i, w := range want {
		m := listLine.FindStringSubmatch(lines[i])
		if m == nil {
			t.Errorf("line %d does not have the expected columns: %q", i, lines[i])
			continue
		}
		if m[1] != w[0] || m[2] != w[1] || m[3] != w[2] || m[4] != "ada@example.com" {
			t.Errorf("line %d = %q, want %v by ada@example.com", i, lines[i], w)
		}
	}
	if !strings.HasSuffix(lines[2], time.Date(2026, 11, 16, 9, 1, 0, 0, time.UTC).Local().Format("2006-01-02 15:04")) {
		t.Errorf("last activity of the old draft is not its creation time: %q", lines[2])
	}
}

func TestListFiltersByDerivedState(t *testing.T) {
	t.Parallel()
	dir, _, shorts := threePlans(t)

	out, err := bfcIn(t, dir, "list", "--state", "in-review")
	if err != nil {
		t.Fatalf("bfc list --state: %v\n%s", err, out)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 || !strings.HasPrefix(lines[0], shorts["In review"]+" ") {
		t.Errorf("bfc list --state in-review =\n%s", out)
	}

	out, err = bfcIn(t, dir, "list", "--state", "reviewing")
	if err == nil || !strings.Contains(out, "in-review") || !strings.Contains(out, "draft") {
		t.Errorf("unknown state: err %v, output does not list valid states:\n%s", err, out)
	}
}

func TestListJSON(t *testing.T) {
	t.Parallel()
	dir, ids, shorts := threePlans(t)

	out, err := bfcIn(t, dir, "list", "--json")
	if err != nil {
		t.Fatalf("bfc list --json: %v\n%s", err, out)
	}

	var got []struct {
		ID                string   `json:"id"`
		ShortID           string   `json:"short_id"`
		State             string   `json:"state"`
		Title             string   `json:"title"`
		Labels            []string `json:"labels"`
		Author            struct{ Name, Email string }
		LastActivity      string `json:"last_activity"`
		Approvals         int    `json:"approvals"`
		RequiredApprovals int    `json:"required_approvals"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 3 {
		t.Fatalf("got %d plans, want 3", len(got))
	}
	first := got[0]
	if first.ID != ids["Approved one"].String() || first.ShortID != shorts["Approved one"] || first.State != "approved" ||
		first.Title != "Approved one" || first.Author.Email != "ada@example.com" || first.Author.Name != "Ada Lovelace" ||
		first.LastActivity != "2026-11-16T09:05:00Z" || first.Approvals != 1 || first.RequiredApprovals != 1 || first.Labels == nil {
		t.Errorf("first plan = %+v", first)
	}
	if got[1].Approvals != 0 || got[1].RequiredApprovals != 2 {
		t.Errorf("in-review plan approvals = %d/%d, want 0/2", got[1].Approvals, got[1].RequiredApprovals)
	}
}

func TestListWithNoPlansPrintsNothingAndJSONAnEmptyArray(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)

	if out, err := bfcIn(t, dir, "list"); err != nil || out != "" {
		t.Errorf("bfc list = %q, %v; want no output", out, err)
	}
	if out, err := bfcIn(t, dir, "list", "--json"); err != nil || strings.TrimSpace(out) != "[]" {
		t.Errorf("bfc list --json = %q, %v; want []", out, err)
	}
}

func TestListKeepsAPlanWithABrokenIntent(t *testing.T) {
	t.Parallel()
	dir, ids, shorts := threePlans(t)
	writeRef(t, dir, "break", map[string][]byte{core.PlanDir(ids["Old draft"]) + "/intent.md": []byte("no front matter\n")})

	out, err := bfcIn(t, dir, "list")
	if err != nil {
		t.Fatalf("bfc list: %v\n%s", err, out)
	}
	if !lineHas(out, shorts["Old draft"], "(unreadable intent.md)") {
		t.Errorf("plan with a broken intent is not listed as unreadable:\n%s", out)
	}
}

func TestListBeforeInitPointsToInit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")

	out, err := bfcIn(t, dir, "list")
	if err == nil || !strings.Contains(out, "bfc init") {
		t.Errorf("bfc list before init: %v\n%s", err, out)
	}
}

func TestShowPrintsHeaderIntentAndPlan(t *testing.T) {
	t.Parallel()
	dir, ids, shorts := threePlans(t)
	prefix := strings.TrimPrefix(shorts["Approved one"], "bfc-")

	for _, query := range []string{shorts["Approved one"], prefix, ids["Approved one"].String()} {
		out, err := bfcIn(t, dir, "show", query)
		if err != nil {
			t.Fatalf("bfc show %s: %v\n%s", query, err, out)
		}
		for _, want := range []string{
			shorts["Approved one"] + "  Approved one\n",
			"ID:         " + ids["Approved one"].String() + "\n",
			"State:      approved\n",
			"Author:     Ada Lovelace <ada@example.com>\n",
			"Approvals:  1/1 (jana@example.com)\n",
			"--- intent.md ---\n---\ntitle: Approved one\n",
			"--- plan.md ---\n## Context\n\nThe plan.\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("bfc show %s lacks %q:\n%s", query, want, out)
			}
		}
	}
}

func TestShowWithoutPlanMdShowsOnlyTheIntent(t *testing.T) {
	t.Parallel()
	dir, _, shorts := threePlans(t)

	out, err := bfcIn(t, dir, "show", shorts["Old draft"])
	if err != nil {
		t.Fatalf("bfc show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "State:      draft\n") || !strings.Contains(out, "Approvals:  -\n") {
		t.Errorf("header:\n%s", out)
	}
	if !strings.Contains(out, "--- intent.md ---") || strings.Contains(out, "--- plan.md ---") {
		t.Errorf("sections:\n%s", out)
	}
}

func TestShowUnknownOrAmbiguousIDListsCandidates(t *testing.T) {
	t.Parallel()
	dir, _, _ := threePlans(t)

	out, err := bfcIn(t, dir, "show", "bfc-zzzzzz")
	if err == nil || !strings.Contains(out, "no plan") || !strings.Contains(out, "bfc list") {
		t.Errorf("unknown ID: %v\n%s", err, out)
	}

	// Two plans that share the first four random characters.
	a := ulid.MustParse("01JD3K8Q2W5V7X1Y0Z1A2B3C4D")
	b := ulid.MustParse("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")
	for _, p := range []struct {
		id    ulid.ULID
		short string
		title string
	}{{a, "bfc-5v7x1", "First twin"}, {b, "bfc-5v7x9", "Second twin"}} {
		created := core.Event{ID: p.id, Type: core.Created, Plan: p.id, Author: adaP, At: time.Now(), ShortID: p.short}
		writeRef(t, dir, "twin", map[string][]byte{
			core.PlanDir(p.id) + "/intent.md":                         []byte("---\ntitle: " + p.title + "\n---\n"),
			core.PlanDir(p.id) + "/events/" + p.id.String() + ".yaml": core.EncodeEvent(created),
		})
	}

	out, err = bfcIn(t, dir, "show", "bfc-5v7x")
	if err == nil {
		t.Fatalf("ambiguous ID resolved:\n%s", out)
	}
	for _, want := range []string{"bfc-5v7x1", "First twin", "bfc-5v7x9", "Second twin"} {
		if !strings.Contains(out, want) {
			t.Errorf("candidates lack %q:\n%s", want, out)
		}
	}
}

func TestListAndShowCarryTheIntentLabels(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	writeRef(t, dir, "labels", map[string][]byte{
		core.PlanDir(id) + "/intent.md": []byte("---\ntitle: Payments\nlabels: [payments, backend]\n---\n"),
	})

	out, err := bfcIn(t, dir, "list", "--json")
	if err != nil || !strings.Contains(out, "\"labels\": [\n      \"payments\",\n      \"backend\"\n    ]") {
		t.Errorf("bfc list --json labels: %v\n%s", err, out)
	}
	out, err = bfcIn(t, dir, "show", short)
	if err != nil || !strings.Contains(out, "Labels:     payments, backend\n") {
		t.Errorf("bfc show labels: %v\n%s", err, out)
	}
}

func TestListShowsAnInvalidPlanWithAComputedShortID(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	id := ulid.MustParse("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")
	writeRef(t, dir, "no created event", map[string][]byte{
		core.PlanDir(id) + "/intent.md": []byte("---\ntitle: Orphan\n---\n"),
	})

	out, err := bfcIn(t, dir, "list")
	if err != nil {
		t.Fatalf("bfc list: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "bfc-5v7x  invalid  Orphan  -  ") {
		t.Errorf("invalid plan line = %q", out)
	}
	if out, err := bfcIn(t, dir, "show", "bfc-5v7x"); err != nil || !strings.Contains(out, "State:      invalid\n") {
		t.Errorf("bfc show of the invalid plan: %v\n%s", err, out)
	}
}
