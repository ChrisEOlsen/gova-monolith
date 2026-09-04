package main

import (
	"bytes"
	"embed"
	"go/format"
	"log"
	"strings"
	"text/template"
	"time"
)

//go:embed templates/*
var templateFS embed.FS

// TemplateData is what every file template renders from.
type TemplateData struct {
	Name         string
	PascalName   string
	PluralName   string
	Fields       []Field
	AuthRequired bool
	Method       string
	Title        string
	CRUD         bool
}

func newData(name string, fields []Field) TemplateData {
	return TemplateData{
		Name:       name,
		PascalName: toPascal(name),
		PluralName: toPlural(name),
		Fields:     fields,
	}
}

var funcMap = template.FuncMap{
	"toPascal":  toPascal,
	"toPlural":  toPlural,
	"goType":    goTypeFor,
	"sqlType":   sqlTypeFor,
	"inputType": htmlInputType,

	"titleCase": func(s string) string {
		words := strings.Fields(strings.ReplaceAll(s, "_", " "))
		for i, w := range words {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		return strings.Join(words, " ")
	},

	// goFieldType is goType plus nullability: a nullable column becomes a Go
	// pointer, which marshals to JSON null and maps to a Swift optional. For
	// use inside package models, where Time is unqualified.
	"goFieldType": func(f Field) string {
		return withNullability(f, goTypeFor(f.Type))
	},

	// goFieldTypeQualified is goFieldType for a template outside package
	// models — the handlers package needs models.Time, not Time.
	"goFieldTypeQualified": func(f Field) string {
		base := goTypeFor(f.Type)
		if f.Type == "timestamp" {
			base = "models." + base
		}
		return withNullability(f, base)
	},

	"joinNames": func(fields []Field) string {
		names := make([]string, len(fields))
		for i, f := range fields {
			names[i] = f.Name
		}
		return strings.Join(names, ", ")
	},

	"placeholders": func(fields []Field) string {
		return strings.TrimSuffix(strings.Repeat("?, ", len(fields)), ", ")
	},

	"updateSet": func(fields []Field) string {
		parts := make([]string, len(fields))
		for i, f := range fields {
			parts[i] = f.Name + " = ?"
		}
		return strings.Join(parts, ", ")
	},

	"createParams": func(fields []Field) string {
		params := make([]string, len(fields))
		for i, f := range fields {
			goT := goTypeFor(f.Type)
			if f.Nullable {
				goT = "*" + goT
			}
			params[i] = f.Name + " " + goT
		}
		return strings.Join(params, ", ")
	},

	// scanDecls declares the temporaries a nullable column scans through.
	"scanDecls": func(fields []Field, indent string) string {
		lines := []string{}
		for _, f := range fields {
			if f.Nullable {
				lines = append(lines, indent+"var "+f.Name+"Null "+nullTypeFor(f.Type))
			}
		}
		return joinLines(lines)
	},

	// scanTargets emits the &-arguments for rows.Scan, routing nullable columns
	// through their temporaries.
	"scanTargets": func(fields []Field, prefix string) string {
		refs := make([]string, len(fields))
		for i, f := range fields {
			if f.Nullable {
				refs[i] = "&" + f.Name + "Null"
			} else {
				refs[i] = prefix + toPascal(f.Name)
			}
		}
		return strings.Join(refs, ", ")
	},

	// scanAssigns copies valid temporaries back onto the struct as pointers.
	"scanAssigns": func(fields []Field, target, indent string) string {
		lines := []string{}
		for _, f := range fields {
			if !f.Nullable {
				continue
			}
			lines = append(lines,
				indent+"if "+f.Name+"Null.Valid {",
				indent+"\t"+target+toPascal(f.Name)+" = &"+f.Name+"Null."+nullFieldFor(f.Type),
				indent+"}")
		}
		return joinLines(lines)
	},

	// structCallArgs passes a decoded request struct's fields to Create/Update.
	"structCallArgs": func(fields []Field, prefix string) string {
		args := make([]string, len(fields))
		for i, f := range fields {
			args[i] = prefix + toPascal(f.Name)
		}
		return strings.Join(args, ", ")
	},

	"sqlNotNull": func(f Field) string {
		if f.Nullable {
			return ""
		}
		return " NOT NULL"
	},

	// hasTimestamp lets a generated test import "time" only when it needs it.
	"hasTimestamp": func(fields []Field) bool {
		for _, f := range fields {
			if f.Type == "timestamp" {
				return true
			}
		}
		return false
	},

	// testArgs/testDecls take the package qualifier for models: "" inside the
	// models package, "models." from a handlers test.
	"testArgs": func(fields []Field, pkg string) string {
		vals := make([]string, len(fields))
		for i, f := range fields {
			if f.Nullable {
				// Non-nil, so the round trip exercises the nullable scan path
				// rather than short-circuiting on NULL.
				vals[i] = "&" + f.Name + "TestVal"
				continue
			}
			vals[i] = testLiteralFor(f.Type, pkg)
		}
		return strings.Join(vals, ", ")
	},

	"testDecls": func(fields []Field, indent, pkg string) string {
		lines := []string{}
		for _, f := range fields {
			if f.Nullable {
				lines = append(lines, indent+f.Name+"TestVal := "+testLiteralFor(f.Type, pkg))
			}
		}
		return joinLines(lines)
	},

	// testFieldMismatch is the round-trip comparison for one field. `!=` is
	// wrong for a timestamp: time.Time's == compares wall clock, monotonic
	// reading and *Location, so a value that came back through a different
	// location object differs while naming the same instant.
	"testFieldMismatch": func(f Field, got, want string) string {
		if f.Type == "timestamp" {
			return "!time.Time(" + got + ").Equal(time.Time(" + want + "))"
		}
		return got + " != " + want
	},

	// testJSON is a request body for a generated handler test. A timestamp must
	// be RFC3339 or models.Time refuses to decode it and the handler answers
	// 422 — which is correct behaviour and a useless test.
	"testJSON": func(fields []Field) string {
		parts := make([]string, len(fields))
		for i, f := range fields {
			v := `"test"`
			switch f.Type {
			case "int":
				v = "1"
			case "boolean":
				v = "true"
			case "float":
				v = "1.5"
			case "timestamp":
				v = `"2024-01-02T03:04:05Z"`
			}
			parts[i] = `"` + f.Name + `": ` + v
		}
		return "{" + strings.Join(parts, ", ") + "}"
	},
}

func withNullability(f Field, base string) string {
	if f.Nullable {
		return "*" + base
	}
	return base
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// getTemplate parses one embedded template. Not cached: the CLI is one process
// per invocation and never renders the same template twice in a run.
func getTemplate(name string) (*template.Template, error) {
	data, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return nil, err
	}
	return template.New(name).Funcs(funcMap).Parse(string(data))
}

func renderToString(tmplName string, data any) (string, error) {
	tmpl, err := getTemplate(tmplName)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderToFile(tmplName, outPath string, data any) error {
	out, err := renderToString(tmplName, data)
	if err != nil {
		return err
	}
	return writeFile(outPath, out)
}

// formatGo runs generated Go through gofmt. Doing it here rather than aligning
// twenty templates by hand against a {{range}} whose output length is unknown
// until it runs is the only maintainable option.
//
// A parse failure is not fatal: the unformatted bytes are written and the error
// surfaces at build time pointing at the real problem, rather than leaving the
// author an empty file and a message about formatting.
func formatGo(name, src string) string {
	formatted, err := format.Source([]byte(src))
	if err != nil {
		log.Printf("gova-builder: %s does not parse as Go, leaving it unformatted: %v", name, err)
		return src
	}
	return string(formatted)
}

// writeFile writes src atomically, gofmt-ing .go output first. Atomic because
// parallel subagents compile this tree while it is being written.
func writeFile(path, src string) error {
	if strings.HasSuffix(path, ".go") {
		src = formatGo(path, src)
	}
	return atomicWriteFile(path, []byte(src), 0644)
}

// updateManifest binds the real paths and the wall clock. Commands call it
// after rendering to self-register into api.json and regenerate the _gen files.
func updateManifest(models []Model, endpoints []Endpoint, pages []Page) error {
	return updateManifestAt(manifestPath(), handlersDir(), time.Now(), models, endpoints, pages)
}
