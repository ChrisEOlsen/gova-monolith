# GOVA API Manifest + Auto-Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `src/app/api.json` the single machine-readable source of truth for the generated API, from which both the served `/api/v1/_manifest` and a generated `handlers/routes_gen.go` are derived — so route wiring is automatic and the contract iOS reads can never drift from the actual routes.

**Architecture:** Every route-producing MCP tool upserts its models and endpoints into `api.json`, then regenerates `handlers/routes_gen.go` (which `main.go` mounts via one `RegisterGenerated` call). The app serves `api.json` at `/api/v1/_manifest` and echoes its content hash from `/api/v1/_version`. `inspect_app` returns structured JSON cross-checking the manifest against on-disk files.

**Tech Stack:** Go 1.25 (`net/http`, `chi`, `database/sql`, `crypto/sha256`, `text/template`, `go:embed`-free), `mark3labs/mcp-go` MCP tools, Docker Compose. Two separate Go modules: `src/app` (the app, tested in Docker) and `src/builder` (the generator, tested on the host).

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-07-23-gova-api-manifest-design.md` — authoritative.
- **Branch:** `build/api-manifest`. Never use a git worktree (the `gova-builder` MCP container's bind mounts are path-bound — see `CLAUDE.md`).
- **`api.json` is committed source, not a build artifact.** Upsert-only; no removal tool.
- **Endpoints keyed by `(method, path)`; models by `name`.** Upsert replaces same-key; a conflicting `(method,path)`→different-`handler` errors and writes nothing (not `api.json`, not any generated file).
- **`hash`** = `sha256:` hex over canonical JSON of `{models, endpoints}` (both sorted — models by `name`, endpoints by `(path, method)`; `generated_at` excluded). Always recomputed and re-sorted on write, so the file is byte-stable.
- **`deps`** is a closed set `["read","write","cache"]`, in constructor-argument order: `read`→`database.Read`, `write`→`database.Write`, `cache`→`appCache`.
- **`auth:true`** means the client must authenticate AND `routes_gen.go` wraps the route in `middleware.RequireAuth`. There is exactly ONE auth-enforcement mechanism — the route wrap. `create_handler`'s inline `middleware.UserID` check is removed.
- **`main.go` is never edited for a route again** after this build — `RegisterGenerated` owns scaffolded routes; `_version`/`_manifest` stay hand-wired framework endpoints.
- **App runtime CWD is `/src/app`** (entrypoint does `cd /src/app`), so `os.ReadFile("./api.json")` resolves to `/src/app/api.json`, exactly where the builder writes it — same mechanism as `http.Dir("./static")`.
- **App changes take effect on `docker compose restart app`** (entrypoint rebuilds from bind-mounted source). **Builder/template changes require `docker compose up -d --build`** — the `mcp` image embeds templates via `//go:embed` at build time; a plain restart runs the stale binary.
- Two test suites, both required: `docker compose exec app go test ./...` (app) and `cd src/builder && go test ./...` (builder, on host).
- No new third-party dependencies. No Node/npm.

---

## Shared Type Definitions

These types appear in multiple tasks. Builder (`src/builder`) and app (`src/app/handlers`) each declare their own copy — they are the wire contract on both sides of a module boundary, so the duplication is intentional.

**Builder — `src/builder/manifest.go`, package `main`:**

```go
type Manifest struct {
	APIVersion  string     `json:"api_version"`
	Hash        string     `json:"hash"`
	GeneratedAt string     `json:"generated_at"`
	Models      []Model    `json:"models"`
	Endpoints   []Endpoint `json:"endpoints"`
}

type Model struct {
	Name   string       `json:"name"`
	Table  string       `json:"table"`
	Fields []ModelField `json:"fields"`
}

type ModelField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type Endpoint struct {
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Handler string   `json:"handler"`
	Deps    []string `json:"deps"`
	Auth    bool     `json:"auth"`
	Model   string   `json:"model,omitempty"`
	Kind    string   `json:"kind"`
}
```

**App — `src/app/handlers/manifest.go`, package `handlers`:** the identical struct set (same JSON tags). The app only reads/decodes.

---

## Task 1: Manifest model — read, upsert, hash (builder)

**Files:**
- Create: `src/builder/manifest.go`
- Test: `src/builder/manifest_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (package `main`):
  - The five types above.
  - `readManifestAt(path string) (Manifest, error)` — a missing file returns the empty manifest (`APIVersion:"1.0.0"`, empty slices), not an error.
  - `(m *Manifest) UpsertModel(Model)` — replace by `Name`, else append.
  - `(m *Manifest) UpsertEndpoint(Endpoint) error` — replace by `(Method,Path)`; a same-key record with a different `Handler` returns an error and mutates nothing.
  - `(m *Manifest) canonicalize()` — sort models by `Name`, endpoints by `(Path,Method)`, recompute `Hash`.
  - `manifestHash(m Manifest) string` — `sha256:`+hex over `{models,endpoints}` (sorted, no `generated_at`).
  - `writeManifestAt(path string, m *Manifest, now time.Time) error` — canonicalize, set `GeneratedAt`, write pretty JSON.

- [ ] **Step 1: Write the failing test**

Create `src/builder/manifest_test.go`:

```go
package main

