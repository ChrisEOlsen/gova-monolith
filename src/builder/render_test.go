package main

import (
	"go/parser"
	"go/token"
	"os"
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

func TestListHandlerTestTemplate_IsValidGo(t *testing.T) {
	data := newData("widget", sampleFields())
	renderAndParse(t, "list_handler_test.go.tmpl", data)
}

func TestAuthTestTemplate_IsValidGo(t *testing.T) {
	data := newData("user", nil)
	renderAndParse(t, "auth_test.go.tmpl", data)
}

func TestRegisterTestTemplate_IsValidGo(t *testing.T) {
	data := newData("user", nil)
	renderAndParse(t, "register_test.go.tmpl", data)
}

func TestMobileAuthTestTemplate_IsValidGo(t *testing.T) {
	renderAndParse(t, "mobile_auth_test.go.tmpl", TemplateData{})
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
			Deps: []string{"read", "write", "cache"}, Kind: "list"},
		Endpoint{Method: "DELETE", Path: "/api/v1/auth/logout_token", Handler: "MobileLogoutDELETE",
			Deps: []string{"write"}, Auth: true, Kind: "mobile_logout"},
		Endpoint{Method: "POST", Path: "/api/v1/auth/logout", Handler: "LogoutPOST",
			Deps: []string{}, Kind: "auth_logout"},
	))
	if err != nil {
		t.Fatalf("renderRoutes: %v", err)
	}
	parseAsGo(t, "routes_gen.go", out)
	want := []string{
		`r.Get("/api/v1/projects", ProjectListGET(database.Read, database.Write, appCache))`,
		`r.With(middleware.RequireAuth).Delete("/api/v1/auth/logout_token", MobileLogoutDELETE(database.Write))`,
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
	e1 := Endpoint{Method: "GET", Path: "/api/v1/a", Handler: "AGet", Deps: []string{"read"}, Kind: "list"}
	e2 := Endpoint{Method: "GET", Path: "/api/v1/b", Handler: "BGet", Deps: []string{"read"}, Kind: "list"}
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
// runs. handleScaffoldMobileAuth registers MobileMeGET/MobileLogoutDELETE
// with Auth:false for exactly this reason — this test proves renderRoutes
// respects that and doesn't add the wrap, while still confirming a genuine
// Auth:true endpoint DOES get wrapped (so the test would catch a regression
// in either direction).
func TestRenderRoutes_MobileBearerNotWrapped(t *testing.T) {
	out, err := renderRoutes(routeManifest(
		Endpoint{Method: "GET", Path: "/api/v1/auth/me_token", Handler: "MobileMeGET",
			Deps: []string{"read", "write", "cache"}, Auth: false, Kind: "mobile_me"},
		Endpoint{Method: "GET", Path: "/api/v1/auth/me", Handler: "MeGET",
			Deps: []string{"read", "write", "cache"}, Auth: true, Kind: "auth_me"},
	))
	if err != nil {
		t.Fatalf("renderRoutes: %v", err)
	}
	parseAsGo(t, "routes_gen.go", out)

	wantMobileLine := `r.Get("/api/v1/auth/me_token", MobileMeGET(database.Read, database.Write, appCache))`
	if !strings.Contains(out, wantMobileLine) {
		t.Errorf("missing unwrapped mobile-me route line:\n  want: %s\n  in:\n%s", wantMobileLine, out)
	}
	if strings.Contains(out, `middleware.RequireAuth).Get("/api/v1/auth/me_token"`) {
		t.Errorf("mobile bearer endpoint must NOT be wrapped in middleware.RequireAuth:\n%s", out)
	}

	wantSessionLine := `r.With(middleware.RequireAuth).Get("/api/v1/auth/me", MeGET(database.Read, database.Write, appCache))`
	if !strings.Contains(out, wantSessionLine) {
		t.Errorf("missing RequireAuth-wrapped session route line:\n  want: %s\n  in:\n%s", wantSessionLine, out)
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
		"func WidgetListGET(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc",
		"func WidgetDetailGET(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc",
		"func WidgetCreatePOST(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc",
		"func WidgetUpdatePUT(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc",
		"func WidgetDeleteDELETE(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc",
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

func TestRenderRoutes_EmptyMatchesCommittedFile(t *testing.T) {
	out, err := renderRoutes(routeManifest())
	if err != nil {
		t.Fatalf("renderRoutes: %v", err)
	}
	committed, err := os.ReadFile("../app/handlers/routes_gen.go")
	if err != nil {
		t.Fatalf("read committed routes_gen.go: %v", err)
	}
	if string(committed) != out {
		t.Errorf("committed routes_gen.go is not byte-identical to renderRoutes(empty).\n"+
			"Regenerate it to match.\n---committed---\n%s\n---generated---\n%s", committed, out)
	}
}
