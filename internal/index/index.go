package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// schemaVersion is stored as the database's user_version. A database of any
// other version is discarded and rebuilt. Bump it whenever the schema or the
// meaning of a column changes, and whenever reading a plan gives another
// result for the same files (core.Derive, core.ParseIntent, searchText):
// plans whose directory did not change are not read again.
const schemaVersion = 1

// File is where the index lives, relative to the git common directory.
const File = "beforecode/index.db"

// unreadableTitle stands for the title of a plan whose intent.md cannot be
// read.
const unreadableTitle = "(unreadable intent.md)"

// ErrEmptyQuery is returned by Search for text without a word to look for.
var ErrEmptyQuery = errors.New("index: nothing to search for")

// Plan is everything the index knows about one plan.
type Plan struct {
	ID ulid.ULID
	// ShortID is the stored Short ID, or for an invalid plan one computed
	// from its ULID, so that every listed plan can be shown.
	ShortID      string
	Status       core.Status
	Title        string
	Labels       []string
	LastActivity time.Time // zero when unknown
	Intent       []byte    // nil when missing
	Plan         []byte    // nil when missing
}

// Hit is a plan that matches a search, with a snippet of the matching text
// in which the matches are marked [like this].
type Hit struct {
	Plan    Plan
	Snippet string
}

// Updated says what an Update did: the plans ref commit the index was built
// from before (empty for a new index) and after it, and the plans it read
// again because their directory changed.
type Updated struct {
	From, To string
	Reread   []ulid.ULID
}

// Index is the SQLite cache of the plans ref of one repository. Every read
// first brings it up to the current tip of the ref, so it never answers from
// an older state than the ref.
type Index struct {
	s    *store.Store
	path string
	db   *sql.DB
}

// errOutdated marks a database written by another schema version, or one
// without the tables of this version.
var errOutdated = errors.New("index: database has another schema version")

// Open opens the index of the store's repository, creating it under the git
// common directory if needed. A database that is corrupt or of another schema
// version is discarded and rebuilt.
func Open(ctx context.Context, s *store.Store) (*Index, error) {
	common, err := s.CommonDir(ctx)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(common, filepath.FromSlash(File))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	ix := &Index{s: s, path: path}
	err = ix.connect(ctx)
	if discardable(err) {
		err = ix.reset(ctx)
	}
	if err != nil {
		_ = ix.Close()
		return nil, err
	}
	return ix, nil
}

// Path returns the path of the database file.
func (ix *Index) Path() string { return ix.path }

// Close closes the database.
func (ix *Index) Close() error {
	if ix.db == nil {
		return nil
	}
	err := ix.db.Close()
	ix.db = nil
	return err
}

// Update brings the index up to the current tip of the plans ref. It reads
// again only the plans whose directory tree changed since the commit the
// index was built from. When that commit is no longer in the ref's history,
// last activity is computed again from the whole history.
func (ix *Index) Update(ctx context.Context) (Updated, error) {
	var u Updated
	err := ix.retry(ctx, func() (err error) {
		u, err = ix.update(ctx)
		return err
	})
	return u, err
}