import (
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src/builder && go test ./... -run 'TestReadManifest|TestUpsert|TestHash|TestWriteThenRead' -v`
Expected: FAIL — `undefined: Manifest`, `undefined: readManifestAt`, etc.

- [ ] **Step 3: Write the implementation**

Create `src/builder/manifest.go`:

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

type Manifest struct {
	APIVersion  string     `json:"api_version"`
	Hash        string     `json:"hash"`
	GeneratedAt string     `json:"generated_at"`
	Models      []Model    `json:"models"`
	Endpoints   []Endpoint `json:"endpoints"`
}

type Model struct {
	Name   string       `json:"name"`
	Table  string       `json:"table"`
	Fields []ModelField `json:"fields"`
}

type ModelField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type Endpoint struct {
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Handler string   `json:"handler"`
	Deps    []string `json:"deps"`
	Auth    bool     `json:"auth"`
	Model   string   `json:"model,omitempty"`
	Kind    string   `json:"kind"`
}

// readManifestAt loads a manifest. A missing file is not an error — it is the
// empty manifest, which is the correct state for an app that has scaffolded
// nothing yet.
func readManifestAt(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Manifest{APIVersion: "1.0.0", Models: []Model{}, Endpoints: []Endpoint{}}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("api.json is corrupt: %w", err)
	}
	if m.APIVersion == "" {
		m.APIVersion = "1.0.0"
	}
	return m, nil
}

func (m *Manifest) UpsertModel(model Model) {
	for i := range m.Models {
		if m.Models[i].Name == model.Name {
			m.Models[i] = model
			return
		}
	}
	m.Models = append(m.Models, model)
}

// UpsertEndpoint replaces a same-key endpoint or appends. A same (method,path)
// naming a different handler is a conflict: two scaffolds claiming one route.
// It errors and leaves the manifest untouched.
func (m *Manifest) UpsertEndpoint(e Endpoint) error {
	for i := range m.Endpoints {
		if m.Endpoints[i].Method == e.Method && m.Endpoints[i].Path == e.Path {
			if m.Endpoints[i].Handler != e.Handler {
				return fmt.Errorf("route conflict: %s %s is already registered by handler %q, cannot reassign to %q",
					e.Method, e.Path, m.Endpoints[i].Handler, e.Handler)
			}
			m.Endpoints[i] = e
			return nil
		}
	}
	m.Endpoints = append(m.Endpoints, e)
	return nil
}

func (m *Manifest) canonicalize() {
	sort.Slice(m.Models, func(i, j int) bool { return m.Models[i].Name < m.Models[j].Name })
	sort.Slice(m.Endpoints, func(i, j int) bool {
		if m.Endpoints[i].Path != m.Endpoints[j].Path {
			return m.Endpoints[i].Path < m.Endpoints[j].Path
		}
		return m.Endpoints[i].Method < m.Endpoints[j].Method
	})
	m.Hash = manifestHash(*m)
}

// manifestHash is sha256 over just the models and endpoints (sorted by
// canonicalize before this is called), excluding generated_at so an
// otherwise-identical manifest always hashes the same.
func manifestHash(m Manifest) string {
	payload := struct {
		Models    []Model    `json:"models"`
		Endpoints []Endpoint `json:"endpoints"`
	}{m.Models, m.Endpoints}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeManifestAt(path string, m *Manifest, now time.Time) error {
	if m.APIVersion == "" {
		m.APIVersion = "1.0.0"
	}
	m.canonicalize()
	m.GeneratedAt = now.UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd src/builder && go test ./... -run 'TestReadManifest|TestUpsert|TestHash|TestWriteThenRead' -v`
Expected: PASS — all seven functions.

- [ ] **Step 5: Commit**

```bash
git add src/builder/manifest.go src/builder/manifest_test.go
git commit -m "feat: manifest model with upsert, conflict detection, stable hash"
```

---

## Task 2: routes_gen generation + committed initial files

**Files:**
- Create: `src/builder/templates/routes_gen.go.tmpl`
- Modify: `src/builder/manifest.go` (add generation functions)
- Modify: `src/builder/render_test.go` (add route-render assertions)
- Create: `src/app/api.json` (committed empty manifest)
- Create: `src/app/handlers/routes_gen.go` (committed empty `RegisterGenerated`)

**Interfaces:**
- Consumes: `Manifest`, `Endpoint` (Task 1).
- Produces (package `main`):
  - `renderRoutes(m Manifest) (string, error)` — returns the full `routes_gen.go` source.
  - `regenerateRoutesAt(handlersDir string, m Manifest) error` — writes `<handlersDir>/routes_gen.go`.
  - `callExpr(e Endpoint) string` — the constructor call, e.g. `ProjectListGET(database.Read, database.Write, appCache)`.
- The committed `src/app/handlers/routes_gen.go` provides `handlers.RegisterGenerated(r chi.Router, database *db.DB, appCache *cache.Cache)` for Task 4's `main.go` call.

- [ ] **Step 1: Write the failing render test**

Add to `src/builder/render_test.go`:

```go
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
```

**Note:** `render_test.go` already has a helper that parses rendered output as Go. In Build 1 it was named `renderAndParse` (renders a *template* by name). Here we render a *string*, so add a sibling helper `parseAsGo(t, name, src)` at the top of `render_test.go`:

```go
func parseAsGo(t *testing.T, name, src string) {
	t.Helper()
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, name, src, parser.AllErrors); err != nil {
		t.Fatalf("%s is not valid Go: %v\n---\n%s", name, err, src)
	}
}
```

(`go/parser` and `go/token` are already imported by `render_test.go`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src/builder && go test ./... -run TestRenderRoutes -v`
Expected: FAIL — `undefined: renderRoutes`.

- [ ] **Step 3: Write the template**

Create `src/builder/templates/routes_gen.go.tmpl`:

```gotemplate
// Code generated from api.json by gova-builder. DO NOT EDIT.
package handlers

import (
	"github.com/go-chi/chi/v5"
	"gova/app/cache"
	"gova/app/db"
{{- if .UsesAuth}}
	"gova/app/middleware"
{{- end}}
)

// RegisterGenerated mounts every scaffolded API route. main.go calls this once
// and is never hand-edited for routes. Framework endpoints (_version,
// _manifest) are wired directly in main.go, not here.
func RegisterGenerated(r chi.Router, database *db.DB, appCache *cache.Cache) {
{{- range .Lines}}
	{{.}}
{{- end}}
}
```

- [ ] **Step 4: Write the generation functions**

Add to `src/builder/manifest.go`:

```go
// callExpr builds the handler constructor call from the endpoint's deps, in
// argument order. read->database.Read, write->database.Write, cache->appCache.
func callExpr(e Endpoint) string {
	args := make([]string, 0, len(e.Deps))
	for _, d := range e.Deps {
		switch d {
		case "read":
			args = append(args, "database.Read")
		case "write":
			args = append(args, "database.Write")
		case "cache":
			args = append(args, "appCache")
		}
	}
	return e.Handler + "(" + strings.Join(args, ", ") + ")"
}

// chiMethod maps an HTTP method to the chi router method name (Get, Post, ...).
func chiMethod(method string) string {
	m := strings.ToLower(method)
	return strings.ToUpper(m[:1]) + m[1:]
}

func renderRoutes(m Manifest) (string, error) {
	m.canonicalize()
	usesAuth := false
	lines := make([]string, 0, len(m.Endpoints))
	for _, e := range m.Endpoints {
		call := callExpr(e)
		var line string
		if e.Auth {
			usesAuth = true
			line = fmt.Sprintf(`r.With(middleware.RequireAuth).%s(%q, %s)`, chiMethod(e.Method), e.Path, call)
		} else {
			line = fmt.Sprintf(`r.%s(%q, %s)`, chiMethod(e.Method), e.Path, call)
		}
		lines = append(lines, line)
	}
	data := struct {
		UsesAuth bool
		Lines    []string
	}{usesAuth, lines}
	return renderNamedToString("routes_gen.go.tmpl", data)
}

func regenerateRoutesAt(handlersDir string, m Manifest) error {
	out, err := renderRoutes(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(handlersDir, "routes_gen.go"), []byte(out), 0644)
}
```

`renderRoutes` needs to render a template with a NON-`TemplateData` payload. The existing `renderToString` takes `TemplateData`. Add a generic sibling in `src/builder/main.go` next to `renderToString`:

```go
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
```

Add `"path/filepath"` and `"strings"` to `manifest.go`'s imports (both are stdlib; `strings` may already be needed).

- [ ] **Step 5: Run the render tests**

Run: `cd src/builder && go test ./... -run TestRenderRoutes -v`
Expected: PASS.

- [ ] **Step 6: Create the committed initial files**

The app must compile before anything is scaffolded, so commit an empty manifest and an empty generated routes file.

Create `src/app/api.json`:

```json
{
  "api_version": "1.0.0",
  "hash": "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
  "generated_at": "2026-07-23T00:00:00Z",
  "models": [],
  "endpoints": []
}
```

(That hash is `sha256("{\"models\":null,\"endpoints\":null}")` — the empty-manifest hash `manifestHash` produces; it is informational and will be overwritten on first scaffold.)

Create `src/app/handlers/routes_gen.go` — must exactly match what `renderRoutes(empty)` produces, so a later regeneration yields a clean (empty) diff:

```go
// Code generated from api.json by gova-builder. DO NOT EDIT.
package handlers

import (
	"github.com/go-chi/chi/v5"
	"gova/app/cache"
	"gova/app/db"
)

// RegisterGenerated mounts every scaffolded API route. main.go calls this once
// and is never hand-edited for routes. Framework endpoints (_version,
// _manifest) are wired directly in main.go, not here.
func RegisterGenerated(r chi.Router, database *db.DB, appCache *cache.Cache) {
}
```

- [ ] **Step 7: Lock the committed empty file to the generator, byte-for-byte**

The committed `src/app/handlers/routes_gen.go` MUST equal what `renderRoutes(empty)` emits, or the first real scaffold rewrites it with spurious whitespace churn. Assert exact equality against the real committed file (the builder test runs on the host with CWD `src/builder`; the app file is at `../app/handlers/routes_gen.go`). Add to `render_test.go`:

```go
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
```

Add `"os"` to `render_test.go`'s imports if absent.

**Implementer note:** the surest way to make the committed file match is to *generate* it, not hand-type it. After the template and `renderRoutes` exist, run this throwaway to emit the exact bytes, then commit that file:

```bash
cd src/builder && cat > /tmp/dump_test.go <<'EOF'
package main
import ("os"; "testing")
func TestDumpEmpty(t *testing.T){ out,_ := renderRoutes(routeManifest()); os.WriteFile("../app/handlers/routes_gen.go", []byte(out), 0644) }
EOF
cp /tmp/dump_test.go ./dump_test.go && go test -run TestDumpEmpty ./... && rm ./dump_test.go
```

Then Step 6's hand-written file is overwritten with the exact generator output. Verify: `cd src/builder && go test ./... -v` (full suite) and `docker compose exec app go build ./...` — both succeed, and `TestRenderRoutes_EmptyMatchesCommittedFile` passes.

- [ ] **Step 8: Commit**

```bash
git add src/builder/manifest.go src/builder/render_test.go src/builder/templates/routes_gen.go.tmpl \
        src/app/api.json src/app/handlers/routes_gen.go
git commit -m "feat: routes_gen.go generation + committed empty manifest and routes"
```

---

## Task 3: App-side ManifestGET and version manifest_hash

**Files:**
- Create: `src/app/handlers/manifest.go`
- Create: `src/app/handlers/manifest_test.go`
- Modify: `src/app/handlers/version.go`
- Modify: `src/app/handlers/version_test.go`

**Interfaces:**
- Consumes: `jsonOK` (envelope), the `api.json` format (Task 1).
- Produces (package `handlers`):
  - The app-side `Manifest`/`Model`/`ModelField`/`Endpoint` structs (same JSON tags as the builder's).
  - `loadManifest(path string) Manifest` — reads+decodes; returns the empty manifest if the file is absent or unreadable.
  - `ManifestGET() http.HandlerFunc` — serves `loadManifest("./api.json")` through `jsonOK`.
  - `manifestPath = "./api.json"` package constant.
  - `VersionGET` gains `manifest_hash` in its response.

- [ ] **Step 1: Write the failing tests**

Create `src/app/handlers/manifest_test.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeTempManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "api.json")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatalf("write temp manifest: %v", err)
	}
	return p
}

