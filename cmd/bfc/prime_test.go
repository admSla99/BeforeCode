package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// golden compares got with testdata/<test name>.golden, or rewrites it with
// -update.
func golden(t *testing.T, got string) {
	t.Helper()
	path := filepath.Join("testdata", t.Name()+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run go test ./cmd/bfc -run %s -update", err, t.Name())
	}
	if got != string(want) {
		t.Errorf("output differs from %s (go test ./cmd/bfc -run %s -update rewrites it):\n%s", path, t.Name(), got)
	}
}

func renderPrimeString(t *testing.T, v primeView) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderPrime(&b, v); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

var (
	primeAt  = time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	planID   = ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")
	question = core.Message{
		ID: ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4E"), Author: benP, At: primeAt,
		To: []string{"ada@example.com"}, Ref: "plan.md#AC-1", Body: "Which card?\nAny card.",
	}
)

func paymentsPlan(state core.State) *index.Plan {
	return &index.Plan{
		ID: planID, ShortID: "bfc-5v7x", Title: "Payments via Stripe",
		Status: core.Status{State: state, Author: adaP, RequiredApprovals: 1, Approvers: []string{"ben@example.com"}},
		Intent: []byte("---\ntitle: Payments via Stripe\n---\n\n## Problem\n\nCards fail.\n"),
		Plan:   []byte(validPlan),
	}
}

// AC-2 and AC-3: the instructions, Waiting on you for the current
// identity, then the Current plan with its open Questions.
func TestPrimeWithACurrentPlan(t *testing.T) {
	t.Parallel()
	golden(t, renderPrimeString(t, primeView{
		Identity: adaP,
		Plan:     paymentsPlan(core.StateApproved), Current: true,
		Open:      []core.Message{question},
		Reviews:   []planRef{{ShortID: "bfc-9k2m", Title: "Refunds"}},
		Questions: []waitingQuestion{{ShortID: "bfc-5v7x", Q: question}},
	}))
}

// AC-4: without a Current plan, the approved plans and how to start one.
func TestPrimeWithoutACurrentPlan(t *testing.T) {
	t.Parallel()
	golden(t, renderPrimeString(t, primeView{
		Identity: adaP,
		Approved: []planRef{{ShortID: "bfc-5v7x", Title: "Payments via Stripe"}, {ShortID: "bfc-9k2m", Title: "Refunds"}},
	}))
}

// AC-6: without a plans ref, how to fix it.
func TestPrimeWithoutAPlansRef(t *testing.T) {
	t.Parallel()
	golden(t, renderPrimeString(t, primeView{Setup: errNotInitialised.Error()}))
}

// AC-2: a plan given by ID that has no plan.md yet shows intent.md; a plan
// that cannot be found is a note.
func TestPrimeOfAGivenDraftShowsTheIntent(t *testing.T) {
	t.Parallel()
	p := paymentsPlan(core.StateDraft)
	p.Plan, p.Status.RequiredApprovals, p.Status.Approvers = nil, 0, nil
	golden(t, renderPrimeString(t, primeView{Identity: adaP, Plan: p}))
}

func TestPrimeWithANote(t *testing.T) {
	t.Parallel()
	golden(t, renderPrimeString(t, primeView{Identity: adaP, Note: `no plan "bfc-zzzz"; bfc list shows the plans`}))
}

// AC-5: a long plan.md is cut so that the output stays within the cap,
// with a note to run bfc show.
func TestPrimeCutsALongPlan(t *testing.T) {
	t.Parallel()
	p := paymentsPlan(core.StateApproved)
	p.Plan = []byte(validPlan + strings.Repeat("- AC-2: a line that repeats to make plan.md long.\n", 400))
	out := renderPrimeString(t, primeView{Identity: adaP, Plan: p, Current: true})
	if len(out) > primeLimit {
		t.Errorf("output is %d bytes, more than %d", len(out), primeLimit)
	}
	if want := "```\n\nplan.md is cut here; run `bfc show bfc-5v7x` for all of it.\n"; !strings.HasSuffix(out, want) {
		t.Errorf("output does not end with the cut note:\n%s", out[len(out)-300:])
	}
	if !strings.Contains(out, "## Acceptance criteria\n") {
		t.Error("the start of plan.md is missing")
	}
}

// AC-2 and AC-3 end to end: Ben's clone primes Ada's plan as the Current
// plan, and lists what waits on Ben: her other plan in review and her
// Question to him.
func TestPrimeCommandGathersThePlanAndWhatWaitsOnTheUser(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	review, rid := newPlan(t, dir, "Refunds")
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(rid) + "/plan.md": []byte(validPlan)})
	mustBfc(t, dir, "submit", review)
	q := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ben@example.com", "-m", "Which card?"))
	mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "-m", "Not for Ben")
	as(t, dir, benP)
	mustBfc(t, dir, "use", short)

	out := mustBfc(t, dir, "prime")
	for _, want := range []string{
		"For Ben Bitdiddle <ben@example.com>",
		"- Review " + review + " Refunds: tell the user",
		"- Question " + q + " in " + short + " from ada@example.com: Which card?",
		"## Current plan: " + short + " Payments\n",
		"- State: draft.",
		"to ben@example.com: Which card?",
		"- AC-1: An invalid card returns 402.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "- Question") && strings.Contains(out, "from ada@example.com: Not for Ben") {
		t.Errorf("a Question to someone else waits on Ben:\n%s", out)
	}
	// The same plan given by ID is not the Current plan.
	if out := mustBfc(t, dir, "prime", short); !strings.Contains(out, "## Plan: "+short) {
		t.Errorf("bfc prime %s:\n%s", short, out)
	}
}

// AC-4: without a Current plan, the approved plans.
func TestPrimeCommandListsTheApprovedPlans(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, benP)
	mustBfc(t, dir, "approve", short)
	newPlan(t, dir, "Still a draft")

	out := mustBfc(t, dir, "prime")
	if !strings.Contains(out, "Approved plans:\n\n- "+short+" Payments\n\nTo work") {
		t.Errorf("bfc prime:\n%s", out)
	}
}

// AC-6: without a plans ref, or with a plan that cannot be found, bfc prime
// still prints the instructions and exits 0.
func TestPrimeCommandNeverFails(t *testing.T) {
	t.Parallel()
	plain := t.TempDir()
	gitIn(t, plain, "init", "-q")
	if out := mustBfc(t, plain, "prime"); !strings.Contains(out, "## Setup\n\nBeforeCode is not set up") || !strings.Contains(out, "bfc bootstrap") {
		t.Errorf("without a plans ref:\n%s", out)
	}
	dir := newLocalRepo(t)
	if out := mustBfc(t, dir, "prime", "bfc-zzzz"); !strings.Contains(out, `no plan "bfc-zzzz"`) {
		t.Errorf("unknown plan:\n%s", out)
	}
}

// AC-7: a second argument makes the first a topic; an unknown topic is an
// error that lists the known ones.
func TestPrimeCommandRefusesAnUnknownTopic(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	out, err := bfcIn(t, dir, "prime", "grilling", "bfc-zzzz")
	if err == nil || !strings.Contains(out, `unknown topic "grilling"`) || !strings.Contains(out, "known topics: none yet") {
		t.Errorf("err = %v\n%s", err, out)
	}
}
