package main

import (
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// parseAsGo parses src as Go source, failing the test if it is not valid.
// Unlike renderAndParse, this renders a raw string rather than a named
// template, for callers (like renderRoutes) that don't take TemplateData.
func parseAsGo(t *testing.T, name, src string) {
	t.Helper()
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, name, src, parser.AllErrors); err != nil {
		t.Fatalf("%s is not valid Go: %v\n---\n%s", name, err, src)
	}
}

// renderAndParse renders tmplName with data and verifies the output is
// syntactically valid Go. It does not type-check or resolve imports — full
// compilation is checked once, end-to-end, in Task 10.
func renderAndParse(t *testing.T, tmplName string, data TemplateData) string {
	t.Helper()
	out, err := renderToString(tmplName, data)
	if err != nil {
		t.Fatalf("render %s: %v", tmplName, err)
	}
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, tmplName, out, parser.AllErrors); err != nil {
		t.Fatalf("render %s: output is not valid Go: %v\n---\n%s", tmplName, err, out)
	}
	return out
}

func sampleFields() []Field {
	return []Field{
		{Name: "title", Type: "string"},
		{Name: "count", Type: "int"},
		{Name: "active", Type: "boolean"},
	}
}

func TestRenderAndParse_ExistingTemplateIsValidGo(t *testing.T) {
	data := newData("widget", sampleFields())
	renderAndParse(t, "model.go.tmpl", data)
}

func TestModelTestTemplate_IsValidGo(t *testing.T) {
	data := newData("widget", sampleFields())
	renderAndParse(t, "model_test.go.tmpl", data)
}

func sampleFieldsWithNullable() []Field {
	return []Field{
		{Name: "title", Type: "string", Nullable: false},
		{Name: "notes", Type: "string", Nullable: true},
		{Name: "count", Type: "int", Nullable: false},
		{Name: "score", Type: "int", Nullable: true},
	}
}

func TestModelTemplate_NullableFieldIsPointer(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", data)

	if !strings.Contains(out, "Notes *string `json:\"notes\"`") {
		t.Errorf("nullable field is not a pointer:\n%s", out)
	}
	if !strings.Contains(out, "Title string `json:\"title\"`") {
		t.Errorf("non-nullable field should not be a pointer:\n%s", out)
	}
	if !strings.Contains(out, "var notesNull sql.NullString") {
		t.Errorf("missing sql.NullString temporary:\n%s", out)
	}
	if !strings.Contains(out, "item.Notes = &notesNull.String") {
		t.Errorf("missing nullable assignment:\n%s", out)
	}

	// A nullable non-string field must route through its own sql.Null*
	// wrapper and accessor — a typo in nullTypeFor/nullFieldFor's int
	// branch would otherwise ship uncaught, since the only other nullable
	// sample field is a string.
	if !strings.Contains(out, "Score *int64 `json:\"score\"`") {
		t.Errorf("nullable int field is not a *int64 pointer:\n%s", out)
	}
	if !strings.Contains(out, "var scoreNull sql.NullInt64") {
		t.Errorf("missing sql.NullInt64 temporary:\n%s", out)
	}
	if !strings.Contains(out, "item.Score = &scoreNull.Int64") {
		t.Errorf("missing nullable int assignment:\n%s", out)
	}
}

func TestModelTemplate_UsesGovaTime(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", data)

	if !strings.Contains(out, "CreatedAt Time `json:\"created_at\"`") {
		t.Errorf("CreatedAt should use models.Time:\n%s", out)
	}
}

func TestModelTemplate_GetPageReplacesGetAll(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", data)

	if !strings.Contains(out, "func (m *WidgetModel) GetPage(limit, offset int, opts QueryOpts) ([]Widget, int, error)") {
		t.Errorf("missing GetPage signature:\n%s", out)
	}
	if strings.Contains(out, "func (m *WidgetModel) GetAll(") {
		t.Errorf("GetAll should be gone:\n%s", out)
	}
	if !strings.Contains(out, "items := []Widget{}") {
		t.Errorf("slice must be initialized non-nil:\n%s", out)
	}
	if !strings.Contains(out, "SELECT COUNT(*) FROM widgets") {
		t.Errorf("missing total count query:\n%s", out)
	}
}