// Plans returns every plan, newest activity first.
func (ix *Index) Plans(ctx context.Context) ([]Plan, error) {
	var plans []Plan
	err := ix.retry(ctx, func() error {
		if _, err := ix.update(ctx); err != nil {
			return err
		}
		rows, err := ix.db.QueryContext(ctx, `SELECT `+planColumns+` FROM plans p ORDER BY p.last_activity DESC, p.id DESC`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		plans = nil
		for rows.Next() {
			p, err := scanPlan(rows)
			if err != nil {
				return err
			}
			plans = append(plans, p)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return ix.fillShortIDs(ctx, plans)
	})
	return plans, err
}

// ErrUnknownPlan is returned by Plan for an ID that is not in the plans ref.
var ErrUnknownPlan = errors.New("index: no such plan in the plans ref")

// Plan returns one plan, or ErrUnknownPlan.
func (ix *Index) Plan(ctx context.Context, id ulid.ULID) (Plan, error) {
	var p Plan
	err := ix.retry(ctx, func() error {
		if _, err := ix.update(ctx); err != nil {
			return err
		}
		rows, err := ix.db.QueryContext(ctx, `SELECT `+planColumns+` FROM plans p WHERE p.id = ?`, id.String())
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return fmt.Errorf("%w: %s", ErrUnknownPlan, id)
		}
		if p, err = scanPlan(rows); err != nil {
			return err
		}
		ps := []Plan{p}
		if err := ix.fillShortIDs(ctx, ps); err != nil {
			return err
		}
		p = ps[0]
		return nil
	})
	return p, err
}

// Search returns the plans whose title, intent.md or plan.md contain every
// word of text, best match first. Front matter and HTML comments are not
// searched. Case and diacritics do not matter; the words are
// taken literally, not as a query language.
func (ix *Index) Search(ctx context.Context, text string) ([]Hit, error) {
	match := matchQuery(text)
	if match == "" {
		return nil, ErrEmptyQuery
	}
	var hits []Hit
	err := ix.retry(ctx, func() error {
		if _, err := ix.update(ctx); err != nil {
			return err
		}
		rows, err := ix.db.QueryContext(ctx, `
			SELECT `+planColumns+`, snippet(plan_text, -1, '[', ']', '…', 12)
			FROM plan_text JOIN plans p ON p.rowid = plan_text.rowid
			WHERE plan_text MATCH ?
			ORDER BY bm25(plan_text), p.last_activity DESC, p.id DESC`, match)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		hits = nil
		var plans []Plan
		for rows.Next() {
			var snippet string
			p, err := scanPlan(rows, &snippet)
			if err != nil {
				return err
			}
			plans = append(plans, p)
			hits = append(hits, Hit{Snippet: strings.Join(strings.Fields(snippet), " ")})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := ix.fillShortIDs(ctx, plans); err != nil {
			return err
		}
		for i := range hits {
			hits[i].Plan = plans[i]
		}
		return nil
	})
	return hits, err
}

// matchQuery turns text into an FTS5 query that matches every word as a
// literal phrase, so that quotes, AND, NOT or * in the text are just text.
func matchQuery(text string) string {
	var terms []string
	for _, w := range strings.Fields(text) {
		terms = append(terms, `"`+strings.ReplaceAll(w, `"`, `""`)+`"`)
	}
	return strings.Join(terms, " ")
}

// schema creates version schemaVersion. plan_text holds the searchable text
// of the plan with the same rowid in plans: its title, the body of
// intent.md and plan.md, without HTML comments (searchText).
const schema = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE plans (
	id                 TEXT PRIMARY KEY,
	tree               TEXT NOT NULL,    -- git tree of the plan directory
	short_id           TEXT NOT NULL,    -- from the created event; '' for an invalid plan
	state              TEXT NOT NULL,
	title              TEXT NOT NULL,
	labels             TEXT NOT NULL,    -- JSON array
	author_name        TEXT NOT NULL,
	author_email       TEXT NOT NULL,
	required_approvals INTEGER NOT NULL,
	approvers          TEXT NOT NULL,    -- JSON array
	last_activity      INTEGER NOT NULL, -- Unix seconds; 0 when unknown
	intent             BLOB,             -- NULL when missing
	plan               BLOB              -- NULL when missing
);
CREATE VIRTUAL TABLE plan_text USING fts5(title, intent, plan, tokenize = 'unicode61 remove_diacritics 2');
`

const planColumns = `p.id, p.short_id, p.state, p.title, p.labels, p.author_name, p.author_email,
	p.required_approvals, p.approvers, p.last_activity, p.intent, p.plan`

// connect opens the database file and checks or creates its schema.
func (ix *Index) connect(ctx context.Context) error {
	// _txlock=immediate takes the write lock at BEGIN, so two bfc processes
	// updating at once wait for each other instead of failing midway.
	db, err := sql.Open("sqlite", ix.path+"?_pragma=busy_timeout(10000)&_txlock=immediate")
	if err != nil {
		return err
	}
	ix.db = db
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var version, tables int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema`).Scan(&tables); err != nil {
		return err
	}
	switch {
	case version == schemaVersion:
		// A damaged file can carry the right version without its tables.
		var ours int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema
			WHERE type = 'table' AND name IN ('meta', 'plans', 'plan_text')`).Scan(&ours); err != nil {
			return err
		}
		if ours != 3 {
			return errOutdated
		}
		return nil
	case version != 0 || tables != 0:
		return errOutdated
	}
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// reset deletes the database file and creates a new one.
func (ix *Index) reset(ctx context.Context) error {
	_ = ix.Close()
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		if err := os.Remove(ix.path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return ix.connect(ctx)
}

// retry runs fn, and once more on a new database if fn found the old one
// corrupt.
func (ix *Index) retry(ctx context.Context, fn func() error) error {
	err := fn()
	if !discardable(err) {
		return err
	}
	if err := ix.reset(ctx); err != nil {
		return err
	}
	return fn()
}

// discardable reports whether err means the database file is of no use and
// must be rebuilt.
func discardable(err error) bool {
	var e *sqlite.Error
	if errors.As(err, &e) {
		code := e.Code() & 0xff
		return code == sqlite3.SQLITE_CORRUPT || code == sqlite3.SQLITE_NOTADB
	}
	return errors.Is(err, errOutdated)
}

func (ix *Index) update(ctx context.Context) (Updated, error) {
	// Without a move of the ref, readers take no write lock.
	if u, err := ix.current(ctx); err != nil || u.From == u.To {
		return u, err
	}
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return Updated{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// The tip is read under the write lock, so a process that waited for
	// another one never sets the index back to an older tip.
	tip, err := ix.s.Tip(ctx)
	if err != nil && !errors.Is(err, store.ErrNotInitialised) {
		return Updated{}, err
	}

	var u Updated
	err = tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'ref_commit'`).Scan(&u.From)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Updated{}, err
	}
	u.To = tip
	if u.From == u.To {
		return u, nil
	}
	since := u.From
	if since != "" && tip != "" {
		if ok, err := ix.s.IsAncestor(ctx, since, tip); err != nil {
			return Updated{}, err
		} else if !ok {
			since = ""
		}
	}
	if since == "" {
		// Not a step forward in the same history: plans are still compared
		// by tree, but last activity is computed from the whole history.
		if _, err := tx.ExecContext(ctx, `UPDATE plans SET last_activity = 0`); err != nil {
			return Updated{}, err
		}
	}

	stored := map[string]string{} // plan ID -> tree
	rows, err := tx.QueryContext(ctx, `SELECT id, tree FROM plans`)
	if err != nil {
		return Updated{}, err
	}
	for rows.Next() {
		var id, tree string
		if err := rows.Scan(&id, &tree); err != nil {
			_ = rows.Close()
			return Updated{}, err
		}
		stored[id] = tree
	}
	if err := rows.Close(); err != nil {
		return Updated{}, err
	}

	var entries []store.Entry
	if tip != "" {
		entries, err = ix.s.Entries(ctx, tip, core.PlansDir)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return Updated{}, err
		}
	}
	for _, e := range entries {
		id, err := ulid.ParseStrict(e.Name)
		if err != nil || !e.Dir {
			continue
		}
		tree, known := stored[e.Name]
		delete(stored, e.Name)
		if known && tree == e.ID {
			continue
		}
		p, err := readPlan(ctx, ix.s, tip, id)
		if err != nil {
			return Updated{}, err
		}
		if err := upsert(ctx, tx, p, e.ID); err != nil {
			return Updated{}, err
		}
		u.Reread = append(u.Reread, id)
	}
	for id := range stored { // plans no longer in the ref
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM plan_text WHERE rowid = (SELECT rowid FROM plans WHERE id = ?1);
			DELETE FROM plans WHERE id = ?1;`, id); err != nil {
			return Updated{}, err
		}
	}

	if tip != "" {
		changed, err := ix.s.LastChanged(ctx, since, tip, core.PlansDir)
		if err != nil {
			return Updated{}, err
		}
		for id, at := range changed {
			if _, err := tx.ExecContext(ctx, `UPDATE plans SET last_activity = max(last_activity, ?) WHERE id = ?`, at.Unix(), id); err != nil {
				return Updated{}, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('ref_commit', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, tip); err != nil {
		return Updated{}, err
	}
	return u, tx.Commit()
}

// current reads the tip of the plans ref and the commit the index was
// built from, without a transaction.
func (ix *Index) current(ctx context.Context) (Updated, error) {
	tip, err := ix.s.Tip(ctx)
	if err != nil && !errors.Is(err, store.ErrNotInitialised) {
		return Updated{}, err
	}
	u := Updated{To: tip}
	err = ix.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'ref_commit'`).Scan(&u.From)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return u, err
}