func TestLoadManifest_Present(t *testing.T) {
	p := writeTempManifest(t, `{"api_version":"1.0.0","hash":"sha256:abc",
		"models":[{"name":"project","table":"projects","fields":[
			{"name":"notes","type":"string","nullable":true}]}],
		"endpoints":[{"method":"GET","path":"/api/v1/projects","handler":"ProjectListGET",
			"deps":["read","write","cache"],"auth":false,"model":"project","kind":"list"}]}`)
	m := loadManifest(p)
	if m.Hash != "sha256:abc" || len(m.Models) != 1 || len(m.Endpoints) != 1 {
		t.Fatalf("load mismatch: %+v", m)
	}
	if !m.Models[0].Fields[0].Nullable {
		t.Error("nullable field lost in decode")
	}
}

func TestLoadManifest_MissingIsEmpty(t *testing.T) {
	m := loadManifest(filepath.Join(t.TempDir(), "absent.json"))
	if m.APIVersion != "1.0.0" || len(m.Models) != 0 || len(m.Endpoints) != 0 {
		t.Errorf("absent manifest should be empty, got %+v", m)
	}
}

func TestManifestGET_ServesEnvelope(t *testing.T) {
	// ManifestGET reads "./api.json" relative to CWD; the committed repo file
	// exists at the app module root, which is the test's working dir.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/_manifest", nil)
	rec := httptest.NewRecorder()
	ManifestGET()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			APIVersion string `json:"api_version"`
			Models     []any  `json:"models"`
			Endpoints  []any  `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if !body.OK || body.Data.APIVersion == "" {
		t.Errorf("unexpected manifest envelope: %s", rec.Body.String())
	}
}
```

Add to `src/app/handlers/version_test.go` (the file exists from Build 1):

```go
func TestVersionGET_IncludesManifestHash(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/_version", nil)
	rec := httptest.NewRecorder()
	VersionGET()(rec, req)

	var body struct {
		Data struct {
			ManifestHash string `json:"manifest_hash"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The committed api.json has a non-empty hash; VersionGET reads it.
	if body.Data.ManifestHash == "" {
		t.Error("version response missing manifest_hash")
	}
}
```

(`encoding/json` is already imported in `version_test.go`.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `docker compose exec app go test ./handlers/... -run 'TestLoadManifest|TestManifestGET|TestVersionGET_IncludesManifestHash' -v`
Expected: FAIL — `undefined: loadManifest`, `undefined: ManifestGET`, and `manifest_hash` absent.

- [ ] **Step 3: Write manifest.go**

Create `src/app/handlers/manifest.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"os"
)

