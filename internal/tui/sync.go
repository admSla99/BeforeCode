package tui

import (
	"context"
	"fmt"
	"time"
)

// Syncer syncs the Plans ref with the remote in the background. cmd/bfc
// implements it on the sync of internal/store; tests use a fake.
type Syncer interface {
	// Sync pulls, then pushes. It never asks the user anything.
	Sync(ctx context.Context) SyncResult
}

// SyncOutcome is how one sync ended.
type SyncOutcome int

const (
	// SyncOK: the remote and this clone hold the same plans.
	SyncOK SyncOutcome = iota
	// SyncOffline: the remote could not be reached or refused the login.
	SyncOffline
	// SyncConflict: both sides changed the same lines; nothing was
	// committed.
	SyncConflict
	// SyncLocalOnly: local mode; nothing is synced.
	SyncLocalOnly
)

// SyncResult is what one sync reports.
type SyncResult struct {
	Outcome SyncOutcome
	// Changed is true when the sync brought changes from the remote.
	Changed bool
	// Message is git's message for SyncOffline, or the conflicting file
	// names for SyncConflict.
	Message string
}

// syncStatus is what the status bar says about sync (issue #17).
type syncStatus int

const (
	statusSynced syncStatus = iota
	statusSyncing
	statusOffline
	statusConflict
	statusLocal
)

// syncState is the state model of the background sync, as prototyped for
// issue #17: one sync at a time, a change during a sync queues one more,
// offline and conflict keep local work going and the timer retries. Its
// methods return the new state and whether a sync must start now.
type syncState struct {
	status        syncStatus
	running       bool      // a sync is in flight
	pendingResync bool      // a local change arrived during the running sync
	unpushed      int       // local changes not yet on the remote
	lastOK        time.Time // end of the last good sync
	message       string    // git's message, or the conflicting files
}

func newSyncState(local bool) syncState {
	if local {
		return syncState{status: statusLocal}
	}
	return syncState{status: statusSynced}
}

// start starts a sync. A retry from offline or conflict keeps that status
// on the screen until it succeeds.
func (s syncState) start() (syncState, bool) {
	s.running, s.pendingResync = true, false
	if s.status == statusSynced {
		s.status = statusSyncing
	}
	return s, true
}

// tick is the timer: it starts a sync unless one is running.
func (s syncState) tick() (syncState, bool) {
	if s.status == statusLocal || s.running {
		return s, false
	}
	return s.start()
}

// localChange is a write to the Plans ref in this clone.
func (s syncState) localChange() (syncState, bool) {
	if s.status == statusLocal {
		return s, false
	}
	s.unpushed++
	if s.running {
		s.pendingResync = true
		return s, false
	}
	return s.start()
}

// done applies the result of the running sync, which ended at now.
func (s syncState) done(r SyncResult, now time.Time) (syncState, bool) {
	if !s.running {
		return s, false
	}
	s.running = false
	switch r.Outcome {
	case SyncOK:
		s.status, s.unpushed, s.lastOK, s.message = statusSynced, 0, now, ""
		if s.pendingResync {
			return s.start()
		}
	case SyncOffline:
		s.status, s.pendingResync, s.message = statusOffline, false, r.Message
	case SyncConflict:
		s.status, s.pendingResync, s.message = statusConflict, false, r.Message
	case SyncLocalOnly:
		s = newSyncState(true)
	}
	return s, false
}

// hint explains an offline or conflict status in one line, or is empty.
func (s syncState) hint() string {
	switch s.status {
	case statusOffline:
		return "Offline: " + s.message
	case statusConflict:
		return "Conflict: nothing was committed. Run bfc pull --keep local or --keep remote."
	}
	return ""
}

// label is the text of the status bar at time now.
func (s syncState) label(now time.Time) string {
	switch s.status {
	case statusLocal:
		return "local mode, not synced"
	case statusSyncing:
		if s.unpushed > 0 {
			return fmt.Sprintf("⟳ syncing · %d to push", s.unpushed)
		}
		return "⟳ syncing"
	case statusOffline:
		if s.unpushed == 0 {
			return "○ offline"
		}
		plural := "s"
		if s.unpushed == 1 {
			plural = ""
		}
		return fmt.Sprintf("○ offline · %d change%s waiting", s.unpushed, plural)
	case statusConflict:
		return "✗ conflict in " + s.message
	}
	switch ago := now.Sub(s.lastOK); {
	case s.lastOK.IsZero() || ago < 5*time.Second:
		return "● synced just now"
	case ago < time.Minute:
		return fmt.Sprintf("● synced %ds ago", int(ago/time.Second))
	default:
		return fmt.Sprintf("● synced %dm ago", int(ago/time.Minute))
	}
}
