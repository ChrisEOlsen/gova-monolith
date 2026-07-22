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
		active BOOLEAN NOT NULL
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
		active BOOLEAN
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
		active INTEGER NOT NULL
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
		active TEXT NOT NULL
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