const manifestPath = "./api.json"

// Manifest mirrors the builder's api.json shape. The app only reads it.
type Manifest struct {
	APIVersion  string     `json:"api_version"`
	Hash        string     `json:"hash"`
	GeneratedAt string     `json:"generated_at"`
	Models      []Model    `json:"models"`
	Endpoints   []Endpoint `json:"endpoints"`
}

type Model struct {
	Name   string       `json:"name"`
	Table  string       `json:"table"`
	Fields []ModelField `json:"fields"`
}

type ModelField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type Endpoint struct {
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Handler string   `json:"handler"`
	Deps    []string `json:"deps"`
	Auth    bool     `json:"auth"`
	Model   string   `json:"model,omitempty"`
	Kind    string   `json:"kind"`
}

// loadManifest reads and decodes the manifest. A missing or unreadable file
// yields the empty manifest — a fresh app has an empty but valid contract, not
// an error.
func loadManifest(path string) Manifest {
	empty := Manifest{APIVersion: "1.0.0", Models: []Model{}, Endpoints: []Endpoint{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return empty
	}
	if m.Models == nil {
		m.Models = []Model{}
	}
	if m.Endpoints == nil {
		m.Endpoints = []Endpoint{}
	}
	return m
}

// ManifestGET handles GET /api/v1/_manifest
func ManifestGET() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, loadManifest(manifestPath))
	}
}
```

- [ ] **Step 4: Update version.go**

Change the `versionInfo` struct and `VersionGET` in `src/app/handlers/version.go`:

```go
type versionInfo struct {
	APIVersion       string `json:"api_version"`
	MinClientVersion string `json:"min_client_version"`
	ManifestHash     string `json:"manifest_hash"`
}

// VersionGET handles GET /api/v1/_version
func VersionGET() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, versionInfo{
			APIVersion:       APIVersion,
			MinClientVersion: MinClientVersion,
			ManifestHash:     loadManifest(manifestPath).Hash,
		})
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `docker compose exec app go test ./handlers/... -v`
Expected: PASS — new manifest tests, the version test, and every Build 1 handler test (the package must still compile with the added structs).

- [ ] **Step 6: Commit**

```bash
git add src/app/handlers/manifest.go src/app/handlers/manifest_test.go \
        src/app/handlers/version.go src/app/handlers/version_test.go
git commit -m "feat: ManifestGET endpoint and manifest_hash on version"
```

---

## Task 4: main.go wiring — RegisterGenerated + _manifest route

**Files:**
- Modify: `src/app/main.go:50-59` (the API + `@gova-routes` region)
- Test: `src/app/handlers/routes_gen_test.go` (create)

