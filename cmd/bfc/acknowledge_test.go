package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// Issue #105 AC-1: bfc acknowledge writes a Message with ref
// checks/<ULID> in the plan that holds the check, found by the ULID alone,
// with the reason from -m or stdin.
func TestAcknowledgeWritesAMessageAboutTheCheck(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	newPlan(t, dir, "Another plan") // the check is found in the plan that holds it
	check := strings.TrimSpace(recordResult(t, dir, short, "7", sampleResult))

	out := strings.TrimSpace(mustBfc(t, dir, "acknowledge", check, "-m", "The email worker change is agreed."))

	msgID, err := ulid.ParseStrict(out)
	if err != nil {
		t.Fatalf("output is not a ULID: %q", out)
	}
	m, err := core.ParseMessage(msgID, []byte(gitIn(t, dir, "show", plansRef+":"+core.MessagePath(id, msgID))))
	if err != nil {
		t.Fatal(err)
	}
	if m.Ref != "checks/"+check || m.Body != "The email worker change is agreed." || m.Agent != "" || len(m.To) != 0 || m.Author.Email != adaP.Email {
		t.Errorf("message = %+v", m)
	}

	out = strings.TrimSpace(mustStdin(t, dir, "Also fine.\n", "acknowledge", check))
	if _, err := ulid.ParseStrict(out); err != nil {
		t.Errorf("stdin: %q", out)
	}
}

func TestAcknowledgeRefusesAnUnknownCheckAndAnEmptyReason(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	check := strings.TrimSpace(recordResult(t, dir, short, "7", sampleResult))
	before := planCommits(t, dir)

	if out, err := bfcIn(t, dir, "acknowledge", "01JD3K8Q2W5V7X9Y0Z1A2B3C4E", "-m", "x"); err == nil || !strings.Contains(out, "no plan check 01JD3K8Q2W5V7X9Y0Z1A2B3C4E") {
		t.Errorf("unknown check: %v\n%s", err, out)
	}
	if out, err := bfcIn(t, dir, "acknowledge", "bfc-xxxx", "-m", "x"); err == nil || !strings.Contains(out, "ULID") {
		t.Errorf("not a ULID: %v\n%s", err, out)
	}
	if out, err := bfcStdin(t, dir, "  \n", "acknowledge", check); err == nil || !strings.Contains(out, "empty") {
		t.Errorf("empty reason: %v\n%s", err, out)
	}
	if planCommits(t, dir) != before {
		t.Errorf("a refused acknowledge wrote")
	}
}

// Issue #105 AC-2: acknowledge is for people: it refuses inside an agent,
// and bfc setup claude denies it.
func TestAcknowledgeIsForPeople(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	check := strings.TrimSpace(recordResult(t, dir, short, "7", sampleResult))
	setEnv(t, agentEnv, "1")

	out, err := bfcIn(t, dir, "acknowledge", check, "-m", "x")
	if err == nil || !strings.Contains(out, "bfc acknowledge") {
		t.Errorf("inside an agent: %v\n%s", err, out)
	}

	mustBfc(t, dir, "setup", "claude")
	settings := readFile(t, filepath.Join(dir, ".claude", "settings.json"))
	if !strings.Contains(settings, `"Bash(bfc acknowledge:*)"`) {
		t.Errorf("settings do not deny acknowledge:\n%s", settings)
	}
}

// Issue #105 AC-3: bfc ask --ref takes checks/<ULID>.
func TestAskAboutACheck(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	check := strings.TrimSpace(recordResult(t, dir, short, "7", sampleResult))
	mustBfc(t, dir, "ask", short, "--to", "ben@example.com", "--ref", "checks/"+check, "-m", "Is AC-1 really unmet?")
	if out, err := bfcIn(t, dir, "ask", short, "--to", "ben@example.com", "--ref", "checks/nope", "-m", "x"); err == nil {
		t.Errorf("a broken check ref was accepted:\n%s", out)
	}
}

// Issue #105 AC-4, AC-5: show marks an acknowledged check with the reason;
// --json has acknowledged on each check and on its Findings, and a newer
// check of the same pull request needs its own Acknowledgement.
func TestShowMarksAcknowledgedChecks(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	first := strings.TrimSpace(recordResult(t, dir, short, "7", sampleResult))
	ackID := strings.TrimSpace(mustBfc(t, dir, "acknowledge", first, "-m", "Agreed with Ben."))

	out := mustBfc(t, dir, "show", short)
	if !strings.Contains(out, "acknowledged by Ada Lovelace: Agreed with Ben.") {
		t.Errorf("show lacks the Acknowledgement:\n%s", out)
	}
	var got struct {
		Checks []struct {
			ID           string `json:"id"`
			Acknowledged *struct {
				ID     string      `json:"id"`
				Author core.Person `json:"author"`
				At     string      `json:"at"`
				Reason string      `json:"reason"`
			} `json:"acknowledged"`
		} `json:"checks"`
		Findings []struct {
			Acknowledged bool `json:"acknowledged"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "show", short, "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Checks) != 1 || got.Checks[0].Acknowledged == nil || got.Checks[0].Acknowledged.ID != ackID ||
		got.Checks[0].Acknowledged.Reason != "Agreed with Ben." || got.Checks[0].Acknowledged.Author.Email != adaP.Email {
		t.Errorf("checks = %+v", got.Checks)
	}
	if len(got.Findings) != 2 || !got.Findings[0].Acknowledged || !got.Findings[1].Acknowledged {
		t.Errorf("findings = %+v", got.Findings)
	}

	recordResult(t, dir, short, "7", sampleResult)
	js := mustBfc(t, dir, "show", short, "--json")
	if !strings.Contains(js, `"acknowledged": null`) || strings.Contains(js, `"acknowledged": true`) {
		t.Errorf("a newer check is acknowledged by the old Acknowledgement:\n%s", js)
	}
}
