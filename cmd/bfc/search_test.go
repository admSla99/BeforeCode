package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/core"
)

func TestListIsTheSameAfterTheIndexIsDeleted(t *testing.T) {
	dir, _, _ := threePlans(t)
	db := filepath.Join(dir, ".git", "beforecode", "index.db")

	var before []string
	for _, args := range [][]string{{"list"}, {"list", "--json"}} {
		out, err := bfcIn(t, dir, args...)
		if err != nil {
			t.Fatalf("bfc %v: %v\n%s", args, err, out)
		}
		before = append(before, out)
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("bfc list did not build the index: %v", err)
	}
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	for i, args := range [][]string{{"list"}, {"list", "--json"}} {
		out, err := bfcIn(t, dir, args...)
		if err != nil {
			t.Fatalf("bfc %v: %v\n%s", args, err, out)
		}
		if out != before[i] {
			t.Errorf("bfc %v after deleting the index:\n%s\nwant\n%s", args, out, before[i])
		}
	}
	if _, err := os.Stat(db); err != nil {
		t.Errorf("the index was not rebuilt: %v", err)
	}
}

func TestSearchPrintsMatchingPlansWithASnippet(t *testing.T) {
	dir, ids, shorts := threePlans(t)
	writeRef(t, dir, "plan", map[string][]byte{
		core.PlanDir(ids["Old draft"]) + "/plan.md": []byte("## Approach\n\nRetry the webhook\nthree times.\n"),
	})

	out, err := bfcIn(t, dir, "search", "webhook")
	if err != nil {
		t.Fatalf("bfc search: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), out)
	}
	if f := strings.Fields(lines[0]); len(f) < 3 || f[0] != shorts["Old draft"] || f[1] != "draft" || !strings.HasSuffix(lines[0], "Old draft") {
		t.Errorf("first line = %q, want Short ID, state and title", lines[0])
	}
	if !strings.HasPrefix(lines[1], "    ") || !strings.Contains(lines[1], "Retry the [webhook] three times.") {
		t.Errorf("snippet line = %q, want it indented, on one line, with the match marked", lines[1])
	}
}

func TestSearchWithoutMatchesSaysSo(t *testing.T) {
	dir, _, _ := threePlans(t)
	out, err := bfcIn(t, dir, "search", "nothing", "here")
	if err != nil || strings.TrimSpace(out) != `no plans match "nothing here"` {
		t.Errorf("bfc search = %q, %v", out, err)
	}
	if _, err := bfcIn(t, dir, "search"); err == nil {
		t.Error("bfc search without text succeeded")
	}
}
