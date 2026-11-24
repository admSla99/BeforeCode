package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// inboxed is what bfc inbox --json lists, here only the Findings.
type inboxed struct {
	Findings []struct {
		ShortID    string `json:"short_id"`
		PR         int    `json:"pr"`
		Check      string `json:"check"`
		Criterion  string `json:"criterion"`
		OutOfScope string `json:"out_of_scope"`
	} `json:"findings"`
}

func (d *dev) inbox() inboxed {
	d.t.Helper()
	var w inboxed
	if err := json.Unmarshal([]byte(d.bfc("inbox", "--json")), &w); err != nil {
		d.t.Fatalf("%s: bfc inbox --json: %v", d.name, err)
	}
	return w
}

// recordCheck runs bfc check record in the developer's clone for a pull
// request, with the result in the testdata file on stdin, as the plan check
// agent does on request (ADR 0015), and pushes it. It returns the check's ULID.
func (d *dev) recordCheck(short string, pr int, fixture string) string {
	d.t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		d.t.Fatal(err)
	}
	// The commits of the pull request only end up in the check file.
	r := d.run(string(data), nil, bfcBin, "check", "record", short, "--pr", strconv.Itoa(pr), "--head", "9f2c", "--base", "1a7e")
	if r.err != nil {
		d.t.Fatalf("%s: bfc check record: %v\n%s%s", d.name, r.err, r.stdout, r.stderr)
	}
	d.bfc("push")
	return strings.TrimSpace(r.stdout)
}

// Issue #107, the phase 3 exit criterion without the network: a pull
// request that departs from its approved plan gets a Finding, recorded in
// one clone and seen in the plan author's Inbox in the other. An
// Acknowledgement, or a clean newer check of the same pull request, takes
// it out.
func TestAFindingReachesThePlanAuthorsInbox(t *testing.T) {
	ana, bare := startProject(t)
	short := ana.newPlan("Payments via Stripe")
	ana.writePlan(short)
	ana.bfc("submit", short)
	ana.bfc("push")

	// Ben approves; the plan check of his clone stands in for GitHub Actions.
	ben := clone(t, bare, "Ben")
	ben.bfc("bootstrap")
	ben.bfc("approve", short)
	ben.bfc("push")
	ana.bfc("pull")
	if p := ana.list()["Payments via Stripe"]; p.State != "approved" {
		t.Fatalf("ana: the plan is %+v", p)
	}
	if w := ana.inbox(); len(w.Findings) != 0 {
		t.Fatalf("ana: Findings before any check: %+v", w.Findings)
	}

	// AC-1: a check of pull request 7 with a Finding, recorded in Ben's
	// clone, reaches Ana's Inbox after her pull.
	check := ben.recordCheck(short, 7, "plan-check-reply.md")
	ana.bfc("pull")
	w := ana.inbox()
	if len(w.Findings) != 2 || w.Findings[0].ShortID != short || w.Findings[0].PR != 7 || w.Findings[0].Check != check ||
		w.Findings[0].Criterion != "AC-1" || w.Findings[1].OutOfScope != "Adds a retry loop to the email worker." {
		t.Errorf("ana: Inbox after the check = %+v", w.Findings)
	}
	if out := ana.bfc("inbox"); !strings.Contains(out, short+"  PR #7  AC-1 is not met") {
		t.Errorf("ana: bfc inbox:\n%s", out)
	}

	// AC-1: Ana accepts it on purpose; the Finding leaves her Inbox and the
	// reason stays in the Discussion, for Ben too.
	ana.bfc("acknowledge", check, "-m", "The retry loop is agreed; AC-1 follows in the next pull request.")
	if w := ana.inbox(); len(w.Findings) != 0 {
		t.Errorf("ana: Findings after the Acknowledgement: %+v", w.Findings)
	}
	ana.bfc("push")
	ben.bfc("pull")
	if out := ben.bfc("show", short); !strings.Contains(out, "acknowledged by Ana: The retry loop is agreed") {
		t.Errorf("ben: show lacks the Acknowledgement:\n%s", out)
	}

	// AC-2: pull request 8 departs from the plan too, then a newer, clean
	// check of it takes its Finding out.
	ben.recordCheck(short, 8, "plan-check-reply.md")
	ana.bfc("pull")
	if w := ana.inbox(); len(w.Findings) != 2 || w.Findings[0].PR != 8 {
		t.Errorf("ana: Inbox after the check of pull request 8 = %+v", w.Findings)
	}
	ben.recordCheck(short, 8, "plan-check-clean.json")
	ana.bfc("pull")
	if w := ana.inbox(); len(w.Findings) != 0 {
		t.Errorf("ana: Findings after the clean check: %+v", w.Findings)
	}
	ana.sameRefAs(bare)
}
