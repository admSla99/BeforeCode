package doctor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

// noRepair is the fix of a problem inside the plans ref: bfc doctor never
// rewrites the ref, and repairing it is not its job.
const noRepair = "bfc doctor does not rewrite the plans ref; "

func checkFormatVersion(ctx context.Context, e *env) Result {
	tip := e.tip(ctx)
	if tip == "" {
		return skip("needs " + store.Ref)
	}
	const update = "update bfc: go install github.com/admSla99/BeforeCode/cmd/bfc@latest"
	data, err := e.s.ReadAt(ctx, tip, "format-version")
	if errors.Is(err, store.ErrNotFound) {
		return fail(noRepair+"ask whoever created it, or start again with bfc init in a new repository", "%s has no format-version file", store.Ref)
	}
	if err != nil {
		return fail(noRepair+"see it with git show "+store.Ref+":format-version", "cannot read format-version: %v", err)
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	switch {
	case err != nil:
		return fail(noRepair+"see it with git show "+store.Ref+":format-version", "format-version %q is not a number", data)
	case v > core.FormatVersion:
		return fail(update, "format-version %d is newer than this bfc supports (%d)", v, core.FormatVersion)
	case v < core.FormatVersion:
		return fail("this bfc cannot migrate it yet; use the bfc release that wrote it",
			"format-version %d is older than this bfc writes (%d) and needs a migration", v, core.FormatVersion)
	}
	return pass("%d", v)
}

func checkIndex(ctx context.Context, e *env) Result {
	if e.s == nil {
		return skip("needs a git repository")
	}
	h, err := index.Check(ctx, e.s)
	switch {
	case err != nil:
		return fail("check the repository with git rev-parse --git-common-dir", "cannot find the index: %v", err)
	case h.Problem != "":
		return fail("bfc doctor --fix", "cannot be read: %s", h.Problem)
	case !h.Built:
		return pass("not built yet; the next read builds it")
	case h.RefCommit != e.tip(ctx):
		return pass("readable, built from another %s commit; the next read updates it", store.Ref)
	}
	return pass("readable and current")
}

func fixIndex(ctx context.Context, e *env) error {
	return index.Rebuild(ctx, e.s)
}

func checkEventFiles(ctx context.Context, e *env) Result {
	sc, r, ok := e.scan(ctx)
	if !ok {
		return r
	}
	if len(sc.invalidFiles) > 0 {
		r := fail(noRepair+"events are never rewritten; see a file with git show "+store.Ref+":<path>",
			"%d of %d not valid", len(sc.invalidFiles), sc.eventFiles)
		r.Items = sc.invalidFiles
		return r
	}
	return pass("%d valid", sc.eventFiles)
}

func checkPlans(ctx context.Context, e *env) Result {
	sc, r, ok := e.scan(ctx)
	if !ok {
		return r
	}
	if len(sc.invalidPlans) > 0 {
		r := fail(noRepair+"create the plan again with bfc new",
			"%d of %d without a valid created event", len(sc.invalidPlans), sc.plans)
		r.Items = sc.invalidPlans
		return r
	}
	return pass("%d valid", sc.plans)
}

func checkSkippedEvents(ctx context.Context, e *env) Result {
	sc, r, ok := e.scan(ctx)
	if !ok {
		return r
	}
	if len(sc.skipped) > 0 {
		return Result{Status: Warn, Items: sc.skipped,
			Detail: fmt.Sprintf("%d not allowed or out of order; they stay in the ref and do not count", len(sc.skipped))}
	}
	return pass("none")
}

func checkShortIDs(ctx context.Context, e *env) Result {
	sc, r, ok := e.scan(ctx)
	if !ok {
		return r
	}
	var dups []string
	for id, plans := range sc.shortIDs {
		if len(plans) > 1 {
			dups = append(dups, id+": "+strings.Join(plans, ", "))
		}
	}
	if len(dups) > 0 {
		sort.Strings(dups)
		return Result{Status: Warn, Items: dups,
			Detail: fmt.Sprintf("%d used by more than one plan; name those plans by a longer prefix", len(dups))}
	}
	return pass("unique")
}

// scan is what bfc doctor learns from reading every plan in the plans ref.
type scan struct {
	eventFiles   int
	invalidFiles []string // "<path>: <why>"
	plans        int
	invalidPlans []string            // plan directories
	skipped      []string            // "<path> (<type> by <email>)"
	shortIDs     map[string][]string // Short ID -> plan ULIDs
}

// scan reads every plan once. Without a plans ref it returns a Skip result
// and ok false.
func (e *env) scan(ctx context.Context) (*scan, Result, bool) {
	tip := e.tip(ctx)
	if tip == "" {
		return nil, skip("needs " + store.Ref), false
	}
	if e.scanned == nil || e.scannedAt != tip {
		sc, err := readPlans(ctx, e.s, tip)
		if err != nil {
			return nil, fail(noRepair+"check the repository with git fsck", "cannot read %s: %v", core.PlansDir, err), false
		}
		e.scanned, e.scannedAt = sc, tip
	}
	return e.scanned, Result{}, true
}

// readPlans validates every event file of every plan in commit and derives
// each plan's state as the index does. A file or plan that cannot be read
// is listed, not fatal.
func readPlans(ctx context.Context, s *store.Store, commit string) (*scan, error) {
	sc := &scan{shortIDs: map[string][]string{}}
	dirs, err := s.Entries(ctx, commit, core.PlansDir)
	if errors.Is(err, store.ErrNotFound) {
		return sc, nil
	}
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		id, err := ulid.ParseStrict(d.Name)
		if err != nil || !d.Dir {
			continue // not a plan; readers ignore unknown entries
		}
		sc.plans++
		readPlan(ctx, s, commit, id, core.PlansDir+"/"+d.Name, sc)
	}
	return sc, nil
}

