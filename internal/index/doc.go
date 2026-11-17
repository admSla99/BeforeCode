// Package index is the local SQLite cache of the plans ref, used to list
// and search plans. It lives in .git/beforecode/index.db, records the ref
// commit it was built from, and is disposable: a missing, corrupt or
// outdated database is rebuilt from the plans ref.
package index
