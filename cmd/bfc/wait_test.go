package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// waitResult is what one bfc run in a goroutine ended with.
type waitResult struct {
	out string
	err error
}

// runAsync runs bfc in dir in the background.
func runAsync(t *testing.T, dir string, args ...string) <-chan waitResult {
	t.Helper()
	done := make(chan waitResult, 1)
	go func() {
		out, err := bfcIn(t, dir, args...)
		done <- waitResult{out, err}
	}()
	return done
}

// openQuestion returns the ULID of the open Question of the plan in dir,
// "" while there is none.
func openQuestion(t *testing.T, dir, short string) string {
	t.Helper()
	var got struct {
		Open []struct {
			ID string `json:"id"`
		} `json:"open_questions"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "show", short, "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Open) == 0 {
		return ""
	}
	return got.Open[0].ID
}

// Issue #114 AC-1, AC-6: bfc ask --wait in Ana's clone pushes the Question
// and pulls until Ben's answer from his clone arrives, then prints it.
func TestAskWaitGetsTheAnswerFromAnotherClone(t *testing.T) {
	t.Parallel()
	ana, ben, _ := twoClones(t)
	short, _ := newPlan(t, ana, "Payments")
	mustBfc(t, ana, "push")

	waiting := runAsync(t, ana, "ask", short, "--to", "ben@example.com", "-m", "EUR only?", "--wait", "--timeout", "2m", "--interval", "50ms")

	var q string
	for deadline := time.Now().Add(time.Minute); q == "" && time.Now().Before(deadline); {
		mustBfc(t, ben, "pull")
		q = openQuestion(t, ben, short)
	}
	if q == "" {
		t.Fatal("the Question never reached Ben")
	}
	mustBfc(t, ben, "reply", q, "-m", "Yes, EUR only.")
	mustBfc(t, ben, "push")

	select {
	case r := <-waiting:
		if r.err != nil || !strings.Contains(r.out, "Yes, EUR only.") || !strings.Contains(r.out, "ben@example.com") {
			t.Errorf("ask --wait: err %v\n%s", r.err, r.out)
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("ask --wait did not return")
	}
}

// Issue #114 AC-2: on timeout it exits 2 with the Question's ULID and the
// command to keep waiting.
func TestAskWaitTimesOut(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")

	out, err := bfcIn(t, dir, "ask", short, "--to", "ben@example.com", "-m", "EUR only?", "--wait", "--timeout", "150ms", "--interval", "20ms")

	var exit *exitCodeError
	if !errors.As(err, &exit) || exit.code != 2 {
		t.Fatalf("err = %v, want exit code 2\n%s", err, out)
	}
	q := openQuestion(t, dir, short)
	if q == "" || !strings.Contains(out, q) || !strings.Contains(out, "bfc wait "+q) {
		t.Errorf("output lacks the Question %q and how to keep waiting:\n%s", q, out)
	}
}

// Issue #114 AC-3, AC-4: bfc wait returns at once for an answered Question,
// and in Local mode an answer written in the same clone ends the wait.
func TestWaitInLocalMode(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	q := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "-m", "EUR only?"))

	waiting := runAsync(t, dir, "wait", q, "--timeout", "1m", "--interval", "20ms", "--json")
	mustBfc(t, dir, "reply", q, "-m", "Yes.")
	select {
	case r := <-waiting:
		var got struct {
			ReplyTo string `json:"reply_to"`
			Body    string `json:"body"`
		}
		if r.err != nil || json.Unmarshal([]byte(r.out), &got) != nil || got.ReplyTo != q || got.Body != "Yes." {
			t.Errorf("wait --json: err %v\n%s", r.err, r.out)
		}
	case <-time.After(time.Minute):
		t.Fatal("bfc wait did not return")
	}

	start := time.Now()
	if out := mustBfc(t, dir, "wait", q, "--timeout", "1m"); !strings.Contains(out, "Yes.") || time.Since(start) > 30*time.Second {
		t.Errorf("wait on an answered Question:\n%s", out)
	}
	if out, err := bfcIn(t, dir, "wait", "01JD3K8Q2W5V7X9Y0Z1A2B3C4E"); err == nil || !strings.Contains(out, "no Message") {
		t.Errorf("wait on an unknown Message: %v\n%s", err, out)
	}
}