// readPlan adds what it finds in one plan directory to sc.
func readPlan(ctx context.Context, s *store.Store, commit string, id ulid.ULID, dir string, sc *scan) {
	files, err := s.Entries(ctx, commit, dir)
	if err != nil {
		sc.invalidFiles = append(sc.invalidFiles, dir+": cannot read: "+firstLine(err))
		sc.invalidPlans = append(sc.invalidPlans, dir)
		return
	}
	planBlob := ""
	for _, f := range files {
		if f.Name == "plan.md" && !f.Dir {
			planBlob = f.ID
		}
	}
	events, err := s.Entries(ctx, commit, dir+"/events")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		sc.invalidFiles = append(sc.invalidFiles, dir+"/events: cannot read: "+firstLine(err))
	}
	// Only .yaml files are event files; readers ignore any other entry
	// (ADR 0002 section 1).
	events = slices.DeleteFunc(events, func(f store.Entry) bool { return f.Dir || !strings.HasSuffix(f.Name, ".yaml") })
	var ids []string
	for _, f := range events {
		ids = append(ids, f.ID)
	}
	content := readBlobs(ctx, s, ids)

	var parsed []core.Event
	paths := map[ulid.ULID]string{}
	for _, f := range events { // in tree order, as the index reads them
		path := dir + "/events/" + f.Name
		sc.eventFiles++
		data, ok := content[f.ID]
		if !ok {
			sc.invalidFiles = append(sc.invalidFiles, path+": cannot read object "+f.ID)
			continue
		}
		if err := core.ValidateEvent(id, f.Name, data); err != nil {
			sc.invalidFiles = append(sc.invalidFiles, path+": "+err.Error())
		}
		// State is derived from every event that parses, as the index
		// does, so that doctor and bfc list agree.
		eid, err := ulid.ParseStrict(strings.TrimSuffix(f.Name, ".yaml"))
		if err != nil {
			continue
		}
		if ev, err := core.ParseEvent(eid, data); err == nil {
			parsed = append(parsed, ev)
			paths[eid] = path
		}
	}

	status := core.Derive(id, parsed, planBlob)
	if status.State == core.StateInvalid {
		sc.invalidPlans = append(sc.invalidPlans, dir)
		return
	}
	if status.ShortID != "" { // a missing short_id is reported with the event files
		sc.shortIDs[status.ShortID] = append(sc.shortIDs[status.ShortID], id.String())
	}
	byID := map[ulid.ULID]core.Event{}
	for _, ev := range parsed {
		byID[ev.ID] = ev
	}
	for _, eid := range core.SkippedEvents(id, parsed, planBlob) {
		ev := byID[eid]
		sc.skipped = append(sc.skipped, fmt.Sprintf("%s (%s by %s)", paths[eid], ev.Type, ev.Author.Email))
	}
}

// readBlobs reads the blobs with one git call, or one by one when that
// fails, so that a missing object loses only itself.
func readBlobs(ctx context.Context, s *store.Store, ids []string) map[string][]byte {
	if all, err := s.Blobs(ctx, ids); err == nil {
		return all
	}
	out := map[string][]byte{}
	for _, id := range ids {
		if one, err := s.Blobs(ctx, []string{id}); err == nil {
			out[id] = one[id]
		}
	}
	return out
}
