package main

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// reservedModelNames would collide with hand-written identifiers in the
// generated models package. models.Time is the shared timestamp type; a model
// named "time" would produce a duplicate declaration that fails to compile.
var reservedModelNames = map[string]bool{
	"time": true,
}

func checkReservedName(name string) error {
	if reservedModelNames[strings.ToLower(name)] {
		return fmt.Errorf("model name %q is reserved — it would collide with a type in the models package", name)
	}
	return nil
}

type column struct {
	Name    string
	SQLType string
	NotNull bool
}

// tableColumnsAt reads a table's shape from SQLite's schema.
//
// PRAGMA does not accept bound parameters, so the table name is interpolated.
// The isSafeIdent check below is what makes that safe — it must stay.
func tableColumnsAt(dsn, table string) ([]column, error) {
	if !isSafeIdent(table) {
		return nil, fmt.Errorf("unsafe table name %q", table)
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := []column{}
	for rows.Next() {
		var (
			cid      int
			name     string
			declType string
			notNull  int
			dflt     sql.NullString
			pk       int
		)
		if err := rows.Scan(&cid, &name, &declType, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, column{Name: name, SQLType: normalizeSQLType(declType), NotNull: notNull == 1})
	}
	return cols, rows.Err()
}

// normalizeSQLType strips length qualifiers and casing so VARCHAR(255) and
// varchar both compare equal to TEXT's affinity family.
func normalizeSQLType(t string) string {
	t = strings.ToUpper(strings.TrimSpace(t))
	if i := strings.Index(t, "("); i >= 0 {
		t = t[:i]
	}
	switch {
	case strings.Contains(t, "BOOL"):
		return "INTEGER"
	case strings.Contains(t, "INT"):
		return "INTEGER"
	case strings.Contains(t, "CHAR"), strings.Contains(t, "TEXT"), strings.Contains(t, "CLOB"):
		return "TEXT"
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"):
		return "REAL"
	}
	return t
}

// acceptedSQLTypes lists the normalized column types a declared field type may
// legitimately sit on. It mirrors the sqlType funcMap helper in main.go.
//
// A list rather than one value because of `timestamp`: SQLite has no date type,
// so a DATETIME column is a convention, and the same column is spelled DATETIME
// by one author and TEXT by another. Both store the identical bytes and both
// scan into models.Time, so refusing one of them would be pedantry that pushes
// the author back to declaring the field a `string` — which is the defect this
// field type exists to remove.
func acceptedSQLTypes(fieldType string) []string {
	switch fieldType {
	case "int", "boolean":
		return []string{"INTEGER"}
	case "float":
		return []string{"REAL"}
	case "timestamp":
		return []string{"DATETIME", "TEXT", "DATE", "TIMESTAMP"}
	default:
		return []string{"TEXT"}
	}
}

// knownFieldTypes is every type a field declaration may name.
//
// Enforced because parseFields has no error return and an unrecognised type
// falls through goTypeFor's default to `string`. So `updated_at:timestmap`
// silently generated a string column, which is precisely the bug the timestamp
// type was added to fix, arriving by typo instead. Semantic formats
// (name:email and friends) are resolved to `string` before this runs and are
// deliberately not listed here.
var knownFieldTypes = map[string]bool{
	"string":    true,
	"int":       true,
	"boolean":   true,
	"float":     true,
	"password":  true,
	"timestamp": true,
}

func validateFieldTypes(fields []Field) error {
	for _, f := range fields {
		if !knownFieldTypes[f.Type] {
			return fmt.Errorf("field %q has unknown type %q — use one of: boolean, float, int, password, string, timestamp",
				f.Name, f.Type)
		}
	}
	return nil
}

// requireImplicitColumns checks the two columns every generated model uses
// without the caller ever declaring them.
//
// model.go.tmpl hard-codes both: `ID int64` and `CreatedAt Time` in the struct,
// "id" and "created_at" in AllowedColumns, and `SELECT id, ..., created_at` in
// GetPage. Nothing asked for them, so nothing checked for them — applySchemaAt
// validated only the fields the caller named.
//
// A table without created_at therefore scaffolded CLEANLY and failed at
// runtime with "no such column: created_at" on the first list request. The
// generated test could not catch it either, because model_test.go.tmpl builds
// its own table from a literal that includes created_at: the test passed
// against a schema the app does not use. A green suite plus a broken endpoint
// is the worst possible pairing, so this moves the failure to the tool call
// where the diff is still in front of you.
//
// orderByClause's default is "ORDER BY created_at DESC", so the column is load
// bearing for every list endpoint, not only for the JSON field.
func requireImplicitColumns(table string, cols []column) error {
	byName := make(map[string]column, len(cols))
	for _, c := range cols {
		byName[c.Name] = c
	}

	id, ok := byName["id"]
	if !ok {
		return fmt.Errorf("table %q has no \"id\" column — every generated model selects it; "+
			"declare it as `id INTEGER PRIMARY KEY`", table)
	}
	if id.SQLType != "INTEGER" {
		return fmt.Errorf("table %q column \"id\" is %s but generated models scan it into an int64 — "+
			"declare it as `id INTEGER PRIMARY KEY`", table, id.SQLType)
	}

	createdAt, ok := byName["created_at"]
	if !ok {
		return fmt.Errorf("table %q has no \"created_at\" column — every generated model selects it and "+
			"lists default to `ORDER BY created_at DESC`; "+
			"declare it as `created_at DATETIME DEFAULT CURRENT_TIMESTAMP`", table)
	}
	if !slices.Contains(acceptedSQLTypes("timestamp"), createdAt.SQLType) {
		return fmt.Errorf("table %q column \"created_at\" is %s but generated models scan it into models.Time — "+
			"declare it as `created_at DATETIME DEFAULT CURRENT_TIMESTAMP`", table, createdAt.SQLType)
	}
	return nil
}

// applySchemaAt validates declared fields against the real table and fills in
// Nullable from it.
//
// The fields argument stays a declaration of intent; the table is the source
// of truth. A mismatch fails the tool with a diff rather than silently
// generating a model that lies about the data.
func applySchemaAt(dsn, table string, fields []Field) ([]Field, error) {
	cols, err := tableColumnsAt(dsn, table)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %q does not exist — run execute_sql to create it before scaffolding", table)
	}

	if err := requireImplicitColumns(table, cols); err != nil {
		return nil, err
	}

	if err := validateFieldTypes(fields); err != nil {
		return nil, err
	}

	byName := make(map[string]column, len(cols))
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		byName[c.Name] = c
		names = append(names, c.Name)
	}

	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		c, ok := byName[f.Name]
		if !ok {
			return nil, fmt.Errorf("field %q is not a column of table %q (columns: %s)",
				f.Name, table, strings.Join(names, ", "))
		}
		accepted := acceptedSQLTypes(f.Type)
		if !slices.Contains(accepted, c.SQLType) {
			return nil, fmt.Errorf("field %q declared as %s (expects %s) but column %q.%s is %s",
				f.Name, f.Type, strings.Join(accepted, " or "), table, f.Name, c.SQLType)
		}
		if f.Type == "password" && !c.NotNull {
			return nil, fmt.Errorf("field %q is a password field but column %q.%s is nullable — declare it NOT NULL",
				f.Name, table, f.Name)
		}
		f.Nullable = !c.NotNull
		out = append(out, f)
	}
	return out, nil
}

// applySchema is the production entry point, against the live app database.
func applySchema(table string, fields []Field) ([]Field, error) {
	return applySchemaAt(sqliteDSN, table, fields)
}
