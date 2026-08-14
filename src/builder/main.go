package main

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed templates/*
var templateFS embed.FS

// sqliteDSN matches src/app/db/db.go's Open() pragmas — WAL mode and a
// busy timeout — so the builder's DDL connections (execute_sql,
// scaffold_auth) behave consistently with the app container's live
// connection instead of using SQLite's rollback-journal default.
const sqliteDSN = "file:/data/app.db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=NORMAL"

var (
	tmplCache   = map[string]*template.Template{}
	tmplCacheMu sync.RWMutex
)

var funcMap = template.FuncMap{
	"toPascal": toPascal,
	"toPlural": toPlural,
	"titleCase": func(s string) string {
		s = strings.ReplaceAll(s, "_", " ")
		words := strings.Fields(s)
		for i, w := range words {
			if len(w) > 0 {
				words[i] = strings.ToUpper(w[:1]) + w[1:]
			}
		}
		return strings.Join(words, " ")
	},
	"goType": goTypeFor,
	// goFieldType is goType plus nullability: a nullable column becomes a Go
	// pointer, which marshals to JSON null and maps to a Swift optional.
	"goFieldType": func(f Field) string {
		base := goTypeFor(f.Type)
		if f.Nullable {
			return "*" + base
		}
		return base
	},
	"joinNames": func(fields []Field) string {
		names := make([]string, len(fields))
		for i, f := range fields {
			names[i] = f.Name
		}
		return strings.Join(names, ", ")
	},
	// scanDecls emits the temporaries a row scan needs for nullable columns.
	"scanDecls": func(fields []Field, indent string) string {
		lines := []string{}
		for _, f := range fields {
			if f.Nullable {
				lines = append(lines, indent+"var "+f.Name+"Null "+nullTypeFor(f.Type))
			}
		}
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n") + "\n"
	},
	// scanTargets emits the &-arguments for rows.Scan, routing nullable
	// columns through their temporaries.
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
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n") + "\n"
	},
	"placeholders": func(fields []Field) string {
		p := make([]string, len(fields))
		for i := range fields {
			p[i] = "?"
		}
		return strings.Join(p, ", ")
	},
	// updateSet emits "field1 = ?, field2 = ?" for an UPDATE statement.
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
	"insertArgs": func(fields []Field) string {
		args := make([]string, len(fields))
		for i, f := range fields {
			if f.Type == "password" {
				args[i] = "string(hashed)"
			} else {
				args[i] = f.Name
			}
		}
		return strings.Join(args, ", ")
	},
	"sqlType": func(t string) string {
		switch t {
		case "int":
			return "INTEGER"
		case "boolean":
			return "INTEGER"
		case "float":
			return "REAL"
		default:
			return "TEXT"
		}
	},
	"testArgs": func(fields []Field) string {
		vals := make([]string, len(fields))
		for i, f := range fields {
			if f.Nullable {
				// Non-nil pointer so the round-trip actually exercises the
				// nullable scan path rather than short-circuiting on NULL.
				vals[i] = "&" + f.Name + "TestVal"
				continue
			}
			vals[i] = testLiteralFor(f.Type)
		}
		return strings.Join(vals, ", ")
	},
	// testDecls declares the addressable locals testArgs points at.
	"testDecls": func(fields []Field, indent string) string {
		lines := []string{}
		for _, f := range fields {
			if f.Nullable {
				lines = append(lines, indent+f.Name+"TestVal := "+testLiteralFor(f.Type))
			}
		}
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n") + "\n"
	},
	// sqlNotNull emits the NOT NULL clause for generated fixture schemas so
	// the test table's shape matches the model the test exercises.
	"sqlNotNull": func(f Field) string {
		if f.Nullable {
			return ""
		}
		return " NOT NULL"
	},
	// structCallArgs emits "prefix.Field1, prefix.Field2" using PascalCase field
	// names — for passing a decoded request struct's fields to Create/Update.
	"structCallArgs": func(fields []Field, prefix string) string {
		args := make([]string, len(fields))
		for i, f := range fields {
			args[i] = prefix + toPascal(f.Name)
		}
		return strings.Join(args, ", ")
	},
	// testJSON emits a JSON object literal with a test value per field, for
	// building a create/update request body in generated tests.
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
			}
			parts[i] = `"` + f.Name + `": ` + v
		}
		return "{" + strings.Join(parts, ", ") + "}"
	},
}

func goTypeFor(t string) string {
	switch t {
	case "int":
		return "int64"
	case "boolean":
		return "bool"
	case "float":
		return "float64"
	default:
		return "string"
	}
}

func nullTypeFor(t string) string {
	switch t {
	case "int":
		return "sql.NullInt64"
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
	case "boolean":
		return "Bool"
	case "float":
		return "Float64"
	default:
		return "String"
	}
}

func testLiteralFor(t string) string {
	switch t {
	case "int":
		return "int64(1)"
	case "boolean":
		return "true"
	case "float":
		return "1.5"
	default:
		return `"test"`
	}
}

func getTemplate(name string) (*template.Template, error) {
	tmplCacheMu.RLock()
	t, ok := tmplCache[name]
	tmplCacheMu.RUnlock()
	if ok {
		return t, nil
	}
	data, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return nil, err
	}
	t, err = template.New(name).Funcs(funcMap).Parse(string(data))
	if err != nil {
		return nil, err
	}
	tmplCacheMu.Lock()
	tmplCache[name] = t
	tmplCacheMu.Unlock()
	return t, nil
}

var safeIdentRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func isSafeIdent(s string) bool { return safeIdentRe.MatchString(s) }

func toPascal(snake string) string {
	parts := strings.Split(snake, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func toPlural(s string) string {
	if strings.HasSuffix(s, "y") {
		return s[:len(s)-1] + "ies"
	}
	if strings.HasSuffix(s, "s") {
		return s + "es"
	}
	return s + "s"
}

type Field struct {
	Name string
	Type string
	// Nullable is filled in by applySchema from the real table's
	// PRAGMA table_info output — never from the caller's field argument.
	Nullable bool
	// Format is a semantic hint (datetime-local, date, time, json, email) for a
	// string-stored column. Ref is the target model name for a foreign key.
	// Both are declaration metadata layered over the base storage Type.
	Format string
	Ref    string
}

// semanticFormats maps a DSL logical type to its manifest format hint. Each is
// stored as TEXT and carried in Go as a string — only the semantic differs.
var semanticFormats = map[string]string{
	"datetime": "datetime-local",
	"date":     "date",
	"time":     "time",
	"json":     "json",
	"email":    "email",
}

func parseFields(raw []string) []Field {
	fields := make([]Field, 0, len(raw))
	for _, f := range raw {
		parts := strings.Split(f, ":")
		name := parts[0]
		switch {
		case len(parts) >= 3 && parts[1] == "ref":
			// name:ref:<model> -> INTEGER FK column, int64 in Go.
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

type TemplateData struct {
	Name         string
	PascalName   string
	PluralName   string
	Fields       []Field
	HasPassword  bool
	AuthRequired bool
	Method       string
	Title        string
	Filename     string
	APIEndpoint  string
	SubmitLabel  string
	FormName     string
	CRUD         bool
}

func newData(name string, fields []Field) TemplateData {
	hasPw := false
	for _, f := range fields {
		if f.Type == "password" {
			hasPw = true
		}
	}
	return TemplateData{
		Name:        name,
		PascalName:  toPascal(name),
		PluralName:  toPlural(name),
		Fields:      fields,
		HasPassword: hasPw,
	}
}

func errResult(msg string) *mcp.CallToolResult {
	return mcp.NewToolResultError(msg)
}

// updateManifest is the production wrapper around updateManifestAt, binding
// the real manifest/handlers paths and the wall clock. Tool handlers call
// this after rendering their files to self-register into api.json and
// regenerate routes_gen.go + pages_gen.go.
func updateManifest(models []Model, endpoints []Endpoint, pages []Page) error {
	return updateManifestAt(manifestFilePath, handlersDirPath, time.Now(), models, endpoints, pages)
}

func renderToFile(tmplName, outPath string, data TemplateData) error {
	tmpl, err := getTemplate(tmplName)
	if err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return tmpl.Execute(f, data)
}

func renderToString(tmplName string, data TemplateData) (string, error) {
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

// renderNamedToString renders tmplName with an arbitrary payload, for
// templates (like routes_gen.go.tmpl) whose data shape isn't TemplateData.
func renderNamedToString(tmplName string, data any) (string, error) {
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

func rawFieldsToStrings(raw []interface{}) []string {
	s := make([]string, len(raw))
	for i, v := range raw {
		s[i], _ = v.(string)
	}
	return s
}

func runPatternChecks() string {
	bannedPatterns := []struct{ pattern, message string }{
		{`db\.Exec\(fmt\.Sprintf`, "SQL injection risk: use prepared statements"},
		{`db\.Query\(fmt\.Sprintf`, "SQL injection risk: use prepared statements"},
		{`\.innerHTML\s*=`, "XSS risk: use textContent or createElement instead of innerHTML"},
	}
	violations := []string{}
	goFiles, _ := filepath.Glob("/src/app/handlers/*.go")
	jsFiles, _ := filepath.Glob("/src/app/static/js/*.js")
	for _, file := range append(goFiles, jsFiles...) {
		content, _ := os.ReadFile(file)
		for _, bp := range bannedPatterns {
			re := regexp.MustCompile(bp.pattern)
			if re.Match(content) {
				violations = append(violations, "  "+filepath.Base(file)+": "+bp.message)
			}
		}
	}
	if len(violations) > 0 {
		return "Pattern check FAILED — fix before deploying:\n" + strings.Join(violations, "\n")
	}
	return "Pattern check passed."
}

func main() {
	s := server.NewMCPServer("gova-builder", "1.0.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(mcp.NewTool("inspect_app",
		mcp.WithDescription("Return current app state: all models, handlers, JS pages, and registered routes. Call BEFORE scaffolding to avoid duplicates."),
	), handleInspectApp)

	s.AddTool(mcp.NewTool("execute_sql",
		mcp.WithDescription("Execute SQL DDL or DML against /data/app.db. Use FIRST — tables must exist before models. Never write raw SQL inside handlers."),
		mcp.WithString("query", mcp.Required(), mcp.Description("SQL to execute")),
	), handleExecuteSQL)

	s.AddTool(mcp.NewTool("create_model",
		mcp.WithDescription("Generate models/Name.go with GetPage/Find/Create/Delete and 5-min cache. Table must exist first (run execute_sql)."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Model name in snake_case")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
	), handleCreateModel)

	s.AddTool(mcp.NewTool("create_handler",
		mcp.WithDescription("Generate a single JSON handler in handlers/name.go AND register its route in api.json + routes_gen.go. Implement the TODO logic after. Declare request_schema/response_schema (JSON: {\"shape\":\"object|list|empty\",\"model\":\"<name>\"?,\"fields\":[{\"name\",\"type\",\"nullable\",\"format\"}]?}) and a one-line summary so native clients can consume this custom endpoint — a custom endpoint without a declared body is opaque to them."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Handler name in snake_case")),
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method: GET, POST, PUT, DELETE")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Full route path, e.g. /api/v1/projects/{id}/archive")),
		mcp.WithBoolean("auth_required", mcp.Description("Require authentication — enforced by a RequireAuth route wrap")),
		mcp.WithString("request_schema", mcp.Description("JSON BodySchema for the request body (omit for GET/no-body endpoints)")),
		mcp.WithString("response_schema", mcp.Description("JSON BodySchema for the response data")),
		mcp.WithString("summary", mcp.Description("One-line description of what this endpoint does")),
	), handleCreateHandler)

	s.AddTool(mcp.NewTool("create_page",
		mcp.WithDescription("Generate a page: static/pages/filename.html + static/js/filename.js, and register it at a human-facing URL in api.json's pages table + pages_gen.go. The page is served by the generated pageFile helper — no Go handler is created or needed. Use create_handler for the JSON endpoints the page's JS calls. After: add forms with add_js_form."),
		mcp.WithString("filename", mcp.Required(), mcp.Description("Page filename without extension")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Page title")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Human-facing URL, e.g. /projects or /settings. Must NOT be under /api/ — that namespace belongs to create_handler.")),
		mcp.WithBoolean("auth_required", mcp.Description("JS module calls requireAuth() on load. Page shells are not wrapped server-side; protect the data on its /api/v1/ endpoints.")),
	), handleCreatePage)

	s.AddTool(mcp.NewTool("scaffold_list",
		mcp.WithDescription("Generate 4 files: model + JSON list handler + HTML shell + JS module, and register the GET route in api.json + routes_gen.go. After: add forms with add_js_form."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Resource name in snake_case")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
	), handleScaffoldList)

	s.AddTool(mcp.NewTool("scaffold_resource",
		mcp.WithDescription("Generate full CRUD for a resource: model (with Update) + list/detail/create/update/delete handlers + list page, and register all 5 routes in api.json + routes_gen.go. List supports ?sort=&filter= (whitelisted columns). Table must exist first (run execute_sql). Endpoints are public; protect per-endpoint via the manifest. Use scaffold_list for read-only resources."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Resource name in snake_case")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
	), handleScaffoldResource)

	s.AddTool(mcp.NewTool("scaffold_auth",
		mcp.WithDescription("Generate the full auth system — cookie (web) AND bearer (mobile) in one run: users + rate_limits + mobile_tokens tables, User + MobileToken models, cookie handlers (login/logout/me) + bearer handlers (login_token/logout_token/me_token) and the login page, all 6 routes self-registered in api.json + routes_gen.go. Run scaffold_registration after for a registration endpoint."),
	), handleScaffoldAuth)

	s.AddTool(mcp.NewTool("scaffold_registration",
		mcp.WithDescription("Generate registration JSON handler + HTML page. Run after scaffold_auth. Registers the route in api.json + routes_gen.go."),
	), handleScaffoldRegistration)

	s.AddTool(mcp.NewTool("add_js_form",
		mcp.WithDescription("Inject a creation form into an existing JS module at the // @inject-forms marker. The form uses api.js for submission. Requires: (1) JS file exists with the marker, (2) a POST handler exists at api_endpoint."),
		mcp.WithString("page", mcp.Required(), mcp.Description("Target page filename without extension")),
		mcp.WithString("api_endpoint", mcp.Required(), mcp.Description("API endpoint the form POSTs to")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
		mcp.WithString("title", mcp.Description("Optional form section title")),
		mcp.WithString("submit_label", mcp.Description("Submit button label (default: Submit)")),
	), handleAddJSForm)

	if err := server.ServeStdio(s); err != nil {
		log.Fatal(err)
	}
}

// Tool handler stubs — implemented in subsequent tasks
func handleInspectApp(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	scan := func(pattern string) []string {
		files, _ := filepath.Glob(pattern)
		names := []string{}
		for _, f := range files {
			base := filepath.Base(f)
			if base == ".gitkeep" {
				continue
			}
			names = append(names, base)
		}
		return names
	}
	onDisk := onDiskFiles{
		Models:   scan("/src/app/models/*.go"),
		Handlers: scan("/src/app/handlers/*.go"),
		Pages:    scan("/src/app/static/pages/*.html"),
		JS:       scan("/src/app/static/js/*.js"),
	}
	m, err := readManifestAt(manifestFilePath)
	if err != nil {
		return errResult(err.Error()), nil
	}
	rep := buildInspection(m, onDisk)
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
func handleExecuteSQL(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, _ := req.Params.Arguments["query"].(string)
	if query == "" {
		return errResult("query is required"), nil
	}
	// Same pragmas as db.Open (src/app/db/db.go): WAL mode and a busy
	// timeout so DDL here doesn't collide with the app container's live
	// connection, and so a fresh db file ends up in WAL mode immediately
	// rather than waiting for the app to connect first.
	db, err := sql.Open("sqlite3", sqliteDSN)
	if err != nil {
		return errResult(err.Error()), nil
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, query); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText("SQL executed successfully"), nil
}
func handleCreateModel(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, _ := req.Params.Arguments["name"].(string)
	if !isSafeIdent(name) {
		return errResult("invalid model name: only alphanumeric and underscore allowed"), nil
	}
	rawFields, _ := req.Params.Arguments["fields"].([]interface{})
	fields := parseFields(rawFieldsToStrings(rawFields))
	if err := checkReservedName(name); err != nil {
		return errResult(err.Error()), nil
	}
	fields, applyErr := applySchema(toPlural(name), fields)
	if applyErr != nil {
		return errResult(applyErr.Error()), nil
	}
	data := newData(name, fields)

	outPath := "/src/app/models/" + toPascal(name) + ".go"
	if err := renderToFile("model.go.tmpl", outPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	testPath := "/src/app/models/" + toPascal(name) + "_test.go"
	if err := renderToFile("model_test.go.tmpl", testPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText("Created: " + outPath + "\nCreated: " + testPath), nil
}
func handleCreateHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, _ := req.Params.Arguments["name"].(string)
	method, _ := req.Params.Arguments["method"].(string)
	path, _ := req.Params.Arguments["path"].(string)
	authRequired, _ := req.Params.Arguments["auth_required"].(bool)
	requestSchema, _ := req.Params.Arguments["request_schema"].(string)
	responseSchema, _ := req.Params.Arguments["response_schema"].(string)
	summary, _ := req.Params.Arguments["summary"].(string)
	if !isSafeIdent(name) {
		return errResult("invalid handler name"), nil
	}
	if !strings.HasPrefix(path, "/api/v1/") {
		return errResult("path must start with /api/v1/"), nil
	}
	reqSchema, err := parseBodySchemaArg(requestSchema)
	if err != nil {
		return errResult("request_schema: " + err.Error()), nil
	}
	respSchema, err := parseBodySchemaArg(responseSchema)
	if err != nil {
		return errResult("response_schema: " + err.Error()), nil
	}
	data := newData(name, nil)
	data.Method = strings.ToUpper(method)
	data.AuthRequired = authRequired

	outPath := "/src/app/handlers/" + name + ".go"
	if err := renderToFile("handler.go.tmpl", outPath, data); err != nil {
		return errResult(err.Error()), nil
	}

	endpoint := Endpoint{
		Method: strings.ToUpper(method), Path: path,
		Handler: toPascal(name) + strings.ToUpper(method),
		Deps:    []string{"read", "write", "cache"},
		Auth:    authRequired, Kind: "custom",
		Summary: summary, Request: reqSchema, Response: respSchema,
	}
	if err := updateManifest(nil, []Endpoint{endpoint}, nil); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}

	return mcp.NewToolResultText("Created: " + outPath +
		"\nRegistered " + strings.ToUpper(method) + " " + path +
		" in api.json + routes_gen.go.\nImplement the TODO logic.\n\n" + runPatternChecks()), nil
}
func handleCreatePage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filename, _ := req.Params.Arguments["filename"].(string)
	title, _ := req.Params.Arguments["title"].(string)
	path, _ := req.Params.Arguments["path"].(string)
	authRequired, _ := req.Params.Arguments["auth_required"].(bool)
	if !isSafeIdent(filename) {
		return errResult("invalid filename"), nil
	}
	if err := validatePagePath(path); err != nil {
		return errResult(err.Error()), nil
	}
	data := newData(filename, nil)
	data.Title = title
	data.AuthRequired = authRequired
	data.Method = "GET"

	htmlPath := "/src/app/static/pages/" + filename + ".html"
	if err := renderToFile("page.html.tmpl", htmlPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	jsPath := "/src/app/static/js/" + filename + ".js"
	if err := renderToFile("page.js.tmpl", jsPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	page := Page{Path: path, File: filename, Title: title, Auth: authRequired}
	if err := updateManifest(nil, nil, []Page{page}); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}

	return mcp.NewToolResultText(
		"Created: " + htmlPath + "\nCreated: " + jsPath +
			"\nRegistered page " + path + " -> static/pages/" + filename + ".html in api.json + pages_gen.go.\n" +
			"The page is served by the generated pageFile helper — there is no Go handler to implement.\n" +
			"Add API endpoints with create_handler, and forms with add_js_form.\n\n" + runPatternChecks(),
	), nil
}

// validatePagePath enforces the page namespace: a human-facing URL, never an
// API one. This is the exact inverse of create_handler's check, and it is what
// makes the two tables provably disjoint — no page can shadow an endpoint and
// no endpoint can shadow a page.
func validatePagePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return errors.New("page path must start with /")
	}
	if path == "/api" || strings.HasPrefix(path, "/api/") {
		return errors.New("page path must not be under /api/ — that namespace belongs to create_handler and the scaffold tools; give the page a human-facing URL like /projects")
	}
	if strings.HasPrefix(path, "/static/") {
		return errors.New("page path must not be under /static/ — that prefix is the static file server")
	}
	return nil
}
func handleScaffoldList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, _ := req.Params.Arguments["name"].(string)
	rawFields, _ := req.Params.Arguments["fields"].([]interface{})
	if !isSafeIdent(name) {
		return errResult("invalid name"), nil
	}
	fields := parseFields(rawFieldsToStrings(rawFields))
	if len(fields) == 0 {
		return errResult("at least one field is required"), nil
	}
	if err := checkReservedName(name); err != nil {
		return errResult(err.Error()), nil
	}
	fields, applyErr := applySchema(toPlural(name), fields)
	if applyErr != nil {
		return errResult(applyErr.Error()), nil
	}
	if err := validateRefs(fields); err != nil {
		return errResult(err.Error()), nil
	}
	data := newData(name, fields)
	data.Title = toPascal(toPlural(name))

	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"model.go.tmpl", "/src/app/models/" + toPascal(name) + ".go"},
		{"model_test.go.tmpl", "/src/app/models/" + toPascal(name) + "_test.go"},
		{"list_handler.go.tmpl", "/src/app/handlers/" + name + "_list.go"},
		{"list_handler_test.go.tmpl", "/src/app/handlers/" + name + "_list_test.go"},
		{"list_page.html.tmpl", "/src/app/static/pages/" + toPlural(name) + ".html"},
		{"list_page.js.tmpl", "/src/app/static/js/" + toPlural(name) + ".js"},
	}

	results := []string{}
	for _, spec := range specs {
		if err := renderToFile(spec.tmpl, spec.out, data); err != nil {
			return errResult(err.Error()), nil
		}
		results = append(results, "Created: "+spec.out)
	}

	model := fieldsToModel(name, toPlural(name), fields)
	endpoint := Endpoint{
		Method: "GET", Path: "/api/v1/" + toPlural(name),
		Handler: toPascal(name) + "ListGET",
		Deps:    []string{"read", "write", "cache"},
		Auth:    false, Model: name, Kind: "list",
		Response: resourceResponse(model, "list"),
	}
	if err := updateManifest([]Model{model}, []Endpoint{endpoint}, nil); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}

	return mcp.NewToolResultText(
		strings.Join(results, "\n") +
			"\n\nRegistered route GET /api/v1/" + toPlural(name) + " and updated api.json + routes_gen.go.\n" +
			"Add forms with add_js_form.\n\n" + runPatternChecks(),
	), nil
}
// resourceEndpoints returns the five CRUD endpoints scaffold_resource registers,
// each carrying the request/response body schema derived from the model + kind.
// The handler symbols must match resource_handlers.go.tmpl exactly.
func resourceEndpoints(m Model) []Endpoint {
	p := toPascal(m.Name)
	plural := toPlural(m.Name)
	base := "/api/v1/" + plural
	rwc := []string{"read", "write", "cache"}
	mk := func(method, path, handler, kind string) Endpoint {
		return Endpoint{
			Method: method, Path: path, Handler: handler, Deps: rwc,
			Model: m.Name, Kind: kind,
			Request:  resourceRequest(m, kind),
			Response: resourceResponse(m, kind),
		}
	}
	return []Endpoint{
		mk("GET", base, p+"ListGET", "list"),
		mk("GET", base+"/{id}", p+"DetailGET", "detail"),
		mk("POST", base, p+"CreatePOST", "create"),
		mk("PUT", base+"/{id}", p+"UpdatePUT", "update"),
		mk("DELETE", base+"/{id}", p+"DeleteDELETE", "delete"),
	}
}

// authEndpoints returns the six endpoints scaffold_auth registers — the cookie
// set (login/logout/me) and the bearer set (login_token/logout_token/me_token).
// The three token endpoints are auth:false: they self-enforce the bearer token
// in the handler; a session-cookie RequireAuth wrap would 401 them.
func authEndpoints() []Endpoint {
	rwc := []string{"read", "write", "cache"}
	return []Endpoint{
		{Method: "POST", Path: "/api/v1/auth/login", Handler: "LoginPOST", Deps: rwc, Kind: "auth_login"},
		{Method: "POST", Path: "/api/v1/auth/logout", Handler: "LogoutPOST", Deps: []string{}, Kind: "auth_logout"},
		{Method: "GET", Path: "/api/v1/auth/me", Handler: "MeGET", Deps: rwc, Auth: true, Kind: "auth_me"},
		{Method: "POST", Path: "/api/v1/auth/login_token", Handler: "MobileLoginPOST", Deps: rwc, Kind: "mobile_login"},
		// logout_token takes both handles because it goes through
		// models.MobileTokenModel, whose constructor owns the read side too —
		// the handler itself no longer touches the database directly.
		{Method: "DELETE", Path: "/api/v1/auth/logout_token", Handler: "MobileLogoutDELETE", Deps: []string{"read", "write"}, Kind: "mobile_logout"},
		{Method: "GET", Path: "/api/v1/auth/me_token", Handler: "MobileMeGET", Deps: rwc, Kind: "mobile_me"},
	}
}

func handleScaffoldResource(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, _ := req.Params.Arguments["name"].(string)
	rawFields, _ := req.Params.Arguments["fields"].([]interface{})
	if !isSafeIdent(name) {
		return errResult("invalid name"), nil
	}
	fields := parseFields(rawFieldsToStrings(rawFields))
	if len(fields) == 0 {
		return errResult("at least one field is required"), nil
	}
	if err := checkReservedName(name); err != nil {
		return errResult(err.Error()), nil
	}
	fields, applyErr := applySchema(toPlural(name), fields)
	if applyErr != nil {
		return errResult(applyErr.Error()), nil
	}
	if err := validateRefs(fields); err != nil {
		return errResult(err.Error()), nil
	}
	data := newData(name, fields)
	data.CRUD = true
	data.Title = toPascal(toPlural(name))

	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"model.go.tmpl", "/src/app/models/" + toPascal(name) + ".go"},
		{"model_test.go.tmpl", "/src/app/models/" + toPascal(name) + "_test.go"},
		{"resource_handlers.go.tmpl", "/src/app/handlers/" + name + "_resource.go"},
		{"resource_handlers_test.go.tmpl", "/src/app/handlers/" + name + "_resource_test.go"},
		{"list_page.html.tmpl", "/src/app/static/pages/" + toPlural(name) + ".html"},
		{"list_page.js.tmpl", "/src/app/static/js/" + toPlural(name) + ".js"},
	}
	results := []string{}
	for _, spec := range specs {
		if err := renderToFile(spec.tmpl, spec.out, data); err != nil {
			return errResult(err.Error()), nil
		}
		results = append(results, "Created: "+spec.out)
	}

	model := fieldsToModel(name, toPlural(name), fields)
	if err := updateManifest([]Model{model}, resourceEndpoints(model), nil); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}

	return mcp.NewToolResultText(
		strings.Join(results, "\n") +
			"\n\nRegistered full CRUD (list, detail, create, update, delete) for /api/v1/" + toPlural(name) +
			" in api.json + routes_gen.go. Endpoints are public — set auth:true per endpoint in api.json to protect them (requires scaffold_auth).\n" +
			"Add a create form with add_js_form.\n\n" + runPatternChecks(),
	), nil
}
func handleScaffoldAuth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Same pragmas as db.Open (src/app/db/db.go): WAL mode and a busy
	// timeout so DDL here doesn't collide with the app container's live
	// connection, and so a fresh db file ends up in WAL mode immediately
	// rather than waiting for the app to connect first.
	db, err := sql.Open("sqlite3", sqliteDSN)
	if err != nil {
		return errResult(err.Error()), nil
	}
	defer db.Close()

	ddl := `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS rate_limits (
	ip TEXT NOT NULL,
	attempts INTEGER DEFAULT 0,
	locked_until DATETIME,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (ip)
);
CREATE TABLE IF NOT EXISTS mobile_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	expires_at DATETIME NOT NULL
);`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return errResult(err.Error()), nil
	}

	results := []string{"Created tables: users, rate_limits, mobile_tokens"}
	data := newData("user", nil)

	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"user_model.go.tmpl", "/src/app/models/User.go"},
		{"mobile_token_model.go.tmpl", "/src/app/models/MobileToken.go"},
		{"auth_handler.go.tmpl", "/src/app/handlers/auth.go"},
		{"auth_test.go.tmpl", "/src/app/handlers/auth_test.go"},
		{"logout_handler.go.tmpl", "/src/app/handlers/logout.go"},
		{"login_page.html.tmpl", "/src/app/static/pages/login.html"},
		{"login.js.tmpl", "/src/app/static/js/login.js"},
		{"mobile_auth_handler.go.tmpl", "/src/app/handlers/mobile_auth.go"},
		{"mobile_auth_test.go.tmpl", "/src/app/handlers/mobile_auth_test.go"},
	}
	for _, spec := range specs {
		if err := renderToFile(spec.tmpl, spec.out, data); err != nil {
			return errResult(err.Error()), nil
		}
		results = append(results, "Created: "+spec.out)
	}

	userModel := Model{Name: "user", Table: "users", Fields: []ModelField{
		{Name: "id", Type: "int", Nullable: false},
		{Name: "name", Type: "string", Nullable: false},
		{Name: "email", Type: "string", Nullable: false},
		{Name: "created_at", Type: "timestamp", Nullable: false},
	}}
	if err := updateManifest([]Model{userModel}, authEndpoints(), nil); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}

	results = append(results, "\nRegistered full auth — cookie (login, logout, me) and bearer (login_token, logout_token, me_token) — plus the user model in api.json + routes_gen.go.")

	return mcp.NewToolResultText(strings.Join(results, "\n") + "\n\n" + runPatternChecks()), nil
}
func handleScaffoldRegistration(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	data := newData("user", nil)
	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"register_handler.go.tmpl", "/src/app/handlers/register.go"},
		{"register_test.go.tmpl", "/src/app/handlers/register_test.go"},
		{"register_page.html.tmpl", "/src/app/static/pages/register.html"},
		{"register.js.tmpl", "/src/app/static/js/register.js"},
	}
	results := []string{}
	for _, spec := range specs {
		if err := renderToFile(spec.tmpl, spec.out, data); err != nil {
			return errResult(err.Error()), nil
		}
		results = append(results, "Created: "+spec.out)
	}

	endpoint := Endpoint{Method: "POST", Path: "/api/v1/auth/register", Handler: "RegisterPOST",
		Deps: []string{"read", "write", "cache"}, Kind: "register"}
	if err := updateManifest(nil, []Endpoint{endpoint}, nil); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}

	results = append(results, "\nRegistered registration route in api.json + routes_gen.go.")
	return mcp.NewToolResultText(strings.Join(results, "\n") + "\n\n" + runPatternChecks()), nil
}
func handleAddJSForm(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	page, _ := req.Params.Arguments["page"].(string)
	apiEndpoint, _ := req.Params.Arguments["api_endpoint"].(string)
	rawFields, _ := req.Params.Arguments["fields"].([]interface{})
	title, _ := req.Params.Arguments["title"].(string)
	submitLabel, _ := req.Params.Arguments["submit_label"].(string)
	if submitLabel == "" {
		submitLabel = "Submit"
	}
	if !isSafeIdent(page) {
		return errResult("invalid page name"), nil
	}

	// Strip the versioned API prefix so the generated form function is named
	// after the resource, not after "v1".
	endpointSlug := strings.TrimPrefix(apiEndpoint, "/api/v1/")
	endpointSlug = strings.TrimPrefix(endpointSlug, "/api/")
	endpointSlug = strings.TrimPrefix(endpointSlug, "/")
	endpointSlug = strings.Trim(endpointSlug, "/")
	formName := toPascal(endpointSlug)
	if formName == "" {
		formName = toPascal(page) + "Form"
	}

	fields := parseFields(rawFieldsToStrings(rawFields))
	data := newData(page, fields)
	data.APIEndpoint = apiEndpoint
	data.SubmitLabel = submitLabel
	data.Title = title
	data.FormName = formName

	formCode, err := renderToString("js_form.js.tmpl", data)
	if err != nil {
		return errResult(err.Error()), nil
	}

	// Try pluralized then singular JS filename
	targetPath := "/src/app/static/js/" + toPlural(page) + ".js"
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		targetPath = "/src/app/static/js/" + page + ".js"
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		return errResult("target JS file not found: " + targetPath), nil
	}

	marker := "// @inject-forms"
	if !strings.Contains(string(content), marker) {
		return errResult("marker '// @inject-forms' not found in " + targetPath + ". Re-add the marker and try again."), nil
	}

	call := "setup" + formName + "Form(document.getElementById('forms-container'));\n" + marker
	updated := strings.Replace(string(content), marker, call, 1)
	updated += "\n\n" + formCode

	if err := os.WriteFile(targetPath, []byte(updated), 0644); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText("Form injected into " + targetPath + "\n\n" + runPatternChecks()), nil
}