func TestModelTemplate_CreateTakesPointerForNullable(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", data)

	if !strings.Contains(out, "Create(title string, notes *string, count int64, score *int64)") {
		t.Errorf("Create should take a pointer for the nullable field:\n%s", out)
	}
}

func TestModelTestTemplate_NullableIsValidGo(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	renderAndParse(t, "model_test.go.tmpl", data)
}

// TestUserModelTemplate_RateLimitBucketDecays guards the sliding-window reset
// in RecordFailedAttempt. Without it the limiter is a lifetime quota and any IP
// that ever accumulates 5 failures is capped at one attempt per 15 minutes for
// good — an availability failure on a shared NAT, not a nuisance.
func TestHandlerTemplate_NoInlineAuthCheck(t *testing.T) {
	data := newData("archive_project", nil)
	data.Method = "POST"
	data.AuthRequired = true
	out := renderAndParse(t, "handler.go.tmpl", data)
	if strings.Contains(out, "middleware.UserID") {
		t.Errorf("inline auth check should be gone (RequireAuth wrap enforces it):\n%s", out)
	}
	if strings.Contains(out, `"gova/app/middleware"`) {
		t.Errorf("handler template should no longer import middleware:\n%s", out)
	}
}

func routeManifest(endpoints ...Endpoint) Manifest {
	m := Manifest{APIVersion: "1.0.0"}
	for _, e := range endpoints {
		_ = m.UpsertEndpoint(e)
	}
	m.canonicalize()
	return m
}

func TestRenderRoutes_EmptyIsValidGoNoMiddleware(t *testing.T) {
	out, err := renderRoutes(routeManifest())
	if err != nil {
		t.Fatalf("renderRoutes: %v", err)
	}
	parseAsGo(t, "routes_gen.go", out)
	if strings.Contains(out, "middleware") {
		t.Errorf("empty route set must not import middleware:\n%s", out)
	}
	if !strings.Contains(out, "func RegisterGenerated(r chi.Router, database *db.DB, appCache *cache.Cache)") {
		t.Errorf("missing RegisterGenerated signature:\n%s", out)
	}
}

func TestRenderRoutes_DepsAndMethods(t *testing.T) {
	out, err := renderRoutes(routeManifest(
		Endpoint{Method: "GET", Path: "/api/v1/projects", Handler: "ProjectListGET",
			Deps: []string{"db", "cache"}, Kind: "list"},
		Endpoint{Method: "DELETE", Path: "/api/v1/auth/logout_token", Handler: "MobileLogoutDELETE",
			Deps: []string{"db"}, Auth: true, Kind: "mobile_logout"},
		Endpoint{Method: "POST", Path: "/api/v1/auth/logout", Handler: "LogoutPOST",
			Deps: []string{}, Kind: "auth_logout"},
	))
	if err != nil {
		t.Fatalf("renderRoutes: %v", err)
	}
	parseAsGo(t, "routes_gen.go", out)
	want := []string{
		`r.Get("/api/v1/projects", ProjectListGET(database, appCache))`,
		`r.With(middleware.RequireAuth).Delete("/api/v1/auth/logout_token", MobileLogoutDELETE(database))`,
		`r.Post("/api/v1/auth/logout", LogoutPOST())`,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing route line:\n  want: %s\n  in:\n%s", w, out)
		}
	}
	if !strings.Contains(out, `"gova/app/middleware"`) {
		t.Errorf("auth route present but middleware not imported:\n%s", out)
	}
}

func TestRenderRoutes_Deterministic(t *testing.T) {
	e1 := Endpoint{Method: "GET", Path: "/api/v1/a", Handler: "AGet", Deps: []string{"db"}, Kind: "list"}
	e2 := Endpoint{Method: "GET", Path: "/api/v1/b", Handler: "BGet", Deps: []string{"db"}, Kind: "list"}
	out1, _ := renderRoutes(routeManifest(e1, e2))
	out2, _ := renderRoutes(routeManifest(e2, e1))
	if out1 != out2 {
		t.Errorf("route render depends on insertion order:\n---1---\n%s\n---2---\n%s", out1, out2)
	}
}

