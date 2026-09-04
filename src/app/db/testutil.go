package db

import (
	"path/filepath"
	"testing"
)

// OpenTest opens a temp-file SQLite database for a test and registers cleanup.
// Never touches /data/app.db. The auth schema is applied by Open; extra is any
// additional DDL the test needs.
//
// A file rather than :memory: because Open returns separate Write and Read
// handles — each would otherwise get its own private database.
func OpenTest(t *testing.T, extra string) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.OpenTest: open: %v", err)
	}
	if extra != "" {
		if _, err := d.Write.Exec(extra); err != nil {
			d.Close()
			t.Fatalf("db.OpenTest: apply schema: %v", err)
		}
	}
	t.Cleanup(func() { d.Close() })
	return d
}
