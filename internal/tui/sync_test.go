package tui

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

// step applies one event and checks whether a sync must start.
func step(t *testing.T, s syncState, ev func(syncState) (syncState, bool), wantStart bool) syncState {
	t.Helper()
	s, start := ev(s)
	if start != wantStart {
		t.Fatalf("start = %v, want %v (state %+v)", start, wantStart, s)
	}
	return s
}

func tick(s syncState) (syncState, bool)   { return s.tick() }
func change(s syncState) (syncState, bool) { return s.localChange() }
func done(r SyncResult) func(syncState) (syncState, bool) {
	return func(s syncState) (syncState, bool) { return s.done(r, t0) }
}

var (
	ok       = SyncResult{Outcome: SyncOK}
	brought  = SyncResult{Outcome: SyncOK, Changed: true}
	offline  = SyncResult{Outcome: SyncOffline, Message: "Could not resolve host: github.com"}
	conflict = SyncResult{Outcome: SyncConflict, Message: "plan.md"}
)

func TestSyncHappyPath(t *testing.T) {
	s := newSyncState(false)
	s = step(t, s, tick, true)
	if s.status != statusSyncing {
		t.Errorf("status = %v", s.status)
	}
	s = step(t, s, done(ok), false)
	if s.status != statusSynced || s.unpushed != 0 || !s.lastOK.Equal(t0) {
		t.Errorf("after ok: %+v", s)
	}
	s = step(t, s, change, true)
	if s.unpushed != 1 {
		t.Errorf("waiting = %d", s.unpushed)
	}
	s = step(t, s, done(brought), false)
	if s.unpushed != 0 {
		t.Errorf("waiting after ok = %d", s.unpushed)
	}
}

// Only one sync at a time; a change during a sync queues exactly one more.
func TestSyncChangeDuringASyncRunsOneMore(t *testing.T) {
	s := step(t, newSyncState(false), tick, true)
	s = step(t, s, change, false)
	s = step(t, s, change, false)
	s = step(t, s, tick, false)
	s = step(t, s, done(ok), true)
	if s.status != statusSyncing {
		t.Errorf("status = %v, want syncing again", s.status)
	}
	s = step(t, s, done(ok), false)
	if s.status != statusSynced {
		t.Errorf("status = %v", s.status)
	}
}

func TestSyncOfflineKeepsCountingAndRetries(t *testing.T) {
	s := step(t, newSyncState(false), change, true)
	s = step(t, s, done(offline), false)
	s = step(t, s, change, true)
	s = step(t, s, done(offline), false)
	if s.status != statusOffline || s.unpushed != 2 || s.message != offline.Message {
		t.Errorf("offline: %+v", s)
	}
	if got := s.label(t0); got != "○ offline · 2 changes waiting" {
		t.Errorf("label = %q", got)
	}
	if got := s.hint(); got != "Offline: Could not resolve host: github.com" {
		t.Errorf("hint = %q", got)
	}
	s = step(t, s, tick, true)
	s = step(t, s, done(ok), false)
	if s.status != statusSynced || s.unpushed != 0 || s.message != "" || s.hint() != "" {
		t.Errorf("back online: %+v", s)
	}
}

func TestSyncConflictStaysUntilASyncSucceeds(t *testing.T) {
	s := step(t, newSyncState(false), change, true)
	s = step(t, s, done(conflict), false)
	if got := s.label(t0); got != "✗ conflict in plan.md" {
		t.Errorf("label = %q", got)
	}
	if got := s.hint(); got != "Conflict: nothing was committed. Run bfc pull --keep local or --keep remote." {
		t.Errorf("hint = %q", got)
	}
	s = step(t, s, tick, true)
	s = step(t, s, done(conflict), false)
	if s.status != statusConflict {
		t.Errorf("status = %v", s.status)
	}
	s = step(t, s, tick, true)
	s = step(t, s, done(ok), false)
	if s.status != statusSynced {
		t.Errorf("status = %v", s.status)
	}
}

func TestSyncLocalModeNeverSyncs(t *testing.T) {
	s := newSyncState(true)
	s = step(t, s, tick, false)
	s = step(t, s, change, false)
	if s.status != statusLocal || s.label(t0) != "local mode, not synced" {
		t.Errorf("local mode: %+v %q", s, s.label(t0))
	}
}

// A result that arrives when no sync runs is ignored.
func TestSyncIgnoresAStrayResult(t *testing.T) {
	s := step(t, newSyncState(false), done(conflict), false)
	if s.status == statusConflict {
		t.Errorf("stray result applied: %+v", s)
	}
}

func TestSyncLabels(t *testing.T) {
	s := step(t, newSyncState(false), tick, true)
	if got := s.label(t0); got != "⟳ syncing" {
		t.Errorf("syncing label = %q", got)
	}
	s = step(t, s, done(ok), false)
	for after, want := range map[time.Duration]string{
		0:                "● synced just now",
		3 * time.Second:  "● synced just now",
		42 * time.Second: "● synced 42s ago",
		3 * time.Minute:  "● synced 3m ago",
	} {
		if got := s.label(t0.Add(after)); got != want {
			t.Errorf("after %v: label = %q, want %q", after, got, want)
		}
	}
	s = step(t, s, change, true)
	if got := s.label(t0); got != "⟳ syncing · 1 to push" {
		t.Errorf("label = %q", got)
	}
}

// A retry from offline or conflict keeps that status and its hint on the
// screen until the retry succeeds.
func TestSyncRetryKeepsOfflineAndConflictVisible(t *testing.T) {
	for _, r := range []SyncResult{offline, conflict} {
		s := step(t, newSyncState(false), change, true)
		s = step(t, s, done(r), false)
		label, hint := s.label(t0), s.hint()
		s = step(t, s, tick, true)
		if s.label(t0) != label || s.hint() != hint {
			t.Errorf("during the retry: %q / %q, want %q / %q", s.label(t0), s.hint(), label, hint)
		}
		s = step(t, s, done(ok), false)
		if s.status != statusSynced {
			t.Errorf("after the retry: %v", s.status)
		}
	}
}

func TestSyncOfflineWithNothingToPushSaysOnlyOffline(t *testing.T) {
	s := step(t, newSyncState(false), tick, true)
	s = step(t, s, done(offline), false)
	if got := s.label(t0); got != "○ offline" {
		t.Errorf("label = %q", got)
	}
}
