package index

import "database/sql"

// WriteOldSchema writes a database at path as an earlier version of bfc
// could have: user_version 0 and a plans table without most columns.
func WriteOldSchema(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE plans (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		_ = db.Close()
		return err
	}
	return db.Close()
}

// DropTable removes a table from the database at path, leaving its
// user_version as it is.
func DropTable(path, table string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`DROP TABLE ` + table); err != nil {
		_ = db.Close()
		return err
	}
	return db.Close()
}
