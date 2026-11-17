package index

import (
	"context"
	"errors"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// readPlan reads one plan as it is in commit. A missing or broken intent.md
// or event file does not fail the plan; bfc doctor reports those. The last
// activity is left to the caller.
func readPlan(ctx context.Context, s *store.Store, commit string, id ulid.ULID) (Plan, error) {
	dir := core.PlanDir(id)
	p := Plan{ID: id, Title: unreadableTitle, Labels: []string{}}

	files, err := s.Entries(ctx, commit, dir)
	if err != nil {
		return p, err
	}
	blobs := map[string]string{}
	for _, f := range files {
		if !f.Dir {
			blobs[f.Name] = f.ID
		}
	}

	names, err := s.Entries(ctx, commit, dir+"/events")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return p, err
	}
	var events []core.Event
	for _, n := range names { // in ULID order
		eid, err := ulid.ParseStrict(strings.TrimSuffix(n.Name, ".yaml"))
		if err != nil || n.Dir || !strings.HasSuffix(n.Name, ".yaml") {
			continue
		}
		data, err := s.ReadAt(ctx, commit, dir+"/events/"+n.Name)
		if err != nil {
			return p, err
		}
		if e, err := core.ParseEvent(eid, data); err == nil {
			events = append(events, e)
		}
	}

	if _, ok := blobs["intent.md"]; ok {
		if p.Intent, err = s.ReadAt(ctx, commit, dir+"/intent.md"); err != nil {
			return p, err
		}
	}
	if in, err := core.ParseIntent(p.Intent); err == nil {
		p.Title = in.Title
		if in.Labels != nil {
			p.Labels = in.Labels
		}
	}
	if _, ok := blobs["plan.md"]; ok {
		if p.Plan, err = s.ReadAt(ctx, commit, dir+"/plan.md"); err != nil {
			return p, err
		}
	}
	p.Status = core.Derive(id, events, blobs["plan.md"])
	return p, nil
}
