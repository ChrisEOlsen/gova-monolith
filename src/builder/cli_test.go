package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newApp points the whole CLI at a temp tree and returns a helper that runs a
// command against it. This is what the MCP protocol boundary used to make
// awkward: the six commands are now ordinary functions with a temp working
// directory, so they can be driven end to end.
func newApp(t *testing.T) func(args ...string) (string, error) {
	t.Helper()
	root := t.TempDir()

	for _, dir := range []string{"handlers", "models", "static/pages", "static/js"} {
		if err := os.MkdirAll(filepath.Join(root, "app", dir), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	dbPath := filepath.Join(root, "app.db")

	prevApp, prevDSN := appDir, dataDSN
	appDir = filepath.Join(root, "app")
	dataDSN = "file:" + dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	t.Setenv("GOVA_LOCK_PATH", filepath.Join(root, "lock"))
	t.Cleanup(func() { appDir, dataDSN = prevApp, prevDSN })

	return func(args ...string) (string, error) {
		return run(args[0], args[1:])
	}
}

func mustRun(t *testing.T, gova func(...string) (string, error), args ...string) string {
	t.Helper()
	out, err := gova(args...)
	if err != nil {
		t.Fatalf("gova %s: %v", strings.Join(args, " "), err)
	}
	return out
}

func readManifest(t *testing.T) Manifest {
	t.Helper()
	m, err := readManifestAt(manifestPath())
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return m
}

func fileExists(t *testing.T, parts ...string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(append([]string{appDir}, parts...)...))
	return err == nil
}

const projectsTable = `CREATE TABLE projects (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	status TEXT,
	priority INTEGER NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`

// The full documented workflow: table, then resource, in one pass.
func TestCLI_ResourceEndToEnd(t *testing.T) {
	gova := newApp(t)

	mustRun(t, gova, "sql", "-query", projectsTable)
	out := mustRun(t, gova, "resource", "-name", "project", "-fields", "name:string,status:string,priority:int")

	for _, f := range [][]string{
		{"models", "Project.go"}, {"models", "Project_test.go"},
		{"handlers", "project_resource.go"}, {"handlers", "project_resource_test.go"},
		{"static", "pages", "projects.html"}, {"static", "js", "projects.js"},
		{"handlers", "routes_gen.go"}, {"handlers", "pages_gen.go"}, {"handlers", "pages_gen_test.go"},
	} {
		if !fileExists(t, f...) {
			t.Errorf("expected %s to be written", filepath.Join(f...))
		}
	}
	if !strings.Contains(out, "Registered CRUD") {
		t.Errorf("report did not mention registration: %s", out)
	}

	m := readManifest(t)
	if len(m.Endpoints) != 5 {
		t.Fatalf("want 5 endpoints, got %d", len(m.Endpoints))
	}
	kinds := map[string]bool{}
	for _, e := range m.Endpoints {
		kinds[e.Kind] = true
		if e.Model != "project" {
			t.Errorf("%s %s: model %q", e.Method, e.Path, e.Model)
		}
	}
	for _, k := range []string{"list", "detail", "create", "update", "delete"} {
		if !kinds[k] {
			t.Errorf("manifest is missing the %q endpoint", k)
		}
	}
	if len(m.Pages) != 1 || m.Pages[0].Path != "/projects" {
		t.Errorf("pages = %+v, want one at /projects", m.Pages)
	}
	if m.BuilderVersion != builderVersion {
		t.Errorf("builder_version = %q, want %q", m.BuilderVersion, builderVersion)
	}

	// The generated routes file must actually name the handlers.
	routes, _ := os.ReadFile(filepath.Join(handlersDir(), "routes_gen.go"))
	if !strings.Contains(string(routes), "ProjectListGET(database, appCache)") {
		t.Errorf("routes_gen.go does not mount the list handler:\n%s", routes)
	}
}

func TestCLI_ResourceRequiresItsTable(t *testing.T) {
	gova := newApp(t)
	_, err := gova("resource", "-name", "project", "-fields", "name:string")
	if err == nil {
		t.Fatal("scaffolding without a table must fail")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should name the missing table, got: %v", err)
	}
}

// The declaration is checked against the real table, so a model cannot lie
// about the data.
func TestCLI_ResourceRejectsFieldMismatch(t *testing.T) {
	gova := newApp(t)
	mustRun(t, gova, "sql", "-query", projectsTable)

	if _, err := gova("resource", "-name", "project", "-fields", "nope:string"); err == nil {
		t.Error("a field that is not a column must fail")
	}
	if _, err := gova("resource", "-name", "project", "-fields", "priority:string"); err == nil {
		t.Error("a type that disagrees with the column must fail")
	}
	if _, err := gova("resource", "-name", "project", "-fields", "name:strng"); err == nil {
		t.Error("an unknown field type must fail, not silently become string")
	}
}

// Scaffolding generates generic CRUD, where a request that omits a field
// overwrites it — so it must never see a credential column.
func TestCLI_RefusesCredentialColumns(t *testing.T) {
	gova := newApp(t)
	mustRun(t, gova, "sql", "-query", `CREATE TABLE accounts (
		id INTEGER PRIMARY KEY,
		nickname TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)

	_, err := gova("resource", "-name", "account", "-fields", "nickname:string,password_hash:string")
	if err == nil {
		t.Fatal("a credential column must be refused")
	}
	if !strings.Contains(err.Error(), "credential") {
		t.Errorf("error should explain why, got: %v", err)
	}
	if fileExists(t, "models", "Account.go") {
		t.Error("a refused scaffold must write nothing")
	}
}

func TestCLI_ReservedNames(t *testing.T) {
	gova := newApp(t)
	for _, name := range []string{"user", "time", "mobile_token"} {
		if _, err := gova("resource", "-name", name, "-fields", "x:string"); err == nil {
			t.Errorf("%q is owned by the template and must be refused", name)
		}
	}
}

func TestCLI_Model(t *testing.T) {
	gova := newApp(t)
	mustRun(t, gova, "sql", "-query", projectsTable)
	mustRun(t, gova, "model", "-name", "project", "-fields", "name:string")

	if !fileExists(t, "models", "Project.go") {
		t.Error("model file not written")
	}
	m := readManifest(t)
	if len(m.Models) != 1 || m.Models[0].Name != "project" {
		t.Errorf("models = %+v", m.Models)
	}
	// A model registers no route.
	if len(m.Endpoints) != 0 || len(m.Pages) != 0 {
		t.Errorf("model registered a route or page: %d endpoints, %d pages", len(m.Endpoints), len(m.Pages))
	}
}

func TestCLI_Page(t *testing.T) {
	gova := newApp(t)
	mustRun(t, gova, "page", "-file", "dashboard", "-title", "Dashboard", "-path", "/dashboard")

	if !fileExists(t, "static", "pages", "dashboard.html") || !fileExists(t, "static", "js", "dashboard.js") {
		t.Error("page files not written")
	}
	m := readManifest(t)
	if len(m.Pages) != 1 || m.Pages[0].Path != "/dashboard" {
		t.Fatalf("pages = %+v", m.Pages)
	}
	pages, _ := os.ReadFile(filepath.Join(handlersDir(), "pages_gen.go"))
	if !strings.Contains(string(pages), `r.Get("/dashboard", pageFile("dashboard"))`) {
		t.Errorf("pages_gen.go does not mount the page:\n%s", pages)
	}
}

// The two namespaces are provably disjoint: a page may not live under /api/,
// and a handler must.
func TestCLI_NamespacesAreDisjoint(t *testing.T) {
	gova := newApp(t)
	for _, path := range []string{"/api/v1/things", "/api", "/static/x", "no-slash"} {
		if _, err := gova("page", "-file", "x", "-title", "X", "-path", path); err == nil {
			t.Errorf("page at %q must be refused", path)
		}
	}
	if _, err := gova("handler", "-name", "x", "-method", "GET", "-path", "/dashboard"); err == nil {
		t.Error("a handler outside /api/v1/ must be refused")
	}
}

func TestCLI_Handler(t *testing.T) {
	gova := newApp(t)
	mustRun(t, gova, "handler",
		"-name", "archive", "-method", "post", "-path", "/api/v1/projects/{id}/archive",
		"-auth", "-summary", "Archive a project",
		"-response-schema", `{"shape":"object","fields":[{"name":"ok","type":"boolean"}]}`)

	if !fileExists(t, "handlers", "archive.go") {
		t.Fatal("handler file not written")
	}
	m := readManifest(t)
	if len(m.Endpoints) != 1 {
		t.Fatalf("endpoints = %+v", m.Endpoints)
	}
	e := m.Endpoints[0]
	if e.Method != "POST" {
		t.Errorf("method = %q, want POST (a lowercase flag must be normalized)", e.Method)
	}
	if e.Handler != "ArchivePOST" || e.Kind != "custom" || !e.Auth {
		t.Errorf("endpoint = %+v", e)
	}
	if e.Summary != "Archive a project" || e.Response == nil || e.Response.Fields[0].Name != "ok" {
		t.Errorf("schema/summary lost: %+v", e)
	}
	// auth:true must produce the RequireAuth wrap.
	routes, _ := os.ReadFile(filepath.Join(handlersDir(), "routes_gen.go"))
	if !strings.Contains(string(routes), "middleware.RequireAuth") {
		t.Errorf("auth endpoint was not wrapped:\n%s", routes)
	}
}

func TestCLI_HandlerRejectsBadInput(t *testing.T) {
	gova := newApp(t)
	if _, err := gova("handler", "-name", "x", "-method", "FETCH", "-path", "/api/v1/x"); err == nil {
		t.Error("an unknown HTTP method must be refused")
	}
	if _, err := gova("handler", "-name", "x", "-method", "GET", "-path", "/api/v1/x",
		"-request-schema", `{"shape":"weird"}`); err == nil {
		t.Error("an unknown schema shape must be refused")
	}
	if _, err := gova("handler", "-name", "bad name", "-method", "GET", "-path", "/api/v1/x"); err == nil {
		t.Error("an unsafe handler name must be refused")
	}
}

func TestCLI_Inspect(t *testing.T) {
	gova := newApp(t)
	mustRun(t, gova, "sql", "-query", projectsTable)
	mustRun(t, gova, "resource", "-name", "project", "-fields", "name:string")

	var rep inspection
	if err := json.Unmarshal([]byte(mustRun(t, gova, "inspect")), &rep); err != nil {
		t.Fatalf("inspect output is not JSON: %v", err)
	}
	if len(rep.Divergence) != 0 {
		t.Errorf("a freshly scaffolded app should not diverge: %v", rep.Divergence)
	}
	if rep.BuilderVersion != builderVersion {
		t.Errorf("builder_version = %q", rep.BuilderVersion)
	}

	// Delete the model file and inspect must notice.
	os.Remove(filepath.Join(modelsDir(), "Project.go"))
	json.Unmarshal([]byte(mustRun(t, gova, "inspect")), &rep)
	if len(rep.Divergence) == 0 {
		t.Error("a missing model file should read as divergence")
	}
}

func TestCLI_SQLErrorsAreReported(t *testing.T) {
	gova := newApp(t)
	if _, err := gova("sql", "-query", "NOT VALID SQL"); err == nil {
		t.Error("invalid SQL must fail")
	}
	if _, err := gova("sql"); err == nil {
		t.Error("a missing -query must fail")
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	gova := newApp(t)
	_, err := gova("scaffold_auth")
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("want an unknown-command error, got %v", err)
	}
}

func TestCLI_Version(t *testing.T) {
	gova := newApp(t)
	if out := mustRun(t, gova, "version"); out != builderVersion {
		t.Errorf("version = %q, want %q", out, builderVersion)
	}
}

func TestSplitFields(t *testing.T) {
	got := splitFields(" name:string , status:string ,")
	want := []string{"name:string", "status:string"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	if len(splitFields("")) != 0 {
		t.Error("an empty string should yield no fields")
	}
}

// Two commands writing in sequence must both survive: the second read must see
// the first's registration, which is what the workspace lock guarantees.
func TestCLI_RegistrationsAccumulate(t *testing.T) {
	gova := newApp(t)
	mustRun(t, gova, "sql", "-query", projectsTable)
	mustRun(t, gova, "sql", "-query", `CREATE TABLE notes (
		id INTEGER PRIMARY KEY, body TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)

	mustRun(t, gova, "resource", "-name", "project", "-fields", "name:string")
	mustRun(t, gova, "resource", "-name", "note", "-fields", "body:string")
	mustRun(t, gova, "page", "-file", "dashboard", "-title", "Dashboard", "-path", "/dashboard")

	m := readManifest(t)
	if len(m.Models) != 2 || len(m.Endpoints) != 10 || len(m.Pages) != 3 {
		t.Fatalf("lost a registration: %d models, %d endpoints, %d pages",
			len(m.Models), len(m.Endpoints), len(m.Pages))
	}
	routes, _ := os.ReadFile(filepath.Join(handlersDir(), "routes_gen.go"))
	for _, want := range []string{"ProjectListGET", "NoteListGET"} {
		if !strings.Contains(string(routes), want) {
			t.Errorf("routes_gen.go is missing %s", want)
		}
	}
}

// A foreign key may not name a model that has not been scaffolded.
func TestCLI_DanglingReferenceRefused(t *testing.T) {
	gova := newApp(t)
	mustRun(t, gova, "sql", "-query", `CREATE TABLE tasks (
		id INTEGER PRIMARY KEY, title TEXT NOT NULL, project_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)

	if _, err := gova("resource", "-name", "task", "-fields", "title:string,project_id:ref:project"); err == nil {
		t.Fatal("a reference to an unscaffolded model must be refused")
	}

	mustRun(t, gova, "sql", "-query", projectsTable)
	mustRun(t, gova, "resource", "-name", "project", "-fields", "name:string")
	mustRun(t, gova, "resource", "-name", "task", "-fields", "title:string,project_id:ref:project")

	// The reference must reach the manifest — gova-ios reads it to decide that
	// task nests under project rather than becoming its own tab.
	m := readManifest(t)
	for _, model := range m.Models {
		if model.Name != "task" {
			continue
		}
		for _, f := range model.Fields {
			if f.Name == "project_id" && f.References != "project" {
				t.Errorf("project_id references %q, want project", f.References)
			}
		}
	}
}

// The tables the app's own schema owns must exist before scaffolding runs
// against the same database.
func TestCLI_SQLPersistsAcrossInvocations(t *testing.T) {
	gova := newApp(t)
	mustRun(t, gova, "sql", "-query", projectsTable)

	database, err := sql.Open("sqlite3", dataDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var name string
	if err := database.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='projects'").Scan(&name); err != nil {
		t.Fatalf("table did not persist: %v", err)
	}
}
