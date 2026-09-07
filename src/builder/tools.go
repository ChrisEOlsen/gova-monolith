package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	// The CGO SQLite driver, registered for database/sql. It lives here rather
	// than in a test file so the shipped binary carries it.
	_ "github.com/mattn/go-sqlite3"
)

// appDir and dataDSN locate the app the builder writes into. They are variables
// rather than constants so tests can point the whole CLI at a temp tree.
var (
	appDir  = "/src/app"
	dataDSN = "file:/data/app.db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=NORMAL"
)

func manifestPath() string { return filepath.Join(appDir, "api.json") }
func handlersDir() string  { return filepath.Join(appDir, "handlers") }
func modelsDir() string    { return filepath.Join(appDir, "models") }
func pagesDir() string     { return filepath.Join(appDir, "static", "pages") }
func jsDir() string        { return filepath.Join(appDir, "static", "js") }

func inspect() (string, error) {
	var out string
	err := withWorkspaceRead(func() error {
		scan := func(dir, ext string) []string {
			files, _ := filepath.Glob(filepath.Join(dir, "*"+ext))
			names := []string{}
			for _, f := range files {
				if base := filepath.Base(f); base != ".gitkeep" {
					names = append(names, base)
				}
			}
			return names
		}
		onDisk := onDiskFiles{
			Models:   scan(modelsDir(), ".go"),
			Handlers: scan(handlersDir(), ".go"),
			Pages:    scan(pagesDir(), ".html"),
			JS:       scan(jsDir(), ".js"),
		}
		m, err := readManifestAt(manifestPath())
		if err != nil {
			return err
		}
		report := buildInspection(m, onDisk)
		report.Divergence = append(report.Divergence, generatedDivergence(handlersDir(), m)...)
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		out = string(data)
		return nil
	})
	return out, err
}

func executeSQL(query string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", errors.New("-query is required")
	}
	// DDL joins the workspace lock: several simultaneous CREATE TABLEs can
	// exhaust the busy timeout and hand a caller a SQLITE_BUSY it did nothing
	// to earn. Serializing turns contention into waiting.
	err := withWorkspaceLock(func() error {
		database, err := sql.Open("sqlite3", dataDSN)
		if err != nil {
			return err
		}
		defer database.Close()
		_, err = database.Exec(query)
		return err
	})
	if err != nil {
		return "", err
	}
	return "SQL executed successfully", nil
}

