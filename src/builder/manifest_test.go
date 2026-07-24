package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedTime() time.Time { return time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC) }

func sampleModel() Model {
	return Model{Name: "project", Table: "projects", Fields: []ModelField{
		{Name: "id", Type: "int", Nullable: false},
		{Name: "notes", Type: "string", Nullable: true},
	}}
}

func sampleEndpoint() Endpoint {
	return Endpoint{Method: "GET", Path: "/api/v1/projects", Handler: "ProjectListGET",
		Deps: []string{"read", "write", "cache"}, Auth: false, Model: "project", Kind: "list"}
}

func TestReadManifest_MissingFileIsEmpty(t *testing.T) {
	m, err := readManifestAt(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("readManifestAt(missing): %v", err)
	}
	if m.APIVersion != "1.0.0" || len(m.Models) != 0 || len(m.Endpoints) != 0 {
		t.Errorf("missing file should be empty manifest, got %+v", m)
	}
}

func TestUpsertModel_AddsThenReplaces(t *testing.T) {
	var m Manifest
	m.UpsertModel(sampleModel())
	if len(m.Models) != 1 {
		t.Fatalf("after add: got %d models, want 1", len(m.Models))
	}
	updated := sampleModel()
	updated.Fields = append(updated.Fields, ModelField{Name: "status", Type: "string"})
	m.UpsertModel(updated)
	if len(m.Models) != 1 {
		t.Fatalf("after replace: got %d models, want 1 (no duplicate)", len(m.Models))
	}
	if len(m.Models[0].Fields) != 3 {
		t.Errorf("replace did not take: got %d fields, want 3", len(m.Models[0].Fields))
	}
}

func TestUpsertEndpoint_AddsThenReplaces(t *testing.T) {
	var m Manifest
	if err := m.UpsertEndpoint(sampleEndpoint()); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := m.UpsertEndpoint(sampleEndpoint()); err != nil {
		t.Fatalf("re-add same handler (idempotent) should not error: %v", err)
	}
	if len(m.Endpoints) != 1 {
		t.Fatalf("re-adding same endpoint duplicated it: got %d", len(m.Endpoints))
	}
}

func TestUpsertEndpoint_ConflictErrors(t *testing.T) {
	var m Manifest
	if err := m.UpsertEndpoint(sampleEndpoint()); err != nil {
		t.Fatalf("add: %v", err)
	}
	conflict := sampleEndpoint()
	conflict.Handler = "SomethingElseGET"
	err := m.UpsertEndpoint(conflict)
	if err == nil {
		t.Fatal("expected conflict error for same (method,path) different handler")
	}
	if !strings.Contains(err.Error(), "/api/v1/projects") {
		t.Errorf("conflict error should name the path: %v", err)
	}
	if len(m.Endpoints) != 1 || m.Endpoints[0].Handler != "ProjectListGET" {
		t.Errorf("conflict must not mutate the manifest, got %+v", m.Endpoints)
	}
}

func TestHash_StableAndSensitive(t *testing.T) {
	var a Manifest
	a.UpsertModel(sampleModel())
	_ = a.UpsertEndpoint(sampleEndpoint())
	a.canonicalize()
	h1 := a.Hash

	// Re-canonicalizing identical content yields the same hash.
	a.canonicalize()
	if a.Hash != h1 {
		t.Errorf("hash not stable across no-op canonicalize: %s vs %s", h1, a.Hash)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("hash missing sha256: prefix: %s", h1)
	}

	// Adding an endpoint changes the hash.
	var b Manifest
	b.UpsertModel(sampleModel())
	_ = b.UpsertEndpoint(sampleEndpoint())
	e2 := sampleEndpoint()
	e2.Method, e2.Handler, e2.Kind = "POST", "ProjectCreatePOST", "create"
	_ = b.UpsertEndpoint(e2)
	b.canonicalize()
	if b.Hash == h1 {
		t.Error("hash did not change after adding an endpoint")
	}
}