**Interfaces:**
- Consumes: `handlers.RegisterGenerated` (Task 2's committed file), `handlers.ManifestGET` (Task 3).
- Produces: an app whose scaffolded routes come entirely from `RegisterGenerated`.

- [ ] **Step 1: Write the failing test**

Create `src/app/handlers/routes_gen_test.go` — proves `RegisterGenerated` is callable and mounts cleanly (the empty committed version mounts nothing; the real integration proof with a live route is the end-to-end task):

```go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"gova/app/cache"
	"gova/app/db"
)

// RegisterGenerated must be safe to call with the committed empty route set —
// the app has to boot before anything is scaffolded.
func TestRegisterGenerated_EmptyIsSafe(t *testing.T) {
	r := chi.NewRouter()
	database := &db.DB{}
	RegisterGenerated(r, database, cache.New())

	// An unregistered path 404s through chi — proves the router is functional
	// and RegisterGenerated added no catch-all.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nothing", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unregistered route, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run the test to verify it passes (already-committed RegisterGenerated)**

Run: `docker compose exec app go test ./handlers/... -run TestRegisterGenerated_EmptyIsSafe -v`
Expected: PASS — Task 2 already committed the empty `RegisterGenerated`, so this compiles and passes immediately. (This task's real change is `main.go`; the test guards that `RegisterGenerated` stays callable.)

- [ ] **Step 3: Wire main.go**

In `src/app/main.go`, replace the block from `// API` through `// @gova-routes` (lines ~50-59):

```go
	// API — framework endpoints (exist before anything is scaffolded)
	r.Get("/api/v1/_version", handlers.VersionGET())
	r.Get("/api/v1/_manifest", handlers.ManifestGET())

	// Generated API routes. Source of truth: api.json -> handlers/routes_gen.go.
	// Never hand-edit routes here; scaffold tools regenerate RegisterGenerated.
	handlers.RegisterGenerated(r, database, appCache)
```

- [ ] **Step 4: Verify the app builds, boots, and serves the framework endpoints**

```bash
docker compose restart app
until curl -sf localhost:8080/api/v1/_version >/dev/null 2>&1; do sleep 2; done
curl -s localhost:8080/api/v1/_version
curl -s localhost:8080/api/v1/_manifest
docker compose exec app go test ./...
```

Expected:
- `_version` returns `api_version`, `min_client_version`, and a non-empty `manifest_hash`.
- `_manifest` returns `{"ok":true,"data":{...,"models":[],"endpoints":[]}}`.
- All app tests pass.

- [ ] **Step 5: Commit**

```bash
git add src/app/main.go src/app/handlers/routes_gen_test.go
git commit -m "feat: main.go mounts _manifest and RegisterGenerated, drops @gova-routes"
```

---

## Task 5: scaffold_list + create_handler/create_page self-register

**Files:**
- Modify: `src/builder/main.go` — `handleScaffoldList`, `handleCreateHandler`, `handleCreatePage`, their `mcp.NewTool` schemas, and a shared `registerEndpoints` helper
- Modify: `src/builder/templates/handler.go.tmpl` — remove inline auth check
- Modify: `src/builder/render_test.go` — assert the auth check is gone

**Interfaces:**
- Consumes: `readManifestAt`, `writeManifestAt`, `UpsertModel`, `UpsertEndpoint`, `regenerateRoutesAt` (Tasks 1-2), `applySchema` (Build 1, for the model's fields+nullability).
- Produces: a shared helper other tasks reuse:
  - `updateManifest(models []Model, endpoints []Endpoint) error` — reads `/src/app/api.json`, upserts all, writes it back, regenerates `/src/app/handlers/routes_gen.go`. Returns an error (e.g. a route conflict) without partial writes.
  - `manifestFilePath = "/src/app/api.json"` and `handlersDirPath = "/src/app/handlers"` constants.
  - `fieldsToModel(name, table string, fields []Field) Model` — converts Build 1 `Field` records (with `.Nullable`) into a manifest `Model`, appending the implicit `id` and `created_at` fields.

- [ ] **Step 1: Write the failing test (manifest helper + fields conversion)**

Add to `src/builder/manifest_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd src/builder && go test ./... -run 'TestFieldsToModel|TestUpdateManifestAt' -v`
Expected: FAIL — `undefined: fieldsToModel`, `undefined: updateManifestAt`.

- [ ] **Step 3: Implement the helpers**

Add to `src/builder/manifest.go`:

```go
const (
	manifestFilePath = "/src/app/api.json"
	handlersDirPath  = "/src/app/handlers"
)

// fieldsToModel converts Build 1 Field records (carrying schema-derived
// Nullable) into a manifest Model, adding the implicit id (first) and
// created_at (last) columns every generated table has.
func fieldsToModel(name, table string, fields []Field) Model {
	out := make([]ModelField, 0, len(fields)+2)
	out = append(out, ModelField{Name: "id", Type: "int", Nullable: false})
	for _, f := range fields {
		typ := f.Type
		if typ == "password" {
			typ = "string"
		}
		out = append(out, ModelField{Name: f.Name, Type: typ, Nullable: f.Nullable})
	}
	out = append(out, ModelField{Name: "created_at", Type: "timestamp", Nullable: false})
	return Model{Name: name, Table: table, Fields: out}
}

// updateManifestAt is the transactional core: read, upsert all, and only if
// every upsert succeeded, write api.json and regenerate routes_gen.go. A
// conflict returns before any file is touched.
func updateManifestAt(apiPath, handlersDir string, now time.Time, models []Model, endpoints []Endpoint) error {
	m, err := readManifestAt(apiPath)
	if err != nil {
		return err
	}
	for _, model := range models {
		m.UpsertModel(model)
	}
	for _, e := range endpoints {
		if err := m.UpsertEndpoint(e); err != nil {
			return err // conflict — nothing written yet
		}
	}
	if err := writeManifestAt(apiPath, &m, now); err != nil {
		return err
	}
	return regenerateRoutesAt(handlersDir, m)
}
```

Add a production wrapper in `src/builder/main.go` that binds the real paths and clock (this is what the tool handlers call):

```go
func updateManifest(models []Model, endpoints []Endpoint) error {
	return updateManifestAt(manifestFilePath, handlersDirPath, time.Now(), models, endpoints)
}
```

Add `"time"` to `main.go`'s imports if not present.

- [ ] **Step 4: Run to verify the helpers pass**

Run: `cd src/builder && go test ./... -run 'TestFieldsToModel|TestUpdateManifestAt' -v`
Expected: PASS.

- [ ] **Step 5: Wire scaffold_list to self-register**

In `handleScaffoldList` (`src/builder/main.go`), after the files are rendered successfully and before the return, replace the current return that prints "Next: wire route in main.go" with manifest registration. The model has `fields` (already `applySchema`-validated with `.Nullable`) and `name`. Add:

```go
	model := fieldsToModel(name, toPlural(name), fields)
	endpoint := Endpoint{
		Method: "GET", Path: "/api/v1/" + toPlural(name),
		Handler: toPascal(name) + "ListGET",
		Deps:    []string{"read", "write", "cache"},
		Auth:    false, Model: name, Kind: "list",
	}
	if err := updateManifest([]Model{model}, []Endpoint{endpoint}); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}
```

Change the final `mcp.NewToolResultText(...)` to report registration instead of hand-wiring:

```go
	return mcp.NewToolResultText(
		strings.Join(results, "\n") +
			"\n\nRegistered route GET /api/v1/" + toPlural(name) + " and updated api.json + routes_gen.go.\n" +
			"Add forms with add_js_form.\n\n" + runPatternChecks(),
	), nil
```

- [ ] **Step 6: Add method+path to create_handler and self-register**

Update the `create_handler` tool schema (`mcp.NewTool("create_handler", ...)` in `main`) to add a required `path`:

```go
	s.AddTool(mcp.NewTool("create_handler",
		mcp.WithDescription("Generate a single JSON handler in handlers/name.go AND register its route in api.json + routes_gen.go. Implement the TODO logic after."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Handler name in snake_case")),
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method: GET, POST, PUT, DELETE")),
		mcp.WithString("path", mcp.Required(), mcp.Description("Full route path, e.g. /api/v1/projects/{id}/archive")),
		mcp.WithBoolean("auth_required", mcp.Description("Require authentication — enforced by a RequireAuth route wrap")),
	), handleCreateHandler)
```

In `handleCreateHandler`, read `path`, validate it, and self-register after rendering:

```go
	path, _ := req.Params.Arguments["path"].(string)
	if !strings.HasPrefix(path, "/api/v1/") {
		return errResult("path must start with /api/v1/"), nil
	}
	// ... existing render of handler.go.tmpl ...
	endpoint := Endpoint{
		Method: strings.ToUpper(method), Path: path,
		Handler: toPascal(name) + strings.ToUpper(method),
		Deps:    []string{"read", "write", "cache"},
		Auth:    authRequired, Kind: "custom",
	}
	if err := updateManifest(nil, []Endpoint{endpoint}); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}
	return mcp.NewToolResultText("Created: " + outPath +
		"\nRegistered " + strings.ToUpper(method) + " " + path +
		" in api.json + routes_gen.go.\nImplement the TODO logic.\n\n" + runPatternChecks()), nil
```

Note the handler symbol is `{Pascal}{METHOD}` (e.g. `ArchiveProjectPOST`), matching `handler.go.tmpl`'s `{{.PascalName}}{{.Method}}` — confirm `data.Method` is set to the uppercased method (it is: `data.Method = strings.ToUpper(method)`).

- [ ] **Step 7: Add method+path to create_page and self-register**

Update the `create_page` schema to add `path` (its Go handler stub needs a route), and in `handleCreatePage`, after rendering, register the endpoint the same way (handler `toPascal(filename) + "GET"`, since create_page's handler is always the page's GET; `data.Method` is `"GET"`). Use `kind: "custom"`. Drop the "Next: wire route in main.go" text.

```go
	path, _ := req.Params.Arguments["path"].(string)
	if !strings.HasPrefix(path, "/api/v1/") {
		return errResult("path must start with /api/v1/"), nil
	}
	// ... existing renders ...
	endpoint := Endpoint{
		Method: "GET", Path: path, Handler: toPascal(filename) + "GET",
		Deps: []string{"read", "write", "cache"}, Auth: authRequired, Kind: "custom",
	}
	if err := updateManifest(nil, []Endpoint{endpoint}); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}
```

And add `path` to the `create_page` `mcp.NewTool` schema with `mcp.Required()`.

- [ ] **Step 8: Remove the inline auth check from handler.go.tmpl**

`routes_gen.go` now enforces auth via the `RequireAuth` wrap, so the handler body must not also check. Replace `src/builder/templates/handler.go.tmpl` with:

```gotemplate
package handlers

import (
	"database/sql"
	"net/http"
	"gova/app/cache"
)

func {{.PascalName}}{{.Method}}(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO: implement handler logic
		// Use model methods — never write raw SQL here.
		// Auth (if required) is enforced by the RequireAuth route wrap in
		// routes_gen.go — do not re-check it here.
		jsonOK(w, nil)
	}
}
```

Update the render assertion in `render_test.go` — replace any assertion that the handler template contains a `middleware.UserID` check with one asserting it does NOT:

```go
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
```

- [ ] **Step 9: Run the full builder suite**

Run: `cd src/builder && go build ./... && go test ./... -v`
Expected: build succeeds; all tests pass, including the updated handler-template assertion and the manifest helpers.

- [ ] **Step 10: Commit**

```bash
git add src/builder/main.go src/builder/manifest.go src/builder/manifest_test.go \
        src/builder/render_test.go src/builder/templates/handler.go.tmpl
git commit -m "feat: scaffold_list + create_handler/create_page self-register into manifest"
```

---

## Task 6: Auth scaffolders self-register

**Files:**
- Modify: `src/builder/main.go` — `handleScaffoldAuth`, `handleScaffoldRegistration`, `handleScaffoldMobileAuth`

**Interfaces:**
- Consumes: `updateManifest`, `Endpoint` (Task 5).
- Produces: the auth/registration/mobile endpoints registered in the manifest instead of printed as paste instructions.

- [ ] **Step 1: Wire scaffold_auth**

In `handleScaffoldAuth`, after the files render successfully, register its three endpoints and drop the printed route lines. The user model is also a manifest model (`fieldsToModel` with the user's fields if available; if the auth scaffolder does not expose its fields as `[]Field`, register just the endpoints and add the `user` model with its known columns — `id`, `name`, `email`, `password`, `created_at` — via an explicit `Model`). Register:

```go
	userModel := Model{Name: "user", Table: "users", Fields: []ModelField{
		{Name: "id", Type: "int", Nullable: false},
		{Name: "name", Type: "string", Nullable: false},
		{Name: "email", Type: "string", Nullable: false},
		{Name: "created_at", Type: "timestamp", Nullable: false},
	}}
	endpoints := []Endpoint{
		{Method: "POST", Path: "/api/v1/auth/login", Handler: "LoginPOST",
			Deps: []string{"read", "write", "cache"}, Kind: "auth_login"},
		{Method: "POST", Path: "/api/v1/auth/logout", Handler: "LogoutPOST",
			Deps: []string{}, Kind: "auth_logout"},
		{Method: "GET", Path: "/api/v1/auth/me", Handler: "MeGET",
			Deps: []string{"read", "write", "cache"}, Auth: true, Kind: "auth_me"},
	}
	if err := updateManifest([]Model{userModel}, endpoints); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}
```

Note the `password` column is deliberately omitted from the `user` manifest model — it must never be exposed to a client. Replace the printed-routes string in the return with `"Registered auth routes (login, logout, me) and the user model in api.json + routes_gen.go."`.

- [ ] **Step 2: Wire scaffold_registration**

In `handleScaffoldRegistration`, register:

```go
	endpoint := Endpoint{Method: "POST", Path: "/api/v1/auth/register", Handler: "RegisterPOST",
		Deps: []string{"read", "write", "cache"}, Kind: "register"}
	if err := updateManifest(nil, []Endpoint{endpoint}); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}
```

Drop its printed route line; report registration.

- [ ] **Step 3: Wire scaffold_mobile_auth**

In `handleScaffoldMobileAuth`, register:

```go
	endpoints := []Endpoint{
		{Method: "POST", Path: "/api/v1/auth/login_token", Handler: "MobileLoginPOST",
			Deps: []string{"read", "write", "cache"}, Kind: "mobile_login"},
		{Method: "DELETE", Path: "/api/v1/auth/logout_token", Handler: "MobileLogoutDELETE",
			Deps: []string{"write"}, Auth: true, Kind: "mobile_logout"},
		{Method: "GET", Path: "/api/v1/auth/me_token", Handler: "MobileMeGET",
			Deps: []string{"read", "write", "cache"}, Auth: true, Kind: "mobile_me"},
	}
	if err := updateManifest(nil, endpoints); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}
```

Drop the printed route lines; report registration.

**Important — CSRF exemption for mobile login:** Build 1 fixed `middleware/csrf.go` to exempt `/api/v1/auth/login_token`. Because that endpoint is now mounted by `RegisterGenerated` with no auth wrap (it *issues* the token), the exemption still applies by path — no change needed. But `MobileLoginPOST` must NOT get `auth:true` (a client cannot present a token before logging in). Confirm it is registered with no `Auth` field, as above.

- [ ] **Step 4: Build and run the builder suite**

Run: `cd src/builder && go build ./... && go test ./...`
Expected: build succeeds, all tests pass. (No new unit tests here — these are wiring changes exercised end-to-end in Task 8; the manifest logic they call is already covered by Task 1/5 tests.)

- [ ] **Step 5: Commit**

```bash
git add src/builder/main.go
git commit -m "feat: auth/registration/mobile_auth scaffolders self-register routes"
```

---

## Task 7: inspect_app returns structured JSON

**Files:**
- Modify: `src/builder/main.go` — `handleInspectApp`
- Test: `src/builder/inspect_test.go` (create)

**Interfaces:**
- Consumes: `readManifestAt`, `Manifest` (Task 1).
- Produces: `inspect_app` returns a JSON object `{manifest, on_disk, divergence}`.

- [ ] **Step 1: Write the failing test**

Because `handleInspectApp` reads hardcoded `/src/...` paths, extract the logic into a testable pure function first. Create `src/builder/inspect_test.go`:

```go
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildInspection_ReportsDivergence(t *testing.T) {
	m := Manifest{
		Models:    []Model{{Name: "task", Table: "tasks"}},
		Endpoints: []Endpoint{{Method: "GET", Path: "/api/v1/tasks", Handler: "TaskListGET"}},
	}
	onDisk := onDiskFiles{
		Models:   []string{}, // Task.go missing
		Handlers: []string{"task_list.go", "routes_gen.go"},
	}
	rep := buildInspection(m, onDisk)

	if len(rep.Divergence) == 0 {
		t.Fatal("expected divergence for missing Task.go")
	}
	joined := strings.Join(rep.Divergence, " ")
	if !strings.Contains(joined, "task") {
		t.Errorf("divergence should name the missing model: %v", rep.Divergence)
	}

	// It must serialize to JSON with the three top-level keys.
	data, _ := json.Marshal(rep)
	for _, key := range []string{`"manifest"`, `"on_disk"`, `"divergence"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("inspection JSON missing %s: %s", key, data)
		}
	}
}