// upsert stores a plan read from tree, keeping its last activity, and
// replaces its searchable text.
func upsert(ctx context.Context, tx *sql.Tx, p Plan, tree string) error {
	labels, err := json.Marshal(p.Labels)
	if err != nil {
		return err
	}
	approvers := p.Status.Approvers
	if approvers == nil {
		approvers = []string{}
	}
	appr, err := json.Marshal(approvers)
	if err != nil {
		return err
	}
	var rowid int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO plans (id, tree, short_id, state, title, labels, author_name, author_email,
			required_approvals, approvers, last_activity, intent, plan)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT (id) DO UPDATE SET tree = excluded.tree, short_id = excluded.short_id,
			state = excluded.state, title = excluded.title, labels = excluded.labels,
			author_name = excluded.author_name, author_email = excluded.author_email,
			required_approvals = excluded.required_approvals, approvers = excluded.approvers,
			intent = excluded.intent, plan = excluded.plan
		RETURNING rowid`,
		p.ID.String(), tree, p.Status.ShortID, string(p.Status.State), p.Title, string(labels),
		p.Status.Author.Name, p.Status.Author.Email, p.Status.RequiredApprovals, string(appr),
		nullable(p.Intent), nullable(p.Plan)).Scan(&rowid)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		DELETE FROM plan_text WHERE rowid = ?1;
		INSERT INTO plan_text (rowid, title, intent, plan) VALUES (?1, ?2, ?3, ?4);`,
		rowid, p.Title, searchText(core.IntentBody(p.Intent)), searchText(p.Plan))
	return err
}

