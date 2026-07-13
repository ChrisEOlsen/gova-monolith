package db

import (
	"path/filepath"
	"testing"
)

// OpenTest opens a temp-file SQLite database for a test, applies schema,
// and registers cleanup. Never touches /data/app.db. Uses a file (not
// :memory:) because Open returns separate Write/Read *sql.DB handles —
// :memory: without a shared-cache DSN would give each handle its own
// private database, so a write via Write would be invisible to Read.
func OpenTest(t *testing.T, schema string) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("db.OpenTest: open: %v", err)
	}
	if _, err := d.Write.Exec(schema); err != nil {
		d.Close()
		t.Fatalf("db.OpenTest: apply schema: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
