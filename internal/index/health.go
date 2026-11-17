package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/admSla99/BeforeCode/internal/store"
)

// Health is what Check learns about the index without changing it.
type Health struct {
	// Built is false while there is no database, or an empty one; the next
	// read builds it.
	Built bool
	// Problem says why the database cannot be used, "" when it can. The next
	// read discards such a database and rebuilds it without a message.
	Problem string
	// RefCommit is the plans ref commit the index was built from.
	RefCommit string
}

// Check opens the index of the store's repository read-only and reports
// whether it can be read and which plans ref commit it was built from. It
// changes nothing; it fails only when the repository cannot be asked where
// the index lives.
func Check(ctx context.Context, s *store.Store) (Health, error) {
	common, err := s.CommonDir(ctx)
	if err != nil {
		return Health{}, err
	}
	path := filepath.Join(common, filepath.FromSlash(File))
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return Health{}, nil
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro&_pragma=busy_timeout(10000)"}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path // a Windows drive letter: file:///C:/...
	}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return Health{Problem: err.Error()}, nil
	}
	defer func() { _ = db.Close() }()

	ix := &Index{path: path}
	done, err := ix.checkSchema(ctx, db)
	switch {
	case errors.Is(err, errOutdated):
		return Health{Problem: "written by another version of bfc"}, nil
	case err != nil:
		return Health{Problem: err.Error()}, nil
	case !done:
		return Health{}, nil
	}
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return Health{Problem: err.Error()}, nil
	}
	if result != "ok" {
		return Health{Problem: "damaged: " + result}, nil
	}
	h := Health{Built: true}
	err = db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'ref_commit'`).Scan(&h.RefCommit)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Health{Problem: err.Error()}, nil
	}
	return h, nil
}

// Rebuild discards the index of the store's repository and builds it again
// from the plans ref.
func Rebuild(ctx context.Context, s *store.Store) error {
	ix, err := Open(ctx, s)
	if err != nil {
		return err
	}
	defer func() { _ = ix.Close() }()
	if err := ix.reset(ctx); err != nil {
		return fmt.Errorf("index: rebuild: %w", err)
	}
	_, err = ix.Update(ctx)
	return err
}