// TestRenderRoutes_MobileBearerNotWrapped guards against regressing the
// mobile bearer-token endpoints back under the RequireAuth session wrap.
// Mobile clients authenticate via Authorization: Bearer <token> and send no
// gova_session cookie, so RequireAuth (which only checks the session-derived
// UserID) would 401 them before the handler's own bearer-token check ever
// runs. The manifest registers MobileMeGET/MobileLogoutDELETE
// with Auth:false for exactly this reason — this test proves renderRoutes
// respects that and doesn't add the wrap, while still confirming a genuine
// Auth:true endpoint DOES get wrapped (so the test would catch a regression
// in either direction).
func pageManifest(pages ...Page) Manifest {
	m := Manifest{APIVersion: "1.0.0"}
	for _, p := range pages {
		_ = m.UpsertPage(p)
	}
	m.canonicalize()
	return m
}

func TestRenderPages_EmptyIsValidGo(t *testing.T) {
	out, err := renderPages(pageManifest())
	if err != nil {
		t.Fatalf("renderPages: %v", err)
	}
	parseAsGo(t, "pages_gen.go", out)
	if !strings.Contains(out, "func RegisterPages(r chi.Router)") {
		t.Errorf("missing RegisterPages signature:\n%s", out)
	}
}

func TestRenderPages_MountsEachPage(t *testing.T) {
	out, err := renderPages(pageManifest(
		Page{Path: "/login", File: "login", Title: "Log In"},
		Page{Path: "/projects", File: "projects", Title: "Projects", Auth: true},
	))
	if err != nil {
		t.Fatalf("renderPages: %v", err)
	}
	parseAsGo(t, "pages_gen.go", out)
	for _, want := range []string{
		`r.Get("/login", pageFile("login"))`,
		// A PAGE'S auth:true MUST RENDER SOMETHING.
		//
		// It used to render nothing at all: the flag was written into api.json,
		// read by nobody, and looked exactly like a security control. The
		// version of this test that stood here asserted the INERTNESS — "page
		// routes must not be wrapped in middleware" — and its comment gave a
		// correct reason for half of it (a browser must not be handed a JSON
		// 401) and then over-concluded to "no middleware at all". That is why
		// the defect survived review: the guard read as already-considered.
		//
		// RequirePageAuth is the page-shaped answer — a 303 to /login, which is
		// what the flag reads as and what a browser can act on.
		`r.With(middleware.RequirePageAuth).Get("/projects", pageFile("projects"))`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing page line:\n  want: %s\n  in:\n%s", want, out)
		}
	}
	// Pages are never mounted under the API prefix, and are never wrapped in
	// RequireAuth — that one writes a JSON body, which a browser navigating to
	// a page must not receive.
	//
	// The guard here is a courtesy, not a boundary: the shell is inert and every
	// datum on the page comes from an /api/v1/ endpoint, so THOSE are what carry
	// auth:true in the endpoint table. What it buys is the removal of the flash
	// — without it a signed-out visitor renders the whole page and is redirected
	// only once its JS module has loaded and called requireAuth().
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "pageFile(") {
			continue
		}
		if strings.Contains(line, `"/api/`) {
			t.Errorf("a page must never be mounted under the API prefix: %s", line)
		}
		if strings.Contains(line, "middleware.RequireAuth)") {
			t.Errorf("a page must not be wrapped in the JSON RequireAuth: %s", line)
		}
	}
}

// TestRenderPages_NoAuthPageMeansNoMiddlewareImport — Go does not compile an
// unused import, so a project with no guarded page must not get one.
func TestRenderPages_NoAuthPageMeansNoMiddlewareImport(t *testing.T) {
	out, err := renderPages(pageManifest(
		Page{Path: "/login", File: "login", Title: "Log In"},
	))
	if err != nil {
		t.Fatalf("renderPages: %v", err)
	}
	parseAsGo(t, "pages_gen.go", out)
	if strings.Contains(out, `"gova/app/middleware"`) {
		t.Errorf("no page is guarded, so the import must be absent:\n%s", out)
	}
}

