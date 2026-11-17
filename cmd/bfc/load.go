package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// unreadableTitle stands for the title of a plan whose intent.md cannot be
// read.
const unreadableTitle = "(unreadable intent.md)"

// planView is everything list and show print about a plan.
type planView struct {
	ID ulid.ULID
	// ShortID is the stored Short ID, or for an invalid plan one computed
	// from its ULID, so that every listed plan can be shown.
	ShortID      string
	Status       core.Status
	Title        string
	Labels       []string
	LastActivity time.Time
	Intent       []byte // nil when missing
	Plan         []byte // nil when missing
}

// loadPlans reads every plan in the plans ref, newest activity first.
func loadPlans(ctx context.Context, s *store.Store) ([]planView, error) {
	ids, err := listPlanIDs(ctx, s)
	if err != nil {
		return nil, err
	}
	changed, err := s.LastChanged(ctx, core.PlansDir)
	if err != nil {
		return nil, err
	}
	views := make([]planView, 0, len(ids))
	for _, id := range ids {
		v, err := loadPlan(ctx, s, id)
		if err != nil {
			return nil, err
		}
		v.LastActivity = changed[id.String()]
		v.ShortID = v.Status.ShortID
		if v.ShortID == "" {
			v.ShortID = core.ShortID(id, ids)
		}
		views = append(views, v)
	}
	sort.SliceStable(views, func(i, j int) bool {
		if !views[i].LastActivity.Equal(views[j].LastActivity) {
			return views[i].LastActivity.After(views[j].LastActivity)
		}
		return views[i].ID.Compare(views[j].ID) > 0
	})
	return views, nil
}

// loadPlan reads one plan. A missing or broken intent.md or event file does
// not fail the plan; bfc doctor reports those.
func loadPlan(ctx context.Context, s *store.Store, id ulid.ULID) (planView, error) {
	dir := core.PlanDir(id)
	v := planView{ID: id, Title: unreadableTitle, Labels: []string{}}

	names, err := s.List(ctx, dir+"/events")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return v, err
	}
	var events []core.Event
	for _, n := range names { // List returns them in ULID order
		eid, err := ulid.ParseStrict(strings.TrimSuffix(n, ".yaml"))
		if err != nil || !strings.HasSuffix(n, ".yaml") {
			continue
		}
		data, err := s.Read(ctx, dir+"/events/"+n)
		if err != nil {
			return v, err
		}
		if e, err := core.ParseEvent(eid, data); err == nil {
			events = append(events, e)
		}
	}

	if v.Intent, err = readOptional(ctx, s, dir+"/intent.md"); err != nil {
		return v, err
	}
	if in, err := core.ParseIntent(v.Intent); err == nil {
		v.Title = in.Title
		if in.Labels != nil {
			v.Labels = in.Labels
		}
	}
	if v.Plan, err = readOptional(ctx, s, dir+"/plan.md"); err != nil {
		return v, err
	}
	blob := ""
	if v.Plan != nil {
		if blob, err = s.ObjectID(ctx, dir+"/plan.md"); err != nil {
			return v, err
		}
	}
	v.Status = core.Derive(id, events, blob)
	return v, nil
}

// readOptional reads a file of the plans ref, or returns nil if it is not
// there.
func readOptional(ctx context.Context, s *store.Store, path string) ([]byte, error) {
	data, err := s.Read(ctx, path)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return data, err
}

// listPlanIDs returns the ULIDs of the plan directories in the plans ref.
func listPlanIDs(ctx context.Context, s *store.Store) ([]ulid.ULID, error) {
	names, err := s.List(ctx, core.PlansDir)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []ulid.ULID
	for _, n := range names {
		if id, err := ulid.ParseStrict(strings.TrimSuffix(n, "/")); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
