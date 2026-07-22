package main

import (
	"database/sql"
	"fmt"
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
	case strings.Contains(t, "INT"):
		return "INTEGER"
	case strings.Contains(t, "CHAR"), strings.Contains(t, "TEXT"), strings.Contains(t, "CLOB"):
		return "TEXT"
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"):
		return "REAL"
	}
	return t
}

// expectedSQLType mirrors the sqlType funcMap helper in main.go.
func expectedSQLType(fieldType string) string {
	switch fieldType {
	case "int", "boolean":
		return "INTEGER"
	case "float":
		return "REAL"
	default:
		return "TEXT"
	}
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
		if want := expectedSQLType(f.Type); c.SQLType != want {
			return nil, fmt.Errorf("field %q declared as %s (expects %s) but column %q.%s is %s",
				f.Name, f.Type, want, table, f.Name, c.SQLType)
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
