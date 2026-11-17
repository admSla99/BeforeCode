package index

import (
	"context"
	"errors"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// readPlan reads one plan as it is in commit, from its directory dir under
// core.PlansDir (normally id.String()). A missing or broken intent.md or
// event file does not fail the plan; bfc doctor reports those. The last
// activity is left to the caller.
func readPlan(ctx context.Context, s *store.Store, commit string, id ulid.ULID, dir string) (Plan, error) {
	dir = core.PlansDir + "/" + dir
	p := Plan{ID: id, Title: unreadableTitle, Labels: []string{}}

	files, err := s.Entries(ctx, commit, dir)
	if err != nil {
		return p, err
	}
	blobs := map[string]string{} // file name -> object ID
	for _, f := range files {
		if !f.Dir {
			blobs[f.Name] = f.ID
		}
	}
	events, err := s.Entries(ctx, commit, dir+"/events")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return p, err
	}
	var eventIDs []ulid.ULID
	var want []string
	for _, e := range events { // in ULID order
		eid, err := ulid.ParseStrict(strings.TrimSuffix(e.Name, ".yaml"))
		if err != nil || e.Dir || !strings.HasSuffix(e.Name, ".yaml") {
			continue
		}
		eventIDs = append(eventIDs, eid)
		want = append(want, e.ID)
	}
	for _, name := range []string{"intent.md", "plan.md"} {
		if oid, ok := blobs[name]; ok {
			want = append(want, oid)
		}
	}
	content, err := s.Blobs(ctx, want)
	if err != nil {
		return p, err
	}

	var parsed []core.Event
	for i, eid := range eventIDs {
		if e, err := core.ParseEvent(eid, content[want[i]]); err == nil {
			parsed = append(parsed, e)
		}
	}
	if oid, ok := blobs["intent.md"]; ok {
		p.Intent = content[oid]
	}
	if in, err := core.ParseIntent(p.Intent); err == nil {
		p.Title = in.Title
		if in.Labels != nil {
			p.Labels = in.Labels
		}
	}
	if oid, ok := blobs["plan.md"]; ok {
		p.Plan = content[oid]
	}
	p.Status = core.Derive(id, parsed, blobs["plan.md"])
	return p, nil
}