// TestRenderPages_ServesByGeneratedFileName pins the two properties that keep
// page serving safe: the file name comes from the generated table (a Go string
// literal, never request input), and filepath.Base is a second guard inside the
// helper.
func TestRenderPages_ServesByGeneratedFileName(t *testing.T) {
	out, err := renderPages(pageManifest(Page{Path: "/projects", File: "projects"}))
	if err != nil {
		t.Fatalf("renderPages: %v", err)
	}
	if !strings.Contains(out, `"./static/pages/" + filepath.Base(name) + ".html"`) {
		t.Errorf("pageFile must derive its path via filepath.Base:\n%s", out)
	}
	if strings.Contains(out, "chi.URLParam") || strings.Contains(out, "r.URL.Path") {
		t.Errorf("pageFile must never build a path from request input:\n%s", out)
	}
}

func TestRenderPages_Deterministic(t *testing.T) {
	a := Page{Path: "/a", File: "a"}
	b := Page{Path: "/b", File: "b"}
	out1, _ := renderPages(pageManifest(a, b))
	out2, _ := renderPages(pageManifest(b, a))
	if out1 != out2 {
		t.Errorf("page render depends on insertion order:\n---1---\n%s\n---2---\n%s", out1, out2)
	}
}

func TestModelTemplate_GetPageTakesQueryOpts(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", data)
	if !strings.Contains(out, "func (m *WidgetModel) GetPage(limit, offset int, opts QueryOpts) ([]Widget, int, error)") {
		t.Errorf("GetPage should take QueryOpts:\n%s", out)
	}
	if !strings.Contains(out, "widgetAllowedColumns = []string{") {
		t.Errorf("missing allowed-columns whitelist:\n%s", out)
	}
	if !strings.Contains(out, "orderByClause(opts.Sort, widgetAllowedColumns)") {
		t.Errorf("GetPage should use orderByClause:\n%s", out)
	}
	// cache key must vary by sort/filter
	if !strings.Contains(out, "opts.Sort") || !strings.Contains(out, "opts.FilterField") {
		t.Errorf("cache key must include sort/filter:\n%s", out)
	}
}

func TestModelTemplate_UpdateOnlyWhenCRUD(t *testing.T) {
	noCrud := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", noCrud)
	if strings.Contains(out, "func (m *WidgetModel) Update(") {
		t.Errorf("Update must NOT appear without CRUD flag:\n%s", out)
	}

	crud := newData("widget", sampleFieldsWithNullable())
	crud.CRUD = true
	out = renderAndParse(t, "model.go.tmpl", crud)
	if !strings.Contains(out, "func (m *WidgetModel) Update(id int64, title string, notes *string, count int64, score *int64) error") {
		t.Errorf("Update signature wrong or missing:\n%s", out)
	}
	if !strings.Contains(out, "UPDATE widgets SET title = ?, notes = ?, count = ?, score = ? WHERE id = ?") {
		t.Errorf("Update SQL wrong:\n%s", out)
	}
	if !strings.Contains(out, "return sql.ErrNoRows") {
		t.Errorf("Update must return sql.ErrNoRows on 0 rows:\n%s", out)
	}
}

func TestModelTestTemplate_CRUDVariantValidGo(t *testing.T) {
	crud := newData("widget", sampleFieldsWithNullable())
	crud.CRUD = true
	renderAndParse(t, "model_test.go.tmpl", crud)
	// non-CRUD variant must also stay valid
	renderAndParse(t, "model_test.go.tmpl", newData("widget", sampleFieldsWithNullable()))
}

