package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

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
const schemaVersion = 5

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

// Plans returns every plan, newest activity first, without its documents.
func (ix *Index) Plans(ctx context.Context) ([]Plan, error) {
	return ix.plans(ctx, false)
}

// PlansWithDocuments is Plans with intent.md and plan.md, for readers of
// every plan's Tasks.
func (ix *Index) PlansWithDocuments(ctx context.Context) ([]Plan, error) {
	return ix.plans(ctx, true)
}

func (ix *Index) plans(ctx context.Context, docs bool) ([]Plan, error) {
	columns := headerColumns
	if docs {
		columns = docColumns
	}
	var plans []Plan
	err := ix.retry(ctx, func() error {
		if _, err := ix.update(ctx); err != nil {
			return err
		}
		rows, err := ix.db.QueryContext(ctx, `SELECT `+columns+` FROM plans p ORDER BY p.last_activity DESC, p.id DESC`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		plans = nil
		for rows.Next() {
			p, err := scanPlan(rows, docs)
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
		rows, err := ix.db.QueryContext(ctx, `SELECT `+docColumns+` FROM plans p WHERE p.id = ?`, id.String())
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
		if p, err = scanPlan(rows, true); err != nil {
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
			SELECT `+headerColumns+`, snippet(plan_text, -1, '[', ']', '…', 12)
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
			p, err := scanPlan(rows, false, &snippet)
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
		// A word without a letter or digit has no token to look for.
		if !strings.ContainsFunc(w, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			continue
		}
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
	self_approval      INTEGER NOT NULL, -- 1 under self-approval (ADR 0016)
	approvers          TEXT NOT NULL,    -- JSON array
	tasks              TEXT NOT NULL,    -- JSON object: Task ID -> core.TaskStatus
	last_activity      INTEGER NOT NULL, -- Unix seconds; 0 when unknown
	intent             TEXT,             -- NULL when missing
	plan               TEXT              -- NULL when missing
);
CREATE VIRTUAL TABLE plan_text USING fts5(title, intent, plan, tokenize = 'unicode61 remove_diacritics 2');
`

// headerColumns are what Plans and Search return; docColumns adds the
// documents, which only Plan returns.
const (
	headerColumns = `p.id, p.short_id, p.state, p.title, p.labels, p.author_name, p.author_email,
	p.required_approvals, p.self_approval, p.approvers, p.tasks, p.last_activity`
	docColumns = headerColumns + `, p.intent, p.plan`
)

// connect opens the database file and checks or creates its schema.
func (ix *Index) connect(ctx context.Context) error {
	// _txlock=immediate takes the write lock at BEGIN, so two bfc processes
	// updating at once wait for each other instead of failing midway.
	db, err := sql.Open("sqlite", dsn(ix.path, "_pragma=busy_timeout(10000)&_txlock=immediate"))
	if err != nil {
		return err
	}
	ix.db = db
	// Checking the schema only reads, so readers do not queue for the
	// write lock; only creating it takes the lock.
	if done, err := checkSchema(ctx, db); done || err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if done, err := checkSchema(ctx, tx); done || err != nil {
		return err // another process created it meanwhile
	}
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// dsn returns the file: URI of the database at path with the given query.
// The path is escaped, so that '?' or '#' in it is not taken for the start of
// the parameters.
func dsn(path, query string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: query}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path // a Windows drive letter: file:///C:/...
	}
	return u.String()
}

// querier is a *sql.DB or a *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// checkSchema reports whether the database has the schema of this version
// (done), or is new and empty (not done, no error). Any other content is
// errOutdated.
func checkSchema(ctx context.Context, q querier) (done bool, err error) {
	var version, tables, ours int
	if err := q.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return false, err
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*),
		count(CASE WHEN type = 'table' AND name IN ('meta', 'plans', 'plan_text') THEN 1 END)
		FROM sqlite_schema`).Scan(&tables, &ours); err != nil {
		return false, err
	}
	switch {
	case version == schemaVersion && ours == 3:
		return true, nil
	case version == 0 && tables == 0:
		return false, nil
	default:
		// Another version, or a damaged file that has the right version
		// without its tables.
		return false, errOutdated
	}
}

// reset deletes the database file and creates a new one.
func (ix *Index) reset(ctx context.Context) error {
	_ = ix.Close()
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		if err := os.Remove(ix.path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			// On Windows a file another bfc has open cannot be deleted.
			// Empty it instead, which works for an outdated schema.
			return ix.wipe(ctx)
		}
	}
	return ix.connect(ctx)
}

// wipe drops everything in the database file and creates the schema anew.
func (ix *Index) wipe(ctx context.Context) error {
	err := ix.connect(ctx)
	if !discardable(err) {
		return err
	}
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for {
		// Virtual tables first: dropping one drops its shadow tables.
		var kind, name string
		err := tx.QueryRowContext(ctx, `SELECT type, name FROM sqlite_schema
			WHERE type IN ('table', 'view', 'trigger') AND name NOT LIKE 'sqlite_%'
			ORDER BY sql LIKE 'CREATE VIRTUAL%' DESC LIMIT 1`).Scan(&kind, &name)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP `+strings.ToUpper(kind)+` "`+strings.ReplaceAll(name, `"`, `""`)+`"`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 0`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = ix.Close()
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
		// An error, such as a pruned ref_commit, also means: not the same
		// history any more.
		if ok, err := ix.s.IsAncestor(ctx, since, tip); err != nil || !ok {
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
		tree, known := stored[id.String()]
		delete(stored, id.String())
		if known && tree == e.ID {
			continue
		}
		p, err := readPlan(ctx, ix.s, tip, id, e.Name)
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
	tasks := p.Status.Tasks
	if tasks == nil {
		tasks = map[string]core.TaskStatus{}
	}
	cl, err := json.Marshal(tasks)
	if err != nil {
		return err
	}
	var rowid int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO plans (id, tree, short_id, state, title, labels, author_name, author_email,
			required_approvals, self_approval, approvers, tasks, last_activity, intent, plan)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT (id) DO UPDATE SET tree = excluded.tree, short_id = excluded.short_id,
			state = excluded.state, title = excluded.title, labels = excluded.labels,
			author_name = excluded.author_name, author_email = excluded.author_email,
			required_approvals = excluded.required_approvals, self_approval = excluded.self_approval,
			approvers = excluded.approvers,
			tasks = excluded.tasks,
			intent = excluded.intent, plan = excluded.plan
		RETURNING rowid`,
		p.ID.String(), tree, p.Status.ShortID, string(p.Status.State), p.Title, string(labels),
		p.Status.Author.Name, p.Status.Author.Email, p.Status.RequiredApprovals, p.Status.SelfApproval, string(appr), string(cl),
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

// nullable stores a missing file as NULL and an empty one as ”.
func nullable(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}

// scanPlan scans a row of headerColumns, or of docColumns when docs is set,
// followed by the columns in extra.
func scanPlan(rows *sql.Rows, docs bool, extra ...any) (Plan, error) {
	var p Plan
	var id, state, labels, approvers, tasks string
	var lastActivity int64
	var intent, plan sql.NullString
	dest := []any{&id, &p.Status.ShortID, &state, &p.Title, &labels, &p.Status.Author.Name,
		&p.Status.Author.Email, &p.Status.RequiredApprovals, &p.Status.SelfApproval, &approvers, &tasks, &lastActivity}
	if docs {
		dest = append(dest, &intent, &plan)
	}
	if err := rows.Scan(append(dest, extra...)...); err != nil {
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
	if err := json.Unmarshal([]byte(tasks), &p.Status.Tasks); err != nil {
		return p, err
	}
	if len(p.Status.Tasks) == 0 {
		p.Status.Tasks = nil // as core.Derive leaves it
	}
	if lastActivity != 0 {
		p.LastActivity = time.Unix(lastActivity, 0).UTC()
	}
	if intent.Valid {
		p.Intent = []byte(intent.String)
	}
	if plan.Valid {
		p.Plan = []byte(plan.String)
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
