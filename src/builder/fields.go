package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Field is one column of a model, as declared by a tool caller and then
// checked against the real table by applySchema.
type Field struct {
	Name string
	Type string
	// Nullable is filled in from PRAGMA table_info, never from the caller.
	Nullable bool
	// Format is a semantic hint (datetime-local, date, time, json, email) on a
	// string-stored column. Ref names the target model of a foreign key.
	Format string
	Ref    string
}

// knownFieldTypes is every type a field declaration may name. Enforced because
// parseFields cannot return an error, so an unrecognised type would otherwise
// fall through to `string` — turning a typo like `updated_at:timestmap` into
// exactly the defect the timestamp type exists to prevent.
var knownFieldTypes = map[string]bool{
	"string": true, "int": true, "boolean": true, "float": true, "timestamp": true,
}

// semanticFormats map a logical type onto a format hint. Each is stored as TEXT
// and carried in Go as a string; only the client-side control differs.
var semanticFormats = map[string]string{
	"datetime": "datetime-local",
	"date":     "date",
	"time":     "time",
	"json":     "json",
	"email":    "email",
}

// parseFields reads the `name:type` DSL. `name:ref:<model>` declares a foreign
// key; `name:<semantic>` declares a string with a format hint.
func parseFields(raw []string) []Field {
	fields := make([]Field, 0, len(raw))
	for _, f := range raw {
		parts := strings.Split(f, ":")
		name := parts[0]
		switch {
		case len(parts) >= 3 && parts[1] == "ref":
			fields = append(fields, Field{Name: name, Type: "int", Ref: parts[2]})
		case len(parts) == 2:
			if hint, ok := semanticFormats[parts[1]]; ok {
				fields = append(fields, Field{Name: name, Type: "string", Format: hint})
			} else {
				fields = append(fields, Field{Name: name, Type: parts[1]})
			}
		default:
			fields = append(fields, Field{Name: name, Type: "string"})
		}
	}
	return fields
}

func validateFieldTypes(fields []Field) error {
	for _, f := range fields {
		if !knownFieldTypes[f.Type] {
			return fmt.Errorf("field %q has unknown type %q — use one of: boolean, float, int, string, timestamp",
				f.Name, f.Type)
		}
	}
	return nil
}

var safeIdentRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func isSafeIdent(s string) bool { return safeIdentRe.MatchString(s) }

func toPascal(snake string) string {
	parts := strings.Split(snake, "_")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func toPlural(s string) string {
	switch {
	case strings.HasSuffix(s, "y"):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"):
		return s + "es"
	default:
		return s + "s"
	}
}

// goTypeFor maps a field type to its Go type. `timestamp` becomes models.Time
// (unqualified — generated models live in that package), which pins the JSON
// form to RFC3339 without fractional seconds. See docs/API-CONTRACT.md.
func goTypeFor(t string) string {
	switch t {
	case "int":
		return "int64"
	case "timestamp":
		return "Time"
	case "boolean":
		return "bool"
	case "float":
		return "float64"
	default:
		return "string"
	}
}

// nullTypeFor is the database/sql temporary a nullable column scans through.
// A timestamp uses models.NullTime rather than sql.NullTime so its payload is
// a Time and the nullable column serializes exactly like the non-nullable one.
func nullTypeFor(t string) string {
	switch t {
	case "int":
		return "sql.NullInt64"
	case "timestamp":
		return "NullTime"
	case "boolean":
		return "sql.NullBool"
	case "float":
		return "sql.NullFloat64"
	default:
		return "sql.NullString"
	}
}

func nullFieldFor(t string) string {
	switch t {
	case "int":
		return "Int64"
	case "timestamp":
		return "Time"
	case "boolean":
		return "Bool"
	case "float":
		return "Float64"
	default:
		return "String"
	}
}

// testLiteralFor is a sample value for a generated test. pkg qualifies the
// models package for a template that does not live in it ("models." from the
// handlers package, "" from within models itself).
func testLiteralFor(t, pkg string) string {
	switch t {
	case "int":
		return "int64(1)"
	case "timestamp":
		return pkg + "Time(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC))"
	case "boolean":
		return "true"
	case "float":
		return "1.5"
	default:
		return `"test"`
	}
}

// sqlTypeFor is the column type a generated test fixture declares.
func sqlTypeFor(t string) string {
	switch t {
	case "timestamp":
		return "DATETIME"
	case "int", "boolean":
		return "INTEGER"
	case "float":
		return "REAL"
	default:
		return "TEXT"
	}
}

// htmlInputType is the form control a field's format (or type) calls for. The
// iOS client picks its own control from the same format — see
// docs/API-CONTRACT.md.
func htmlInputType(f Field) string {
	if f.Format != "" && f.Format != "json" {
		return f.Format
	}
	switch f.Type {
	case "int", "float":
		return "number"
	case "boolean":
		return "checkbox"
	case "timestamp":
		return "datetime-local"
	default:
		return "text"
	}
}