func TestBuildInspection_CleanWhenConsistent(t *testing.T) {
	m := Manifest{
		Models:    []Model{{Name: "project", Table: "projects"}},
		Endpoints: []Endpoint{},
	}
	onDisk := onDiskFiles{Models: []string{"Project.go"}, Handlers: []string{"routes_gen.go"}}
	rep := buildInspection(m, onDisk)
	if len(rep.Divergence) != 0 {
		t.Errorf("expected no divergence, got %v", rep.Divergence)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd src/builder && go test ./... -run TestBuildInspection -v`
Expected: FAIL — `undefined: onDiskFiles`, `undefined: buildInspection`.

- [ ] **Step 3: Implement the pure inspection logic**

Add to `src/builder/main.go` (or a new `inspect.go` in package `main`):

```go
type onDiskFiles struct {
	Models   []string `json:"models"`
	Handlers []string `json:"handlers"`
	Pages    []string `json:"pages"`
	JS       []string `json:"js"`
}

type inspection struct {
	Manifest   Manifest    `json:"manifest"`
	OnDisk     onDiskFiles `json:"on_disk"`
	Divergence []string    `json:"divergence"`
}

// buildInspection cross-checks the manifest against the files actually on disk.
func buildInspection(m Manifest, onDisk onDiskFiles) inspection {
	present := func(list []string, name string) bool {
		for _, f := range list {
			if f == name {
				return true
			}
		}
		return false
	}
	div := []string{}
	for _, model := range m.Models {
		if !present(onDisk.Models, toPascal(model.Name)+".go") {
			div = append(div, "api.json lists model '"+model.Name+"' but src/app/models/"+toPascal(model.Name)+".go is missing")
		}
	}
	return inspection{Manifest: m, OnDisk: onDisk, Divergence: div}
}
```

- [ ] **Step 4: Rewrite handleInspectApp to use it and return JSON**

Replace the body of `handleInspectApp` so it (a) scans the four directories into an `onDiskFiles` (reusing the existing glob logic, but collecting names into slices), (b) reads the manifest via `readManifestAt(manifestFilePath)`, (c) calls `buildInspection`, (d) marshals to indented JSON and returns it:

```go
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
```

Add `"encoding/json"` to `main.go`'s imports if not already present. Remove the now-unused old prose-building code (the `listDir` closure and route regex) from `handleInspectApp`.

- [ ] **Step 5: Run the builder suite**

Run: `cd src/builder && go build ./... && go test ./... -v`
Expected: build succeeds, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add src/builder/main.go src/builder/inspect_test.go
git commit -m "feat: inspect_app returns structured JSON with manifest/on_disk/divergence"
```

---

## Task 8: Documentation

**Files:**
- Modify: `CLAUDE.md` (this repo)
- Modify: `../gova-ios/CLAUDE.md`

**Interfaces:**
- Consumes: everything above. Produces no code.

- [ ] **Step 1: Update the monolith CLAUDE.md**

Verify each claim against the code before writing it. Edits:

1. **The Golden Recipe, step 3 and the Custom/Escape-Hatch Pattern** — remove every "wire route in main.go" / "wire GET route in main.go" instruction. Routing is now automatic. In the Escape-Hatch numbered list, replace the "wire route" step with: "routes are registered automatically — scaffold and create_handler/create_page update api.json and routes_gen.go."
2. **`create_handler` / `create_page` in the Tool Cheat Sheet** — note they now take `method` + `path` and self-register their route.
3. **Add an "API Manifest & Routing" subsection** after the existing "API Wire Contract" section:

```markdown
## API Manifest & Routing

`src/app/api.json` is the machine-readable source of truth for the API surface —
every model (with field types and nullability) and every endpoint (method, path,
handler, auth, kind). It is committed source, not a build artifact.

- **Routes are automatic.** Scaffold tools and `create_handler`/`create_page`
  upsert their records into `api.json` and regenerate
  `src/app/handlers/routes_gen.go`. `main.go` mounts them with one
  `handlers.RegisterGenerated(...)` call. **Never hand-wire a route in main.go,
  and never edit `routes_gen.go` (it is generated).**
- **Per-endpoint auth is declarative.** An endpoint's `auth: true` makes
  `routes_gen.go` wrap it in `middleware.RequireAuth`. Handlers do not check auth
  inline.
- **Served at `GET /api/v1/_manifest`.** `GET /api/v1/_version` also reports a
  `manifest_hash` so a client or CI can detect any surface change.
- **`inspect_app` returns JSON** — `{manifest, on_disk, divergence}` — and flags
  files that drifted from the manifest.
- **No removal tool.** `api.json` is upsert-only; to remove a resource, edit
  `api.json` and re-run a scaffold, or regenerate.
```

4. **Confirm the Build 1 mcp-rebuild note is present** (it was added in Build 1). It applies doubly now — builder changes to manifest/template logic need `docker compose up -d --build`.

- [ ] **Step 2: Update ../gova-ios/CLAUDE.md (forward-pointer only)**

Add a short note (do NOT rewire `/export:mobile` — that is Build 3):

```markdown
> **API manifest (as of the monolith's Build 2):** the web app now serves a
> machine-readable contract at `GET /api/v1/_manifest` — every model (with field
> types and nullability) and every endpoint (method, path, auth, kind). A future
> update to `/export:mobile` will read this instead of parsing Go/JS source. Until
> then, `/export:mobile` still works as documented below.
```

- [ ] **Step 3: Verify no stale hand-wiring instructions remain**

```bash
grep -n "wire route in main.go\|wire GET route\|wire the route" CLAUDE.md
```
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: API manifest, automatic routing, declarative auth"
cd ../gova-ios && git add CLAUDE.md && \
  git commit -m "docs: forward-pointer to /api/v1/_manifest (consumed in Build 3)" && cd -
```

---

## Task 9: End-to-end verification via MCP

**Files:** none modified — run and observe, then revert the scratch scaffold.

**Interfaces:** consumes everything above.

**Context:** The `mcp` container embeds templates AND the manifest/generator code via `go:embed` and compilation at IMAGE BUILD time. This task MUST rebuild the image first, or it exercises the stale binary (this exact trap cost real time in Build 1). Drive the MCP server over stdio via `docker exec -i gove-test-mcp-1 /usr/local/bin/mcp-server` with a JSON-RPC handshake (initialize → notifications/initialized → tools/call), as validated in Build 1.

- [ ] **Step 1: Rebuild everything clean**

```bash
docker compose down
rm -f data/app.db data/app.db-wal data/app.db-shm   # scratch DB is a git-ignored bind mount
docker compose up -d --build
until docker compose exec -T app true 2>/dev/null; do sleep 2; done
```

- [ ] **Step 2: Both suites green on the clean build**

```bash
docker compose exec app go test ./...
cd src/builder && go test ./... && cd -
```
Expected: all packages `ok`.

- [ ] **Step 3: Fresh manifest is empty, main.go untouched by scaffolding**

```bash
git stash list; git status --short   # confirm clean tree before scaffolding
curl -s localhost:8080/api/v1/_manifest
```
Expected: `_manifest` returns `data.models: []`, `data.endpoints: []`.

- [ ] **Step 4: Scaffold a resource with a nullable column via MCP**

Drive the server (helper shell function as in Build 1). Call `execute_sql` to create:
```sql
CREATE TABLE projects (id INTEGER PRIMARY KEY, name TEXT NOT NULL, notes TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
```
then `scaffold_list(name='project', fields=['name:string','notes:string'])`.

Expected: the tool reports "Registered route GET /api/v1/projects ... updated api.json + routes_gen.go".

- [ ] **Step 5: Assert the manifest and generated routes — and that main.go was NOT touched**

```bash
git status --short src/app/main.go              # expected: NO change to main.go
cat src/app/api.json | python3 -m json.tool | grep -A2 '"notes"'   # nullable:true present
grep -n "ProjectListGET" src/app/handlers/routes_gen.go             # route generated
```
Expected: `main.go` unmodified; `api.json` has the `project` model with `notes` `nullable:true` and the `GET /api/v1/projects` endpoint; `routes_gen.go` mounts `ProjectListGET`.

- [ ] **Step 6: The route is live and the manifest serves it**

```bash
docker compose restart app     # app rebuilds from bind-mounted source, picks up routes_gen.go
until curl -sf localhost:8080/api/v1/_version >/dev/null 2>&1; do sleep 2; done
curl -s "localhost:8080/api/v1/projects"
curl -s "localhost:8080/api/v1/_manifest" | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; print('models:',[m['name'] for m in d['models']],'endpoints:',[e['path'] for e in d['endpoints']])"
```
Expected: `/api/v1/projects` returns `{"ok":true,"data":[],"meta":{...}}` (mounted by RegisterGenerated, no hand-wiring); `_manifest` lists the `project` model and `/api/v1/projects` endpoint.

- [ ] **Step 7: A custom auth endpoint via create_handler self-registers with a RequireAuth wrap**

Call `create_handler(name='archive_project', method='POST', path='/api/v1/projects/archive', auth_required=true)`.

```bash
grep -n "RequireAuth).Post(\"/api/v1/projects/archive\"" src/app/handlers/routes_gen.go
docker compose restart app
until curl -sf localhost:8080/api/v1/_version >/dev/null 2>&1; do sleep 2; done
curl -s -o /dev/null -w "%{http_code}\n" -X POST "localhost:8080/api/v1/projects/archive"
```
Expected: `routes_gen.go` wraps the route in `RequireAuth`; the unauthenticated POST returns **401** (the wrap enforces auth — the generated handler has no inline check).

- [ ] **Step 8: A conflicting registration errors and changes nothing**

Call `create_handler(name='rogue', method='GET', path='/api/v1/projects', auth_required=false)` — same `(GET, /api/v1/projects)` as the list route, different handler.

Expected: the tool returns an error naming the conflict; `git status --short src/app/api.json` shows NO change (the conflict wrote nothing). Confirm `handlers/rogue.go` may exist as a rendered file but the manifest/routes were not mutated — note this in the report (the file render happens before the manifest step; a follow-up could reorder, but the route is not registered, which is the safety-critical property).

- [ ] **Step 9: inspect_app returns JSON**

Call `inspect_app`. Expected: a JSON object with `manifest`, `on_disk`, `divergence` keys; `manifest.endpoints` includes `/api/v1/projects`.

- [ ] **Step 10: Revert the scratch scaffold**

```bash
git checkout -- src/app/api.json src/app/handlers/routes_gen.go
rm -f src/app/models/Project.go src/app/models/Project_test.go \
      src/app/handlers/project_list.go src/app/handlers/project_list_test.go \
      src/app/handlers/archive_project.go src/app/handlers/rogue.go \
      src/app/static/pages/projects.html src/app/static/js/projects.js
git status --short
```
Expected: clean tree (only the committed empty `api.json` and `routes_gen.go` remain, unmodified).

- [ ] **Step 11: Reset scratch DB and final full-suite on clean tree**

```bash
docker compose down
rm -f data/app.db data/app.db-wal data/app.db-shm
docker compose up -d
until docker compose exec -T app true 2>/dev/null; do sleep 2; done
docker compose exec app go test ./...
cd src/builder && go test ./... && cd -
git log --oneline main..HEAD | wc -l
```
Expected: both suites pass on the clean tree; the log shows the Build 2 commits.

- [ ] **Step 12: Confirm nothing outstanding**

```bash
git status --short
```
Expected: clean.

---

## Verification Summary

| Concern | Where proven |
|---|---|
| api.json upsert / conflict / stable hash | Task 1 |
| routes_gen.go valid, deterministic, deps+auth correct | Task 2, Task 5 |
| Committed empty files let a fresh app compile | Task 2 Step 7, Task 4 |
| ManifestGET serves the contract; version carries the hash | Task 3 |
| main.go mounts via RegisterGenerated, never hand-edited | Task 4, Task 9 Step 5 |
| Nullability reaches the manifest | Task 5 Step 1, Task 9 Step 5 |
| Auth enforced by route wrap, not inline | Task 5 Step 8, Task 9 Step 7 |
| All route-producing tools self-register | Tasks 5-6, Task 9 |
| inspect_app returns JSON with divergence | Task 7, Task 9 Step 9 |
| Route conflict is loud and non-mutating | Task 1, Task 9 Step 8 |
| End-to-end through the rebuilt MCP server | Task 9 |
