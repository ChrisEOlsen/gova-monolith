package main

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// reservedModelNames would collide with hand-written code in the models
// package. "time" is the shared timestamp type; "user" and "mobile_token" are
// auth's own models, which ship with the template.
var reservedModelNames = map[string]bool{
	"time":         true,
	"user":         true,
	"mobile_token": true,
}

func checkReservedName(name string) error {
	if reservedModelNames[strings.ToLower(name)] {
		return fmt.Errorf("model name %q is reserved by the template — it already exists in the models package", name)
	}
	return nil
}

// credentialColumn reports a column that stores a secret. The scaffolding tools
// build generic CRUD, where a PUT that merely omits a field writes its zero
// value — so a credential column reachable from one is a credential anybody can
// blank. Authentication ships as hand-written code in src/app; nothing
// generated has any business holding a secret.
func credentialColumn(name string) bool {
	n := strings.ToLower(name)
	// A stored digest, however it is spelled: password_hash, token_hash,
	// PasswordHash.
	if strings.HasSuffix(n, "hash") {
		return true
	}
	if strings.Contains(n, "password") || strings.Contains(n, "passwd") || strings.Contains(n, "passphrase") {
		return true
	}
	// Matched per underscore-separated segment, not as a substring, so
	// `secretary_id` and `salted_caramel` stay ordinary columns while
	// `client_secret` does not.
	for _, seg := range strings.Split(n, "_") {
		switch seg {
		case "secret", "otp", "salt", "apikey":
			return true
		}
	}
	return false
}

func checkNoCredentialColumns(fields []Field) error {
	for _, f := range fields {
		if credentialColumn(f.Name) {
			return fmt.Errorf("field %q looks like a credential — the scaffolding tools generate generic CRUD, "+
				"where a request that omits the field would overwrite it. Authentication already ships in "+
				"src/app/handlers/auth.go; keep secrets there", f.Name)
		}
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

// requireImplicitColumns checks the two columns every generated model uses
// without the caller declaring them: model.go.tmpl hard-codes `id` and
// `created_at` in the struct, the sort whitelist and the SELECT, and lists
// default to ORDER BY created_at DESC. Checked here so a missing column fails
// the command rather than the first request.
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
	if err := checkNoCredentialColumns(fields); err != nil {
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
		f.Nullable = !c.NotNull
		out = append(out, f)
	}
	return out, nil
}

// applySchema is the production entry point, against the live app database.
func applySchema(table string, fields []Field) ([]Field, error) {
	return applySchemaAt(dataDSN, table, fields)
}