func TestResourceHandlersTemplate_ValidGoAllFive(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	data.CRUD = true
	out := renderAndParse(t, "resource_handlers.go.tmpl", data)
	for _, sym := range []string{
		"func WidgetListGET(database *db.DB, appCache *cache.Cache) http.HandlerFunc",
		"func WidgetDetailGET(database *db.DB, appCache *cache.Cache) http.HandlerFunc",
		"func WidgetCreatePOST(database *db.DB, appCache *cache.Cache) http.HandlerFunc",
		"func WidgetUpdatePUT(database *db.DB, appCache *cache.Cache) http.HandlerFunc",
		"func WidgetDeleteDELETE(database *db.DB, appCache *cache.Cache) http.HandlerFunc",
	} {
		if !strings.Contains(out, sym) {
			t.Errorf("missing handler %q:\n%s", sym, out)
		}
	}
	// sort/filter parsed and mapped to 422 on ErrInvalidQuery
	if !strings.Contains(out, "errors.Is(err, models.ErrInvalidQuery)") {
		t.Errorf("list handler must map ErrInvalidQuery to 422:\n%s", out)
	}
	if !strings.Contains(out, "chi.URLParam(r, \"id\")") {
		t.Errorf("detail/update/delete must read the id path param:\n%s", out)
	}
	if !strings.Contains(out, "sql.ErrNoRows") {
		t.Errorf("detail/update must handle not-found:\n%s", out)
	}
}

func TestResourceHandlersTestTemplate_ValidGo(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	data.CRUD = true
	renderAndParse(t, "resource_handlers_test.go.tmpl", data)
}

// TestRenderRoutes_MatchesCommittedManifest asserts that routes_gen.go is in
// sync with the api.json sitting next to it.
//
// This used to render from an EMPTY manifest and demand byte-equality with the
// committed file, which is true only in a pristine template: the instant a
// project ran any scaffold, routes_gen.go held real routes and this test was
// red forever. Every generated app inherited a failing src/builder suite, which
// teaches people to ignore it. Sync between manifest and generated file is the
// property actually worth asserting, and it holds in every project.
func TestRenderRoutes_MatchesCommittedManifest(t *testing.T) {
	m, err := readManifestAt("../app/api.json")
	if err != nil {
		t.Fatalf("read committed api.json: %v", err)
	}
	out, err := renderRoutes(m)
	if err != nil {
		t.Fatalf("renderRoutes: %v", err)
	}
	committed, err := os.ReadFile("../app/handlers/routes_gen.go")
	if err != nil {
		t.Fatalf("read committed routes_gen.go: %v", err)
	}
	if string(committed) != out {
		t.Errorf("handlers/routes_gen.go has drifted from api.json.\n"+
			"Re-run any scaffold tool (or regenerate) to bring them back in sync.\n"+
			"---committed---\n%s\n---rendered from api.json---\n%s", committed, out)
	}
}

// TestRenderPages_MatchesCommittedManifest is the same sync property for the
// page table — api.json's "pages" against handlers/pages_gen.go and its
// generated companion test.
func TestRenderPages_MatchesCommittedManifest(t *testing.T) {
	m, err := readManifestAt("../app/api.json")
	if err != nil {
		t.Fatalf("read committed api.json: %v", err)
	}
	for _, tc := range []struct {
		file   string
		render func(Manifest) (string, error)
	}{
		{"pages_gen.go", renderPages},
		{"pages_gen_test.go", renderPagesTest},
	} {
		out, err := tc.render(m)
		if err != nil {
			t.Fatalf("render %s: %v", tc.file, err)
		}
		committed, err := os.ReadFile("../app/handlers/" + tc.file)
		if err != nil {
			t.Fatalf("read committed %s: %v", tc.file, err)
		}
		if string(committed) != out {
			t.Errorf("handlers/%s has drifted from api.json.\n"+
				"Re-run any scaffold tool (or regenerate) to bring them back in sync.\n"+
				"---committed---\n%s\n---rendered from api.json---\n%s", tc.file, committed, out)
		}
	}
}

func TestRenderPagesTest_IsValidGoAndTablesThePages(t *testing.T) {
	out, err := renderPagesTest(pageManifest(
		Page{Path: "/login", File: "login", Title: "Sign In"},
		Page{Path: "/widgets", File: "widgets", Title: "Widgets", Auth: true},
	))
	if err != nil {
		t.Fatalf("renderPagesTest: %v", err)
	}
	parseAsGo(t, "pages_gen_test.go", out)
	for _, want := range []string{
		`{path: "/login", file: "login", title: "Sign In", auth: false},`,
		// The table carries auth, so the generated test can assert per page
		// that a guarded path redirects a signed-out visitor. Without the field
		// here, api.json could declare a page guarded and nothing generated
		// would ever check.
		`{path: "/widgets", file: "widgets", title: "Widgets", auth: true},`,
		"RegisterPages(r)",
		"func TestGeneratedPages_ServeTheirShell(t *testing.T)",
		"func TestGeneratedPages_GuardedPagesRedirectWhenSignedOut(t *testing.T)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in generated page test:\n%s", want, out)
		}
	}

	// The empty case must still compile — a pristine app has no pages.
	empty, err := renderPagesTest(pageManifest())
	if err != nil {
		t.Fatalf("renderPagesTest(empty): %v", err)
	}
	parseAsGo(t, "pages_gen_test.go", empty)
}