func TestHash_OrderIndependent(t *testing.T) {
	e2 := sampleEndpoint()
	e2.Method, e2.Path, e2.Handler = "POST", "/api/v1/projects", "ProjectCreatePOST"

	var a Manifest
	_ = a.UpsertEndpoint(sampleEndpoint())
	_ = a.UpsertEndpoint(e2)
	a.canonicalize()

	var b Manifest
	_ = b.UpsertEndpoint(e2)
	_ = b.UpsertEndpoint(sampleEndpoint())
	b.canonicalize()

	if a.Hash != b.Hash {
		t.Errorf("hash depends on insertion order: %s vs %s", a.Hash, b.Hash)
	}
}

func TestFieldsToModel_AddsIDAndCreatedAt(t *testing.T) {
	fields := []Field{
		{Name: "name", Type: "string", Nullable: false},
		{Name: "notes", Type: "string", Nullable: true},
	}
	m := fieldsToModel("project", "projects", fields)
	if m.Name != "project" || m.Table != "projects" {
		t.Fatalf("model identity wrong: %+v", m)
	}
	// id first, created_at last, declared fields in between.
	if m.Fields[0].Name != "id" || m.Fields[0].Type != "int" {
		t.Errorf("first field should be id:int, got %+v", m.Fields[0])
	}
	last := m.Fields[len(m.Fields)-1]
	if last.Name != "created_at" || last.Type != "timestamp" {
		t.Errorf("last field should be created_at:timestamp, got %+v", last)
	}
	// nullable carried through.
	var notes ModelField
	for _, f := range m.Fields {
		if f.Name == "notes" {
			notes = f
		}
	}
	if !notes.Nullable {
		t.Error("notes should be nullable in the model")
	}
}

func TestUpdateManifestAt_WritesAndRegenerates(t *testing.T) {
	dir := t.TempDir()
	handlersDir := filepath.Join(dir, "handlers")
	if err := os.MkdirAll(handlersDir, 0755); err != nil {
		t.Fatal(err)
	}
	apiPath := filepath.Join(dir, "api.json")

	err := updateManifestAt(apiPath, handlersDir, fixedTime(),
		[]Model{sampleModel()},
		[]Endpoint{sampleEndpoint()})
	if err != nil {
		t.Fatalf("updateManifestAt: %v", err)
	}

	m, _ := readManifestAt(apiPath)
	if len(m.Models) != 1 || len(m.Endpoints) != 1 {
		t.Fatalf("manifest not written: %+v", m)
	}
	routes, err := os.ReadFile(filepath.Join(handlersDir, "routes_gen.go"))
	if err != nil {
		t.Fatalf("routes_gen.go not written: %v", err)
	}
	if !strings.Contains(string(routes), "ProjectListGET(database.Read, database.Write, appCache)") {
		t.Errorf("routes_gen.go missing the route:\n%s", routes)
	}
}

