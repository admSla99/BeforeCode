package core_test

import (
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

const template = `---
title: 
labels: []
---

## Problem

<!-- What is wrong or missing today? -->
`

func TestFillIntentTemplateSetsTheTitle(t *testing.T) {
	got := string(core.FillIntentTemplate([]byte(template), `Payments via "Stripe": v2`))

	intent, err := core.ParseIntent([]byte(got))
	if err != nil {
		t.Fatalf("ParseIntent: %v\n%s", err, got)
	}
	if intent.Title != `Payments via "Stripe": v2` {
		t.Errorf("title = %q", intent.Title)
	}
	if !strings.Contains(got, "## Problem\n\n<!-- What is wrong or missing today? -->\n") {
		t.Errorf("body of the template was not kept:\n%s", got)
	}
}

func TestFillIntentTemplateWithoutATitleLineAddsOne(t *testing.T) {
	for _, tpl := range []string{"---\nlabels: []\n---\n\n## Problem\n", "## Problem\n"} {
		got := core.FillIntentTemplate([]byte(tpl), "Payments")
		intent, err := core.ParseIntent(got)
		if err != nil || intent.Title != "Payments" {
			t.Errorf("template %q gave %+v, %v:\n%s", tpl, intent, err, got)
		}
	}
}

func TestParseIntentChecksTitleAndLabels(t *testing.T) {
	good := "---\ntitle: Payments\nlabels: [payments, back-end2]\n---\n\nbody\n"
	intent, err := core.ParseIntent([]byte(good))
	if err != nil {
		t.Fatalf("ParseIntent: %v", err)
	}
	if intent.Title != "Payments" || strings.Join(intent.Labels, ",") != "payments,back-end2" {
		t.Errorf("ParseIntent = %+v", intent)
	}

	for name, data := range map[string]string{
		"no front matter":  "# Payments\n",
		"unclosed":         "---\ntitle: Payments\n",
		"empty title":      "---\ntitle: \"  \"\n---\n",
		"no title":         "---\nlabels: []\n---\n",
		"two-line title":   "---\ntitle: \"a\\nb\"\n---\n",
		"upper-case label": "---\ntitle: P\nlabels: [Payments]\n---\n",
		"broken yaml":      "---\ntitle: [\n---\n",
	} {
		if _, err := core.ParseIntent([]byte(data)); err == nil {
			t.Errorf("%s: ParseIntent succeeded", name)
		}
	}
}

// sequence returns the given ULIDs one after another.
func sequence(ids ...string) func() ulid.ULID {
	return func() ulid.ULID {
		id := ulid.MustParse(ids[0])
		ids = ids[1:]
		return id
	}
}

func TestNewPlanMakesTheIntentAndACreatedEvent(t *testing.T) {
	intent := []byte("---\ntitle: Payments via Stripe\nlabels: []\n---\n\n## Problem\n\nCards fail.\n")
	existing := []ulid.ULID{ulid.MustParse("01JA0000005V7XAAAAAAAAAAAA")}

	plan, files, err := core.NewPlan(intent, ada, at, sequence(planID.String(), eventID.String()), existing)
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}

	if plan.ID != planID || plan.ShortID != "bfc-5v7x9" || plan.Title != "Payments via Stripe" || plan.Author != ada {
		t.Errorf("plan = %+v", plan)
	}
	if len(files) != 2 {
		t.Errorf("files = %v, want intent.md and one event", keys(files))
	}
	if got := string(files["plans/01JD3K8Q2W5V7X9Y0Z1A2B3C4D/intent.md"]); got != string(intent) {
		t.Errorf("intent.md = %q", got)
	}
	data, ok := files["plans/01JD3K8Q2W5V7X9Y0Z1A2B3C4D/events/01JD3K8Q2X0000000000000000.yaml"]
	if !ok {
		t.Fatalf("no created event in %v", keys(files))
	}
	e, err := core.ParseEvent(eventID, data)
	if err != nil {
		t.Fatal(err)
	}
	want := core.Event{ID: eventID, Type: core.Created, Plan: planID, Author: ada, At: at, ShortID: "bfc-5v7x9"}
	if e != want {
		t.Errorf("event = %+v, want %+v", e, want)
	}
	if got := core.DeriveState(plan.ID, []core.Event{e}); got != core.Draft {
		t.Errorf("state = %q, want draft", got)
	}
}

func TestNewPlanRejectsAnIntentWithoutTitle(t *testing.T) {
	_, _, err := core.NewPlan([]byte("---\ntitle: \n---\n"), ada, at, sequence(planID.String(), eventID.String()), nil)
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Errorf("NewPlan error = %v, want one about the title", err)
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