// prepareModel runs the checks every model-producing command shares: a safe
// name, safe field names, at least one field, a table that exists and matches
// the declaration, and no dangling foreign key.
func prepareModel(name string, rawFields []string, owned bool) ([]Field, error) {
	if !isSafeIdent(name) {
		return nil, errors.New("-name must be alphanumeric and underscore only")
	}
	if err := checkReservedName(name); err != nil {
		return nil, err
	}
	fields := parseFields(rawFields)
	if len(fields) == 0 {
		return nil, errors.New("-fields is required (at least one name:type pair)")
	}
	if err := checkSafeFieldNames(fields); err != nil {
		return nil, err
	}
	fields, err := applySchema(toPlural(name), fields, owned)
	if err != nil {
		return nil, err
	}
	if err := validateRefs(fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// checkSafeFieldNames holds field names to the same rule as the model name.
//
// A field name is not merely a SQL identifier here — it is interpolated into
// generated Go (a struct tag, an identifier), into generated JS (an object key)
// and into generated HTML (a label). isSafeIdent on -name alone left that whole
// path open: `gova sql` will create a column called anything at all, and the
// caller upstream of these tools is an agent that may be acting on text it was
// handed. One regex closes it for every template at once.
func checkSafeFieldNames(fields []Field) error {
	for _, f := range fields {
		if !isSafeIdent(f.Name) {
			return fmt.Errorf("field name %q must be alphanumeric and underscore only — "+
				"field names are interpolated into generated Go, JS and HTML", f.Name)
		}
		if f.Ref != "" && !isSafeIdent(f.Ref) {
			return fmt.Errorf("field %q references %q, which must be alphanumeric and underscore only",
				f.Name, f.Ref)
		}
	}
	return nil
}

func createModel(name string, rawFields []string, owned bool) (string, error) {
	fields, err := prepareModel(name, rawFields, owned)
	if err != nil {
		return "", err
	}
	data := newData(name, fields)
	data.Owned = owned

	written, err := renderAll(data, []fileSpec{
		{"model.go.tmpl", filepath.Join(modelsDir(), toPascal(name)+".go")},
		{"model_test.go.tmpl", filepath.Join(modelsDir(), toPascal(name)+"_test.go")},
	})
	if err != nil {
		return "", err
	}

	// A model registers no route, so only `models` is touched — but it does
	// register: a model missing from the manifest while endpoints reference it
	// is a hole nothing goes looking for.
	if err := updateManifest([]Model{fieldsToModel(name, toPlural(name), fields, owned)}, nil, nil); err != nil {
		return "", fmt.Errorf("manifest update failed: %w", err)
	}
	return written + "\n\nRegistered model " + name + " in api.json." + ownerNote(owned), nil
}

// ownerNote is the reminder appended to any owned scaffold's report: the
// generated methods take a user id, and nothing else can supply it.
func ownerNote(owned bool) string {
	if !owned {
		return ""
	}
	return "\nOwned: every generated query is scoped to " + OwnerColumn +
		"; the model methods take the session user id as their first argument."
}

func createHandler(name, method, path string, auth bool, summary, requestSchema, responseSchema string) (string, error) {
	if !isSafeIdent(name) {
		return "", errors.New("-name must be alphanumeric and underscore only")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case "GET", "POST", "PUT", "DELETE":
	default:
		return "", fmt.Errorf("-method must be GET, POST, PUT or DELETE, got %q", method)
	}
	if !strings.HasPrefix(path, "/api/v1/") {
		return "", errors.New("-path must start with /api/v1/")
	}
	reqSchema, err := parseBodySchemaArg(requestSchema)
	if err != nil {
		return "", fmt.Errorf("-request-schema: %w", err)
	}
	respSchema, err := parseBodySchemaArg(responseSchema)
	if err != nil {
		return "", fmt.Errorf("-response-schema: %w", err)
	}

	data := newData(name, nil)
	data.Method = method

	outPath := filepath.Join(handlersDir(), name+".go")
	if err := renderToFile("handler.go.tmpl", outPath, data); err != nil {
		return "", err
	}

	endpoint := Endpoint{
		Method: method, Path: path,
		Handler: toPascal(name) + method,
		Deps:    []string{"db", "cache"},
		Auth:    auth, Kind: "custom",
		Summary: summary, Request: reqSchema, Response: respSchema,
	}
	if err := updateManifest(nil, []Endpoint{endpoint}, nil); err != nil {
		return "", fmt.Errorf("manifest update failed: %w", err)
	}
	return "Created: " + outPath +
		"\nRegistered " + method + " " + path + " in api.json + routes_gen.go." +
		"\nImplement the TODO logic, then write a test for it.", nil
}

func createPage(file, title, path string, auth bool) (string, error) {
	if !isSafeIdent(file) {
		return "", errors.New("-file must be alphanumeric and underscore only")
	}
	if strings.TrimSpace(title) == "" {
		return "", errors.New("-title is required")
	}
	if err := validatePagePath(path); err != nil {
		return "", err
	}

	data := newData(file, nil)
	data.Title = title
	data.AuthRequired = auth

	written, err := renderAll(data, []fileSpec{
		{"page.html.tmpl", filepath.Join(pagesDir(), file+".html")},
		{"page.js.tmpl", filepath.Join(jsDir(), file+".js")},
	})
	if err != nil {
		return "", err
	}

	if err := updateManifest(nil, nil, []Page{{Path: path, File: file, Title: title, Auth: auth}}); err != nil {
		return "", fmt.Errorf("manifest update failed: %w", err)
	}
	return written +
		"\nRegistered page " + path + " in api.json + pages_gen.go." +
		"\nThe generated pageFile helper serves the shell — there is no Go handler to write." +
		"\nAdd the JSON endpoints its module calls with `gova handler`.", nil
}

// validatePagePath enforces the page namespace: a human-facing URL, never an
// API one. The exact inverse of createHandler's check, which is what makes the
// two manifest tables provably disjoint.
func validatePagePath(path string) error {
	switch {
	case !strings.HasPrefix(path, "/"):
		return errors.New("-path must start with /")
	case path == "/api" || strings.HasPrefix(path, "/api/"):
		return errors.New("-path must not be under /api/ — that namespace belongs to `gova handler`; give the page a human-facing URL like /projects")
	case strings.HasPrefix(path, "/static/"):
		return errors.New("-path must not be under /static/ — that prefix is the static file server")
	}
	return nil
}

func scaffoldResource(name string, rawFields []string, public, owned bool) (string, error) {
	if owned && public {
		return "", errors.New("-owner and -public are contradictory: an owned resource is scoped to the " +
			"session user, and a public route has no session user to scope to")
	}
	fields, err := prepareModel(name, rawFields, owned)
	if err != nil {
		return "", err
	}

	auth := !public
	data := newData(name, fields)
	data.CRUD = true
	data.Owned = owned
	data.AuthRequired = auth
	data.Title = toPascal(toPlural(name))
	plural := toPlural(name)

	written, err := renderAll(data, []fileSpec{
		{"model.go.tmpl", filepath.Join(modelsDir(), toPascal(name)+".go")},
		{"model_test.go.tmpl", filepath.Join(modelsDir(), toPascal(name)+"_test.go")},
		{"resource_handlers.go.tmpl", filepath.Join(handlersDir(), name+"_resource.go")},
		{"resource_handlers_test.go.tmpl", filepath.Join(handlersDir(), name+"_resource_test.go")},
		{"list_page.html.tmpl", filepath.Join(pagesDir(), plural+".html")},
		{"list_page.js.tmpl", filepath.Join(jsDir(), plural+".js")},
	})
	if err != nil {
		return "", err
	}

	model := fieldsToModel(name, plural, fields, owned)
	if err := updateManifest([]Model{model}, resourceEndpoints(model, auth), []Page{listPage(name, data.Title, auth)}); err != nil {
		return "", fmt.Errorf("manifest update failed: %w", err)
	}
	return written +
		"\n\nRegistered CRUD for /api/v1/" + plural + " and the page /" + plural +
		" in api.json + routes_gen.go + pages_gen.go." +
		"\nThe page includes a create form and delete buttons." +
		authNote(auth) + ownerNote(owned), nil
}

// authNote reports what the scaffold just decided about who may call it. Said
// out loud on both branches: the default is the guarded one, and a caller who
// passed -public should see that written back to them.
func authNote(auth bool) string {
	if auth {
		return "\nEndpoints and page require a signed-in caller (auth:true). " +
			"Pass -public to open them."
	}
	return "\nPUBLIC: these endpoints answer anyone, including writes. " +
		"Nothing else in the app will check for you."
}

// regen re-renders routes_gen.go, pages_gen.go and pages_gen_test.go from
// api.json, without scaffolding anything.
//
// It exists because api.json is editable by hand and the generated files were
// only ever rewritten as a side effect of a scaffold. Flipping auth:true on an
// endpoint therefore did nothing until the next unrelated `gova` command
// happened to run — the manifest said the route was guarded and the router
// still mounted it bare. This is the command that makes a hand edit take
// effect, and `gova inspect` is what tells you one is pending.
func regen() (string, error) {
	var out string
	err := withWorkspaceLock(func() error {
		m, err := readManifestAt(manifestPath())
		if err != nil {
			return err
		}
		// api.json is rewritten too, not just read. Its builder_version and
		// hash are provenance for the generated files — leaving them behind
		// after a regen would have inspect reporting a stale builder forever,
		// with no command able to clear it.
		if err := writeManifestAt(manifestPath(), &m, time.Now()); err != nil {
			return err
		}
		if err := regenerateRoutesAt(handlersDir(), m); err != nil {
			return err
		}
		if err := regeneratePagesAt(handlersDir(), m); err != nil {
			return err
		}
		out = fmt.Sprintf("Regenerated routes_gen.go, pages_gen.go and pages_gen_test.go from api.json "+
			"(%d endpoints, %d pages).\nRestart the app for it to take effect: docker compose restart app",
			len(m.Endpoints), len(m.Pages))
		return nil
	})
	return out, err
}

type fileSpec struct{ tmpl, out string }

// renderAll renders a set of files and returns the "Created: ..." report.
func renderAll(data TemplateData, specs []fileSpec) (string, error) {
	lines := make([]string, 0, len(specs))
	for _, spec := range specs {
		if err := renderToFile(spec.tmpl, spec.out, data); err != nil {
			return "", err
		}
		lines = append(lines, "Created: "+spec.out)
	}
	return strings.Join(lines, "\n"), nil
}

// listPage is the page row a resource registers for its list shell.
//
// Resource pages are always plural and the auth pages take their singular verb
// (/login, /register). Since toPlural never returns its input unchanged, a
// resource named "login" lands at /logins — so the two namespaces cannot
// collide without needing a reserved-word list to enforce it.
func listPage(name, title string, auth bool) Page {
	plural := toPlural(name)
	return Page{Path: "/" + plural, File: plural, Title: title, Auth: auth}
}

// resourceEndpoints are the five CRUD endpoints a resource registers. The
// handler symbols must match resource_handlers.go.tmpl exactly.
func resourceEndpoints(m Model, auth bool) []Endpoint {
	p := toPascal(m.Name)
	base := "/api/v1/" + toPlural(m.Name)
	deps := []string{"db", "cache"}
	mk := func(method, path, handler, kind string) Endpoint {
		return Endpoint{
			Method: method, Path: path, Handler: handler, Deps: deps,
			Model: m.Name, Kind: kind, Auth: auth,
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