func budgetConst(t *testing.T, code, name string) int {
	t.Helper()
	m := regexp.MustCompile(name + `\s*=\s*(\d+)`).FindStringSubmatch(code)
	if m == nil {
		t.Fatalf("auth_buckets.go.tmpl does not define %s", name)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("%s is not an integer: %v", name, err)
	}
	return n
}

func TestModelTemplate_TimestampFieldIsModelsTime(t *testing.T) {
	out := renderAndParse(t, "model.go.tmpl", newData("widget", []Field{
		{Name: "updated_at", Type: "timestamp", Nullable: false},
		{Name: "archived_at", Type: "timestamp", Nullable: true},
	}))
	if !strings.Contains(out, "UpdatedAt Time `json:\"updated_at\"`") {
		t.Errorf("a NOT NULL timestamp must be models.Time:\n%s", out)
	}
	if !strings.Contains(out, "ArchivedAt *Time `json:\"archived_at\"`") {
		t.Errorf("a nullable timestamp must be *models.Time:\n%s", out)
	}
	if strings.Contains(out, "UpdatedAt string") || strings.Contains(out, "ArchivedAt *string") {
		t.Errorf("a timestamp field fell back to string — the defect this type exists to close:\n%s", out)
	}
	// The nullable scan path goes through models.NullTime, whose payload is a
	// Time. sql.NullTime's payload is a bare time.Time and would put a
	// *time.Time on the struct — RFC3339Nano, the thing models.Time prevents.
	if !strings.Contains(out, "var archived_atNull NullTime") {
		t.Errorf("a nullable timestamp must scan through models.NullTime:\n%s", out)
	}
	if strings.Contains(out, "sql.NullTime") {
		t.Errorf("sql.NullTime puts a bare time.Time on the struct:\n%s", out)
	}
}

// TestUserModelTemplate_CreatedAtIsModelsTime is §A8: the template broke the
// wire contract it ships with.
//
// CreatedAt was a bare time.Time here while every create_model output correctly
// used models.Time — and the contract's own words are "never use a bare
// time.Time in a model struct". Latent only while nothing serializes a User
// wholesale; the first endpoint returning the struct emits RFC3339Nano.
func TestValidateFieldTypes_RejectsUnknown(t *testing.T) {
	if err := validateFieldTypes([]Field{{Name: "updated_at", Type: "timestmap"}}); err == nil {
		t.Fatal("an unknown field type must be an error, not a silent string")
	}
	for _, ok := range []string{"string", "int", "float", "boolean", "timestamp"} {
		if err := validateFieldTypes([]Field{{Name: "f", Type: ok}}); err != nil {
			t.Errorf("%s should be accepted: %v", ok, err)
		}
	}
}

// TestAcceptedSQLTypes_TimestampTakesBothSpellings — SQLite has no date type,
// so DATETIME is a convention and the same column is spelled DATETIME by one
// author and TEXT by another. Both store identical bytes and both scan into
// models.Time; refusing one would push the author back to declaring the field a
// string, which is the defect.
func TestAcceptedSQLTypes_TimestampTakesBothSpellings(t *testing.T) {
	got := acceptedSQLTypes("timestamp")
	for _, want := range []string{"DATETIME", "TEXT"} {
		if !slices.Contains(got, want) {
			t.Errorf("timestamp should accept a %s column, got %v", want, got)
		}
	}
	if slices.Contains(acceptedSQLTypes("string"), "DATETIME") {
		t.Error("a string field must not silently sit on a DATETIME column")
	}
}