// htmlComment matches the hints the templates leave in <!-- -->.
var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// searchText is the part of a document worth searching: without HTML
// comments, which hold template hints that every plan shares.
func searchText(doc []byte) string {
	return htmlComment.ReplaceAllString(string(doc), "")
}

// nullable stores a missing file as NULL and an empty one as an empty blob.
func nullable(b []byte) any {
	if b == nil {
		return nil
	}
	return append([]byte{}, b...)
}

func scanPlan(rows *sql.Rows, extra ...any) (Plan, error) {
	var p Plan
	var id, state, labels, approvers string
	var lastActivity int64
	var intent, plan sql.RawBytes
	dest := append([]any{&id, &p.Status.ShortID, &state, &p.Title, &labels, &p.Status.Author.Name,
		&p.Status.Author.Email, &p.Status.RequiredApprovals, &approvers, &lastActivity, &intent, &plan}, extra...)
	if err := rows.Scan(dest...); err != nil {
		return p, err
	}
	var err error
	if p.ID, err = ulid.ParseStrict(id); err != nil {
		return p, err
	}
	p.Status.State = core.State(state)
	p.ShortID = p.Status.ShortID
	if err := json.Unmarshal([]byte(labels), &p.Labels); err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(approvers), &p.Status.Approvers); err != nil {
		return p, err
	}
	if len(p.Status.Approvers) == 0 {
		p.Status.Approvers = nil // as core.Derive leaves it
	}
	if lastActivity != 0 {
		p.LastActivity = time.Unix(lastActivity, 0).UTC()
	}
	if intent != nil {
		p.Intent = append([]byte{}, intent...)
	}
	if plan != nil {
		p.Plan = append([]byte{}, plan...)
	}
	return p, nil
}

// fillShortIDs gives each invalid plan a Short ID computed from its ULID
// among all plans.
func (ix *Index) fillShortIDs(ctx context.Context, plans []Plan) error {
	var all []ulid.ULID
	for i := range plans {
		if plans[i].ShortID != "" {
			continue
		}
		if all == nil {
			rows, err := ix.db.QueryContext(ctx, `SELECT id FROM plans`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					_ = rows.Close()
					return err
				}
				id, err := ulid.ParseStrict(s)
				if err != nil {
					_ = rows.Close()
					return err
				}
				all = append(all, id)
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
		plans[i].ShortID = core.ShortID(plans[i].ID, all)
	}
	return nil
}
