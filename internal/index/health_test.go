package index_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

func check(t *testing.T, s *store.Store) index.Health {
	t.Helper()
	h, err := index.Check(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func tip(t *testing.T, s *store.Store) string {
	t.Helper()
	commit, err := s.Tip(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

func TestCheckReportsTheIndexWithoutChangingIt(t *testing.T) {
	s := newStore(t)
	newPlan(t, s, "First", "")
	if h := check(t, s); h.Built || h.Problem != "" {
		t.Errorf("Check before any read = %+v, want not built", h)
	}

	ix := openIndex(t, s)
	plans(t, ix)
	path := ix.Path()
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	if h := check(t, s); !h.Built || h.Problem != "" || h.RefCommit != tip(t, s) {
		t.Errorf("Check after a read = %+v, want built from %s", h, tip(t, s))
	}

	newPlan(t, s, "Second", "")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if h := check(t, s); h.RefCommit == tip(t, s) {
		t.Errorf("Check = %+v, want the older ref commit", h)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("Check changed the database")
	}
}

func TestCheckFindsAnUnreadableIndexAndRebuildRepairsIt(t *testing.T) {
	s := newStore(t)
	newPlan(t, s, "First", "")
	damage(t, s, func(path string) {
		if err := os.WriteFile(path, []byte("not a database"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if h := check(t, s); h.Problem == "" {
		t.Errorf("Check of a damaged index = %+v, want a problem", h)
	}

	if err := index.Rebuild(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if h := check(t, s); !h.Built || h.Problem != "" || h.RefCommit != tip(t, s) {
		t.Errorf("Check after Rebuild = %+v", h)
	}
}

func TestCheckOfAnIndexOfAnotherSchemaVersion(t *testing.T) {
	s := newStore(t)
	damage(t, s, func(path string) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := index.WriteOldSchema(path); err != nil {
			t.Fatal(err)
		}
	})
	if h := check(t, s); h.Problem != "written by another version of bfc" {
		t.Errorf("Check = %+v", h)
	}
}
