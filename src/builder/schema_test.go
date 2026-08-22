package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func testDSN(t *testing.T, schema string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	dsn := "file:" + path + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return dsn
}

const widgetSchema = `CREATE TABLE widgets (
	id INTEGER PRIMARY KEY,
	title TEXT NOT NULL,
	notes TEXT,
	count INTEGER NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`

func TestApplySchema_MarksNullableFields(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{
		{Name: "title", Type: "string"},
		{Name: "notes", Type: "string"},
		{Name: "count", Type: "int"},
	}

	got, err := applySchemaAt(dsn, "widgets", in)
	if err != nil {
		t.Fatalf("applySchemaAt: %v", err)
	}
	want := map[string]bool{"title": false, "notes": true, "count": false}
	for _, f := range got {
		if f.Nullable != want[f.Name] {
			t.Errorf("%s: Nullable got %v, want %v", f.Name, f.Nullable, want[f.Name])
		}
	}
}

func TestApplySchema_PreservesFieldOrder(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{
		{Name: "count", Type: "int"},
		{Name: "title", Type: "string"},
	}

	got, err := applySchemaAt(dsn, "widgets", in)
	if err != nil {
		t.Fatalf("applySchemaAt: %v", err)
	}
	if len(got) != 2 || got[0].Name != "count" || got[1].Name != "title" {
		t.Errorf("order not preserved: got %v", got)
	}
}