func TestUpdateManifestAt_ConflictWritesNothing(t *testing.T) {
	dir := t.TempDir()
	handlersDir := filepath.Join(dir, "handlers")
	_ = os.MkdirAll(handlersDir, 0755)
	apiPath := filepath.Join(dir, "api.json")

	// Seed one endpoint.
	if err := updateManifestAt(apiPath, handlersDir, fixedTime(), nil, []Endpoint{sampleEndpoint()}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(apiPath)

	// Same (method,path), different handler -> conflict.
	bad := sampleEndpoint()
	bad.Handler = "Rogue"
	err := updateManifestAt(apiPath, handlersDir, fixedTime(), nil, []Endpoint{bad})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	after, _ := os.ReadFile(apiPath)
	if string(before) != string(after) {
		t.Error("conflict must not modify api.json")
	}
}

func TestWriteThenRead_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.json")
	var m Manifest
	m.APIVersion = "1.0.0"
	m.UpsertModel(sampleModel())
	_ = m.UpsertEndpoint(sampleEndpoint())
	if err := writeManifestAt(path, &m, fixedTime()); err != nil {
		t.Fatalf("write: %v", err)
	}
	back, err := readManifestAt(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if back.Hash != m.Hash || len(back.Models) != 1 || len(back.Endpoints) != 1 {
		t.Errorf("round-trip mismatch: %+v", back)
	}
	if back.GeneratedAt != "2026-07-23T00:00:00Z" {
		t.Errorf("generated_at: got %q", back.GeneratedAt)
	}
}

func TestResourceEndpoints_FiveWithKinds(t *testing.T) {
	eps := resourceEndpoints("project")
	if len(eps) != 5 {
		t.Fatalf("got %d endpoints, want 5", len(eps))
	}
	want := map[string]string{
		"GET /api/v1/projects":         "list",
		"GET /api/v1/projects/{id}":    "detail",
		"POST /api/v1/projects":        "create",
		"PUT /api/v1/projects/{id}":    "update",
		"DELETE /api/v1/projects/{id}": "delete",
	}
	for _, e := range eps {
		key := e.Method + " " + e.Path
		wantKind, ok := want[key]
		if !ok {
			t.Errorf("unexpected endpoint %s", key)
			continue
		}
		if e.Kind != wantKind {
			t.Errorf("%s: kind got %q want %q", key, e.Kind, wantKind)
		}
		if e.Auth {
			t.Errorf("%s: should be public (auth:false)", key)
		}
		if e.Model != "project" {
			t.Errorf("%s: model got %q want project", key, e.Model)
		}
		if len(e.Deps) != 3 {
			t.Errorf("%s: deps got %v want [read write cache]", key, e.Deps)
		}
	}
	// The handler symbols must match what resource_handlers.go.tmpl generates.
	byKey := map[string]string{}
	for _, e := range eps {
		byKey[e.Method+" "+e.Path] = e.Handler
	}
	if byKey["GET /api/v1/projects"] != "ProjectListGET" ||
		byKey["GET /api/v1/projects/{id}"] != "ProjectDetailGET" ||
		byKey["POST /api/v1/projects"] != "ProjectCreatePOST" ||
		byKey["PUT /api/v1/projects/{id}"] != "ProjectUpdatePUT" ||
		byKey["DELETE /api/v1/projects/{id}"] != "ProjectDeleteDELETE" {
		t.Errorf("handler symbols wrong: %+v", byKey)
	}
}

func TestAuthEndpoints_SixWithKinds(t *testing.T) {
	eps := authEndpoints()
	if len(eps) != 6 {
		t.Fatalf("got %d endpoints, want 6", len(eps))
	}
	type want struct {
		handler string
		kind    string
		auth    bool
		deps    int
	}
	expect := map[string]want{
		"POST /api/v1/auth/login":          {"LoginPOST", "auth_login", false, 3},
		"POST /api/v1/auth/logout":         {"LogoutPOST", "auth_logout", false, 0},
		"GET /api/v1/auth/me":              {"MeGET", "auth_me", true, 3},
		"POST /api/v1/auth/login_token":    {"MobileLoginPOST", "mobile_login", false, 3},
		"DELETE /api/v1/auth/logout_token": {"MobileLogoutDELETE", "mobile_logout", false, 1},
		"GET /api/v1/auth/me_token":        {"MobileMeGET", "mobile_me", false, 3},
	}
	for _, e := range eps {
		key := e.Method + " " + e.Path
		w, ok := expect[key]
		if !ok {
			t.Errorf("unexpected endpoint %s", key)
			continue
		}
		if e.Handler != w.handler {
			t.Errorf("%s: handler %q want %q", key, e.Handler, w.handler)
		}
		if e.Kind != w.kind {
			t.Errorf("%s: kind %q want %q", key, e.Kind, w.kind)
		}
		if e.Auth != w.auth {
			t.Errorf("%s: auth %v want %v", key, e.Auth, w.auth)
		}
		if len(e.Deps) != w.deps {
			t.Errorf("%s: deps len %d want %d", key, len(e.Deps), w.deps)
		}
		if e.Model != "" {
			t.Errorf("%s: auth endpoints carry no model, got %q", key, e.Model)
		}
	}
}
