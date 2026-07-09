package main

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed templates/*
var templateFS embed.FS

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
	"goType": func(t string) string {
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
	},
	"joinNames": func(fields []Field) string {
		names := make([]string, len(fields))
		for i, f := range fields {
			names[i] = f.Name
		}
		return strings.Join(names, ", ")
	},
	"scanFields": func(fields []Field, prefix string) string {
		refs := make([]string, len(fields))
		for i, f := range fields {
			refs[i] = prefix + toPascal(f.Name)
		}
		return strings.Join(refs, ", ")
	},
	"placeholders": func(fields []Field) string {
		p := make([]string, len(fields))
		for i := range fields {
			p[i] = "?"
		}
		return strings.Join(p, ", ")
	},
	"createParams": func(fields []Field) string {
		params := make([]string, len(fields))
		for i, f := range fields {
			goT := "string"
			switch f.Type {
			case "int":
				goT = "int64"
			case "boolean":
				goT = "bool"
			case "float":
				goT = "float64"
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
}

func parseFields(raw []string) []Field {
	fields := make([]Field, 0, len(raw))
	for _, f := range raw {
		parts := strings.SplitN(f, ":", 2)
		if len(parts) == 2 {
			fields = append(fields, Field{Name: parts[0], Type: parts[1]})
		} else {
			fields = append(fields, Field{Name: parts[0], Type: "string"})
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
		mcp.WithDescription("Generate models/Name.go with GetAll/Find/Create/Update/Delete and 5-min cache. Table must exist first (run execute_sql)."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Model name in snake_case")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
	), handleCreateModel)

	s.AddTool(mcp.NewTool("create_handler",
		mcp.WithDescription("Generate a single JSON handler stub in handlers/name.go. Implement the TODO logic. Wire route in main.go after."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Handler name in snake_case")),
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method: GET, POST, PUT, DELETE")),
		mcp.WithBoolean("auth_required", mcp.Description("Inject auth guard — returns JSON 401 if unauthenticated")),
	), handleCreateHandler)

	s.AddTool(mcp.NewTool("create_page",
		mcp.WithDescription("Generate: static/pages/filename.html + static/js/filename.js + handlers/filename.go stub. After: add forms with add_js_form, wire route in main.go."),
		mcp.WithString("filename", mcp.Required(), mcp.Description("Page filename without extension")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Page title")),
		mcp.WithBoolean("auth_required", mcp.Description("JS module calls requireAuth() on load")),
	), handleCreatePage)

	s.AddTool(mcp.NewTool("scaffold_list",
		mcp.WithDescription("Generate 4 files: model + JSON list handler + HTML shell + JS module. After: add forms with add_js_form, wire route in main.go."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Resource name in snake_case")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
	), handleScaffoldList)

	s.AddTool(mcp.NewTool("scaffold_auth",
		mcp.WithDescription("Generate full auth system: users + rate_limits tables, User model, login/logout/me JSON handlers and HTML pages. Wire 5 routes in main.go (printed in output)."),
	), handleScaffoldAuth)

	s.AddTool(mcp.NewTool("scaffold_registration",
		mcp.WithDescription("Generate registration JSON handler + HTML page. Run after scaffold_auth. Wire 2 routes in main.go (printed in output)."),
	), handleScaffoldRegistration)

	s.AddTool(mcp.NewTool("add_js_form",
		mcp.WithDescription("Inject a creation form into an existing JS module at the // @inject-forms marker. The form uses api.js for submission. Requires: (1) JS file exists with the marker, (2) a POST handler exists at api_endpoint."),
		mcp.WithString("page", mcp.Required(), mcp.Description("Target page filename without extension")),
		mcp.WithString("api_endpoint", mcp.Required(), mcp.Description("API endpoint the form POSTs to")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
		mcp.WithString("title", mcp.Description("Optional form section title")),
		mcp.WithString("submit_label", mcp.Description("Submit button label (default: Submit)")),
	), handleAddJSForm)

	s.AddTool(mcp.NewTool("scaffold_mobile_auth",
		mcp.WithDescription("Add token-based auth endpoints to the Go API for mobile clients (iOS, Android). Idempotent — safe to call from multiple mobile repos. Creates mobile_tokens table and handlers/mobile_auth.go with MobileLoginPOST, MobileLogoutDELETE, MobileMeGET. Requires scaffold_auth to have been run first (users table must exist)."),
	), handleScaffoldMobileAuth)

	if err := server.ServeStdio(s); err != nil {
		log.Fatal(err)
	}
}

// Tool handler stubs — implemented in subsequent tasks
func handleInspectApp(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	listDir := func(pattern, label string) string {
		files, _ := filepath.Glob(pattern)
		names := make([]string, 0, len(files))
		for _, f := range files {
			base := filepath.Base(f)
			if base == ".gitkeep" {
				continue
			}
			names = append(names, "  "+base)
		}
		if len(names) == 0 {
			return label + "\n  (none)"
		}
		return label + "\n" + strings.Join(names, "\n")
	}

	sections := []string{
		listDir("/src/app/models/*.go", "Models:"),
		listDir("/src/app/handlers/*.go", "Handlers:"),
		listDir("/src/app/static/pages/*.html", "Pages (HTML):"),
		listDir("/src/app/static/js/*.js", "Pages (JS):"),
	}

	mainContent, err := os.ReadFile("/src/app/main.go")
	if err == nil {
		routeRe := regexp.MustCompile(`r\.(Get|Post|Put|Delete|Patch)\("([^"]+)"`)
		matches := routeRe.FindAllStringSubmatch(string(mainContent), -1)
		routes := make([]string, 0, len(matches))
		for _, m := range matches {
			routes = append(routes, "  "+m[1]+" "+m[2])
		}
		if len(routes) == 0 {
			sections = append(sections, "Routes (main.go):\n  (none registered)")
		} else {
			sections = append(sections, "Routes (main.go):\n"+strings.Join(routes, "\n"))
		}
	}

	return mcp.NewToolResultText(strings.Join(sections, "\n\n")), nil
}
func handleExecuteSQL(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, _ := req.Params.Arguments["query"].(string)
	if query == "" {
		return errResult("query is required"), nil
	}
	db, err := sql.Open("sqlite3", "/data/app.db?_foreign_keys=on")
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
	data := newData(name, fields)

	outPath := "/src/app/models/" + toPascal(name) + ".go"
	if err := renderToFile("model.go.tmpl", outPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText("Created: " + outPath), nil
}
func handleCreateHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, _ := req.Params.Arguments["name"].(string)
	method, _ := req.Params.Arguments["method"].(string)
	authRequired, _ := req.Params.Arguments["auth_required"].(bool)
	if !isSafeIdent(name) {
		return errResult("invalid handler name"), nil
	}
	data := newData(name, nil)
	data.Method = strings.ToUpper(method)
	data.AuthRequired = authRequired

	outPath := "/src/app/handlers/" + name + ".go"
	if err := renderToFile("handler.go.tmpl", outPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText("Created: " + outPath + "\n\nImplement the TODO logic. Wire route in main.go.\n\n" + runPatternChecks()), nil
}
func handleCreatePage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filename, _ := req.Params.Arguments["filename"].(string)
	title, _ := req.Params.Arguments["title"].(string)
	authRequired, _ := req.Params.Arguments["auth_required"].(bool)
	if !isSafeIdent(filename) {
		return errResult("invalid filename"), nil
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
	handlerPath := "/src/app/handlers/" + filename + ".go"
	if err := renderToFile("handler.go.tmpl", handlerPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText(
		"Created: " + htmlPath + "\nCreated: " + jsPath + "\nCreated: " + handlerPath +
			"\n\nNext: wire route in main.go. Add forms with add_js_form.\n\n" + runPatternChecks(),
	), nil
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
	data := newData(name, fields)
	data.Title = toPascal(toPlural(name))

	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"model.go.tmpl", "/src/app/models/" + toPascal(name) + ".go"},
		{"list_handler.go.tmpl", "/src/app/handlers/" + name + "_list.go"},
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
	return mcp.NewToolResultText(
		strings.Join(results, "\n") +
			"\n\nNext: wire GET route in main.go, add POST handler with create_handler, add form with add_js_form.\n\n" +
			runPatternChecks(),
	), nil
}
func handleScaffoldAuth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	db, err := sql.Open("sqlite3", "/data/app.db?_foreign_keys=on")
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
);`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return errResult(err.Error()), nil
	}

	results := []string{"Created tables: users, rate_limits"}
	data := newData("user", nil)

	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"user_model.go.tmpl", "/src/app/models/User.go"},
		{"auth_handler.go.tmpl", "/src/app/handlers/auth.go"},
		{"logout_handler.go.tmpl", "/src/app/handlers/logout.go"},
		{"login_page.html.tmpl", "/src/app/static/pages/login.html"},
		{"login.js.tmpl", "/src/app/static/js/login.js"},
	}
	for _, spec := range specs {
		if err := renderToFile(spec.tmpl, spec.out, data); err != nil {
			return errResult(err.Error()), nil
		}
		results = append(results, "Created: "+spec.out)
	}
	results = append(results, "\nRegister routes in main.go:\n"+
		"  r.Post(\"/api/auth/login\",  handlers.LoginPOST(database.Read, database.Write, appCache))\n"+
		"  r.Post(\"/api/auth/logout\", handlers.LogoutPOST())\n"+
		"  r.Get(\"/api/auth/me\",      handlers.MeGET(database.Read, database.Write, appCache))")

	return mcp.NewToolResultText(strings.Join(results, "\n") + "\n\n" + runPatternChecks()), nil
}
func handleScaffoldRegistration(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	data := newData("user", nil)
	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"register_handler.go.tmpl", "/src/app/handlers/register.go"},
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
	results = append(results, "\nAdd routes in main.go:\n"+
		"  r.Post(\"/api/auth/register\", handlers.RegisterPOST(database.Read, database.Write, appCache))")
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

	endpointSlug := strings.TrimPrefix(apiEndpoint, "/api/")
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
func handleScaffoldMobileAuth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Step 1: Create mobile_tokens table (idempotent — IF NOT EXISTS)
	db, err := sql.Open("sqlite3", "/data/app.db?_foreign_keys=on")
	if err != nil {
		return errResult(err.Error()), nil
	}
	defer db.Close()

	ddl := `CREATE TABLE IF NOT EXISTS mobile_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	expires_at DATETIME NOT NULL
);`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return errResult("SQL failed: " + err.Error()), nil
	}

	results := []string{"Table: mobile_tokens (created or already existed)"}

	// Step 2: Generate handler file — skip if already exists (idempotent)
	outPath := "/src/app/handlers/mobile_auth.go"
	if _, statErr := os.Stat(outPath); statErr == nil {
		results = append(results, "handlers/mobile_auth.go already exists — skipping (idempotent)")
		return mcp.NewToolResultText(strings.Join(results, "\n") + mobileAuthRouteInstructions()), nil
	}

	if err := renderToFile("mobile_auth_handler.go.tmpl", outPath, TemplateData{}); err != nil {
		return errResult(err.Error()), nil
	}
	results = append(results, "Created: "+outPath)

	return mcp.NewToolResultText(strings.Join(results, "\n") + mobileAuthRouteInstructions() + "\n\n" + runPatternChecks()), nil
}

func mobileAuthRouteInstructions() string {
	return `

Register routes in main.go (check for duplicates before adding):
  r.Post("/api/auth/login_token",    handlers.MobileLoginPOST(database.Read, database.Write, appCache))
  r.Delete("/api/auth/logout_token", handlers.MobileLogoutDELETE(database.Write))
  r.Get("/api/auth/me_token",        handlers.MobileMeGET(database.Read, database.Write, appCache))

Web cookie auth is untouched. Mobile clients use Bearer token headers instead of cookies.`
}