func TestApplySchema_UnknownFieldFails(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{{Name: "stat", Type: "string"}}

	_, err := applySchemaAt(dsn, "widgets", in)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
	// The message must name the real columns so the caller can self-correct.
	for _, want := range []string{"stat", "title", "notes", "count"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestApplySchema_TypeMismatchFails(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{{Name: "title", Type: "int"}}

	_, err := applySchemaAt(dsn, "widgets", in)
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("error %q missing field name", err)
	}
}

func TestApplySchema_MissingTableFails(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{{Name: "title", Type: "string"}}

	_, err := applySchemaAt(dsn, "gadgets", in)
	if err == nil {
		t.Fatal("expected error for missing table, got nil")
	}
	if !strings.Contains(err.Error(), "execute_sql") {
		t.Errorf("error %q should point at execute_sql", err)
	}
}

func TestApplySchema_NullablePasswordFails(t *testing.T) {
	dsn := testDSN(t, `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		email TEXT NOT NULL,
		password TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	in := []Field{{Name: "password", Type: "password"}}

	_, err := applySchemaAt(dsn, "users", in)
	if err == nil {
		t.Fatal("expected error for nullable password column, got nil")
	}
}

// checkReservedName is a pure lookup — no database involved.
func TestCheckReservedName(t *testing.T) {
	if err := checkReservedName("time"); err == nil {
		t.Error("expected 'time' to be rejected as a model name")
	}
	if err := checkReservedName("Time"); err == nil {
		t.Error("reserved check must be case-insensitive")
	}
	if err := checkReservedName("widget"); err != nil {
		t.Errorf("widget should be allowed, got %v", err)
	}
}

func TestApplySchema_UnsafeTableNameRejected(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	_, err := applySchemaAt(dsn, "widgets; DROP TABLE widgets", []Field{{Name: "title", Type: "string"}})
	if err == nil {
		t.Fatal("expected error for unsafe table name, got nil")
	}
}

func TestApplySchema_BooleanNotNullColumn(t *testing.T) {
	dsn := testDSN(t, `CREATE TABLE flags (
		id INTEGER PRIMARY KEY,
		active BOOLEAN NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	in := []Field{{Name: "active", Type: "boolean"}}

	got, err := applySchemaAt(dsn, "flags", in)
	if err != nil {
		t.Fatalf("applySchemaAt: %v", err)
	}
	if len(got) != 1 || got[0].Name != "active" || got[0].Nullable {
		t.Errorf("active: got Nullable=%v, want false", got[0].Nullable)
	}
}

func TestApplySchema_BooleanNullableColumn(t *testing.T) {
	dsn := testDSN(t, `CREATE TABLE flags (
		id INTEGER PRIMARY KEY,
		active BOOLEAN,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	in := []Field{{Name: "active", Type: "boolean"}}

	got, err := applySchemaAt(dsn, "flags", in)
	if err != nil {
		t.Fatalf("applySchemaAt: %v", err)
	}
	if len(got) != 1 || got[0].Name != "active" || !got[0].Nullable {
		t.Errorf("active: got Nullable=%v, want true", got[0].Nullable)
	}
}

func TestApplySchema_IntegerBooleanColumn(t *testing.T) {
	dsn := testDSN(t, `CREATE TABLE flags (
		id INTEGER PRIMARY KEY,
		active INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	in := []Field{{Name: "active", Type: "boolean"}}

	got, err := applySchemaAt(dsn, "flags", in)
	if err != nil {
		t.Fatalf("applySchemaAt: %v", err)
	}
	if len(got) != 1 || got[0].Name != "active" || got[0].Nullable {
		t.Errorf("active: got Nullable=%v, want false", got[0].Nullable)
	}
}

func TestApplySchema_BooleanFieldAgainstTextColumnFails(t *testing.T) {
	dsn := testDSN(t, `CREATE TABLE flags (
		id INTEGER PRIMARY KEY,
		active TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	in := []Field{{Name: "active", Type: "boolean"}}

	_, err := applySchemaAt(dsn, "flags", in)
	if err == nil {
		t.Fatal("expected error for boolean field against TEXT column, got nil")
	}
	if !strings.Contains(err.Error(), "active") {
		t.Errorf("error %q missing field name", err)
	}
}

// A table missing the columns every generated model selects must fail the tool
// call, not the request that eventually runs the query.
//
// These two were the gap: applySchemaAt checked every field the caller
// DECLARED and nothing it did not. model.go.tmpl selects "id" and "created_at"
// regardless, so a table without them scaffolded clean and broke on the first
// list request — and the generated model test could not see it, because
// model_test.go.tmpl builds its own table from a literal that has created_at in
// it. Green tests, broken endpoint.
func TestApplySchema_RequiresImplicitColumns(t *testing.T) {
	cases := []struct {
		name    string
		schema  string
		wantSub string
	}{
		{
			name: "no created_at",
			schema: `CREATE TABLE notes (
				id INTEGER PRIMARY KEY,
				body TEXT NOT NULL
			)`,
			wantSub: `has no "created_at" column`,
		},
		{
			name: "no id",
			schema: `CREATE TABLE notes (
				body TEXT NOT NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)`,
			wantSub: `has no "id" column`,
		},
		{
			name: "id is not an integer",
			schema: `CREATE TABLE notes (
				id TEXT PRIMARY KEY,
				body TEXT NOT NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)`,
			wantSub: `column "id" is TEXT`,
		},
		{
			name: "created_at is not a timestamp",
			schema: `CREATE TABLE notes (
				id INTEGER PRIMARY KEY,
				body TEXT NOT NULL,
				created_at INTEGER
			)`,
			wantSub: `column "created_at" is INTEGER`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dsn := testDSN(t, c.schema)
			_, err := applySchemaAt(dsn, "notes", []Field{{Name: "body", Type: "string"}})
			if err == nil {
				t.Fatal("expected an error, got none — the model would select a column that does not exist")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q does not mention %q", err.Error(), c.wantSub)
			}
		})
	}
}

// The happy path still passes: a table shaped like the Golden Recipe's example
// is accepted, so the new check cannot be satisfied only by the fixtures above.
func TestApplySchema_GoldenRecipeShapeAccepted(t *testing.T) {
	dsn := testDSN(t, `CREATE TABLE projects (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		status TEXT DEFAULT 'active',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	got, err := applySchemaAt(dsn, "projects", []Field{
		{Name: "name", Type: "string"},
		{Name: "status", Type: "string"},
	})
	if err != nil {
		t.Fatalf("the CREATE TABLE from CLAUDE.md's Golden Recipe must scaffold: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d fields, want 2", len(got))
	}
}
