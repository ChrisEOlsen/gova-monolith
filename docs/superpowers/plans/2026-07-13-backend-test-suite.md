# Backend Test Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Go-only backend test suite — a shared test-db helper, generated tests for every MCP-scaffolded model/handler/auth endpoint, and wiring so `/build`'s verification loop actually runs `go test`.

**Architecture:** New hand-written helper `src/app/db/testutil.go` opens a temp-file SQLite db per test. New `_test.go.tmpl` template files render alongside their existing code templates (`model.go.tmpl`, `auth_handler.go.tmpl`, etc.), driven by the same `TemplateData`/`fields` argument, and get wired into `src/builder/main.go`'s existing `renderToFile` calls. A new `src/builder/render_test.go` harness verifies every new template renders syntactically valid Go, fast and without Docker. `go test ./...` runs via `docker compose exec app go test ./...` (the `app` container already bind-mounts all of `/src`, including `/src/builder`, and already has the cgo/gcc toolchain).

**Tech Stack:** Go stdlib `testing` + `net/http/httptest` + `go/parser`. No new dependency.

## Global Constraints

- No new dependency — Go stdlib only (`testing`, `net/http/httptest`, `go/parser`, `go/token`).
- Tests use a temp-file SQLite db (`t.TempDir()/test.db` via the existing `db.Open`), never SQLite `:memory:` (unsafe with `db.Open`'s separate Write/Read `*sql.DB` handles) and never `/data/app.db`.
- `go test` runs via `docker compose exec app ...` — never baked into `entrypoint.sh` or the Dockerfile build stage (would test a stale snapshot and could block a broken app from starting for interactive debugging).
- Every generated test template is driven by the same `TemplateData`/`fields` argument as its corresponding code template — one source of truth, no separate schema definition to drift.
- Out of scope (per `docs/superpowers/specs/2026-07-13-backend-test-suite-design.md`): JS/frontend testing, the repo-wide constraint-enforcement grep test, CI pipeline integration, backfilling tests onto already-scaffolded apps.

---

### Task 1: Test database helper

**Files:**
- Create: `src/app/db/testutil.go`
- Test: `src/app/db/testutil_test.go`

**Interfaces:**
- Produces: `func OpenTest(t *testing.T, schema string) *DB` — every later task's generated tests call this.

- [ ] **Step 1: Write the failing test**

```go
// src/app/db/testutil_test.go
package db

import "testing"

func TestOpenTest_WriteVisibleToRead(t *testing.T) {
	d := OpenTest(t, `CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT);`)

	if _, err := d.Write.Exec("INSERT INTO widgets (id, name) VALUES (1, 'a')"); err != nil {
		t.Fatalf("insert via Write: %v", err)
	}

	var name string
	if err := d.Read.QueryRow("SELECT name FROM widgets WHERE id = 1").Scan(&name); err != nil {
		t.Fatalf("select via Read: %v", err)
	}
	if name != "a" {
		t.Errorf("got %q, want %q", name, "a")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec app go test ./db/... -run TestOpenTest -v`
Expected: FAIL — `undefined: OpenTest`

- [ ] **Step 3: Write minimal implementation**

```go
// src/app/db/testutil.go
package db

import (
	"path/filepath"
	"testing"
)

// OpenTest opens a temp-file SQLite database for a test, applies schema,
// and registers cleanup. Never touches /data/app.db. Uses a file (not
// :memory:) because Open returns separate Write/Read *sql.DB handles —
// :memory: without a shared-cache DSN would give each handle its own
// private database, so a write via Write would be invisible to Read.
func OpenTest(t *testing.T, schema string) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("db.OpenTest: open: %v", err)
	}
	if _, err := d.Write.Exec(schema); err != nil {
		d.Close()
		t.Fatalf("db.OpenTest: apply schema: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `docker compose exec app go test ./db/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/app/db/testutil.go src/app/db/testutil_test.go
git commit -m "test: add temp-file db helper for backend tests"
```

---

### Task 2: Template-render verification harness

**Files:**
- Create: `src/builder/render_test.go`

**Interfaces:**
- Consumes: `renderToString(tmplName string, data TemplateData) (string, error)` (existing, `src/builder/main.go`), `newData(name string, fields []Field) TemplateData` (existing), `Field` (existing struct: `{Name, Type string}`).
- Produces: `func renderAndParse(t *testing.T, tmplName string, data TemplateData) string` and `func sampleFields() []Field` — every Task 3-7 template gets a sanity test built on these.

- [ ] **Step 1: Write the failing test**

```go
// src/builder/render_test.go
package main

import (
	"go/parser"
	"go/token"
	"testing"
)

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -run TestRenderAndParse -v"`
Expected: this specific test actually PASSES immediately (`model.go.tmpl` already exists and is already valid Go) — there is no red state for this step since it exercises an existing template. Confirm the harness itself is correct: temporarily rename `getTemplate`'s embed pattern (or misspell `tmplName` to `"model.go.NOPE"` in a scratch edit, not committed) and confirm the test fails with "output is not valid Go" or a render error, then revert. This proves the harness catches a real breakage before Task 3 relies on it.

- [ ] **Step 3: N/A — harness has no separate implementation step**

The harness IS the implementation; Step 1 already contains it in full.

- [ ] **Step 4: Run test to verify it passes**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -v"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/builder/render_test.go
git commit -m "test: add template-render validity harness for the builder"
```

---

### Task 3: Model test template

**Files:**
- Create: `src/builder/templates/model_test.go.tmpl`
- Modify: `src/builder/main.go` (add `sqlType`/`testArgs` to `funcMap`; wire template into `handleCreateModel` and `handleScaffoldList`)
- Modify: `src/builder/render_test.go` (add sanity test)

**Interfaces:**
- Consumes: `Field{Name, Type string}`, `TemplateData.PascalName`, `TemplateData.PluralName`, `TemplateData.Fields` (existing), `renderAndParse`/`sampleFields` (Task 2).
- Produces: generated `models/<Name>_test.go` files calling `New<Name>Model(...)`, `.Create(...)`, `.Find(...)`, `.GetAll()`, `.Delete(...)` — the same model interface `model.go.tmpl` already generates.

- [ ] **Step 1: Write the failing test**

```go
// src/builder/render_test.go — add this function
func TestModelTestTemplate_IsValidGo(t *testing.T) {
	data := newData("widget", sampleFields())
	renderAndParse(t, "model_test.go.tmpl", data)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -run TestModelTestTemplate -v"`
Expected: FAIL — `render model_test.go.tmpl: open templates/model_test.go.tmpl: file does not exist`

- [ ] **Step 3: Write minimal implementation**

Add two functions to `funcMap` in `src/builder/main.go` (in the existing `var funcMap = template.FuncMap{...}` block, alongside `toPascal`/`toPlural`/etc.):

```go
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
			switch f.Type {
			case "int":
				vals[i] = "int64(1)"
			case "boolean":
				vals[i] = "true"
			case "float":
				vals[i] = "1.5"
			default:
				vals[i] = `"test"`
			}
		}
		return strings.Join(vals, ", ")
	},
```

New file:

```go
// src/builder/templates/model_test.go.tmpl
package models

import (
	"testing"

	"gova/app/cache"
	"gova/app/db"
)

func Test{{.PascalName}}Model_CRUD(t *testing.T) {
	testDB := db.OpenTest(t, `CREATE TABLE {{.PluralName}} (
		id INTEGER PRIMARY KEY,
		{{- range .Fields}}
		{{.Name}} {{sqlType .Type}},
		{{- end}}
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	m := New{{.PascalName}}Model(testDB.Read, testDB.Write, cache.New())

	id, err := m.Create({{testArgs .Fields}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == 0 {
		t.Fatal("Create returned id 0")
	}

	found, err := m.Find(id)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.ID != id {
		t.Errorf("Find: got ID %d, want %d", found.ID, id)
	}

	all, err := m.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("GetAll: got %d items, want 1", len(all))
	}
	if all[0].ID != id {
		t.Errorf("GetAll: got ID %d, want %d", all[0].ID, id)
	}

	if err := m.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Find(id); err == nil {
		t.Error("Find after Delete: expected error, got nil")
	}
}
```

In `handleCreateModel`, after the existing `model.go.tmpl` render, add:

```go
	testPath := "/src/app/models/" + toPascal(name) + "_test.go"
	if err := renderToFile("model_test.go.tmpl", testPath, data); err != nil {
		return errResult(err.Error()), nil
	}
```

(and include `testPath` in the returned "Created:" message, matching the existing style)

In `handleScaffoldList`'s `specs` slice, add one entry:

```go
		{"model_test.go.tmpl", "/src/app/models/" + toPascal(name) + "_test.go"},
```

right after the existing `{"model.go.tmpl", ...}` entry.

- [ ] **Step 4: Run test to verify it passes**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -v"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/builder/templates/model_test.go.tmpl src/builder/main.go src/builder/render_test.go
git commit -m "feat: generate model CRUD tests from create_model/scaffold_list"
```

---

### Task 4: List handler test template

**Files:**
- Create: `src/builder/templates/list_handler_test.go.tmpl`
- Modify: `src/builder/main.go` (wire into `handleScaffoldList`)
- Modify: `src/builder/render_test.go` (add sanity test)

**Interfaces:**
- Consumes: `TemplateData.PascalName`/`.PluralName`/`.Fields` (existing), `sqlType`/`testArgs` (Task 3), the handler signature `func {{.PascalName}}ListGET(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc` produced by `list_handler.go.tmpl` (existing).

- [ ] **Step 1: Write the failing test**

```go
// src/builder/render_test.go — add this function
func TestListHandlerTestTemplate_IsValidGo(t *testing.T) {
	data := newData("widget", sampleFields())
	renderAndParse(t, "list_handler_test.go.tmpl", data)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -run TestListHandlerTestTemplate -v"`
Expected: FAIL — template file does not exist

- [ ] **Step 3: Write minimal implementation**

```go
// src/builder/templates/list_handler_test.go.tmpl
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gova/app/cache"
	"gova/app/db"
	"gova/app/models"
)

func Test{{.PascalName}}ListGET(t *testing.T) {
	testDB := db.OpenTest(t, `CREATE TABLE {{.PluralName}} (
		id INTEGER PRIMARY KEY,
		{{- range .Fields}}
		{{.Name}} {{sqlType .Type}},
		{{- end}}
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	appCache := cache.New()
	model := models.New{{.PascalName}}Model(testDB.Read, testDB.Write, appCache)
	if _, err := model.Create({{testArgs .Fields}}); err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/{{.PluralName}}", nil)
	rec := httptest.NewRecorder()

	{{.PascalName}}ListGET(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("body missing ok:true: %s", rec.Body.String())
	}
}
```

In `handleScaffoldList`'s `specs` slice, add:

```go
		{"list_handler_test.go.tmpl", "/src/app/handlers/" + name + "_list_test.go"},
```

right after the existing `{"list_handler.go.tmpl", ...}` entry.

- [ ] **Step 4: Run test to verify it passes**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -v"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/builder/templates/list_handler_test.go.tmpl src/builder/main.go src/builder/render_test.go
git commit -m "feat: generate list handler tests from scaffold_list"
```

---

### Task 5: Auth test template

**Files:**
- Create: `src/builder/templates/auth_test.go.tmpl`
- Modify: `src/builder/main.go` (wire into `handleScaffoldAuth`)
- Modify: `src/builder/render_test.go` (add sanity test)

**Interfaces:**
- Consumes: `LoginPOST(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc` (existing, `auth_handler.go.tmpl`), `middleware.CSRF(next http.Handler) http.Handler` (existing, exported), `models.NewUserModel`/`.Create`/`.RecordFailedAttempt` (existing, `user_model.go.tmpl`), `clientIP(r *http.Request) string` reads `CF-Connecting-IP` header (existing, `auth_handler.go.tmpl`).
- Produces: package-level `authTestSchema` constant and `loginBody` helper — Tasks 6 and 7's templates (same `handlers` package) reuse both.

- [ ] **Step 1: Write the failing test**

```go
// src/builder/render_test.go — add this function
func TestAuthTestTemplate_IsValidGo(t *testing.T) {
	data := newData("user", nil)
	renderAndParse(t, "auth_test.go.tmpl", data)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -run TestAuthTestTemplate -v"`
Expected: FAIL — template file does not exist

- [ ] **Step 3: Write minimal implementation**

```go
// src/builder/templates/auth_test.go.tmpl
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gova/app/cache"
	"gova/app/db"
	"gova/app/middleware"
	"gova/app/models"
)

const authTestSchema = `
CREATE TABLE users (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE rate_limits (
	ip TEXT NOT NULL,
	attempts INTEGER DEFAULT 0,
	locked_until DATETIME,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (ip)
);`

func loginBody(email, password string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{"email": email, "password": password})
	return bytes.NewBuffer(b)
}

func TestLoginPOST_Success(t *testing.T) {
	testDB := db.OpenTest(t, authTestSchema)
	appCache := cache.New()
	userModel := models.NewUserModel(testDB.Read, testDB.Write, appCache)
	if _, err := userModel.Create("Ada", "ada@example.com", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody("ada@example.com", "correct-horse-battery-staple"))
	rec := httptest.NewRecorder()

	LoginPOST(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("expected gova_session cookie to be set on success")
	}
}

func TestLoginPOST_WrongPassword(t *testing.T) {
	testDB := db.OpenTest(t, authTestSchema)
	appCache := cache.New()
	userModel := models.NewUserModel(testDB.Read, testDB.Write, appCache)
	if _, err := userModel.Create("Ada", "ada@example.com", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody("ada@example.com", "wrong-password"))
	rec := httptest.NewRecorder()

	LoginPOST(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLoginPOST_RateLimited(t *testing.T) {
	testDB := db.OpenTest(t, authTestSchema)
	appCache := cache.New()
	userModel := models.NewUserModel(testDB.Read, testDB.Write, appCache)
	if _, err := userModel.Create("Ada", "ada@example.com", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// Drive the rate limiter directly through its own recording method
	// rather than 5 real HTTP round trips against a real clock.
	for i := 0; i < 5; i++ {
		userModel.RecordFailedAttempt("203.0.113.1")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody("ada@example.com", "correct-horse-battery-staple"))
	req.Header.Set("CF-Connecting-IP", "203.0.113.1")
	rec := httptest.NewRecorder()

	LoginPOST(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want %d, body: %s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
}

func TestLoginPOST_CSRFRejectsMismatchedToken(t *testing.T) {
	testDB := db.OpenTest(t, authTestSchema)
	appCache := cache.New()
	userModel := models.NewUserModel(testDB.Read, testDB.Write, appCache)
	if _, err := userModel.Create("Ada", "ada@example.com", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	protected := middleware.CSRF(LoginPOST(testDB.Read, testDB.Write, appCache))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody("ada@example.com", "correct-horse-battery-staple"))
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "server-issued-token"})
	req.Header.Set("X-CSRF-Token", "attacker-guessed-token")
	rec := httptest.NewRecorder()

	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want %d, body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
```

In `handleScaffoldAuth`'s `specs` slice, add:

```go
		{"auth_test.go.tmpl", "/src/app/handlers/auth_test.go"},
```

right after the existing `{"auth_handler.go.tmpl", ...}` entry.

- [ ] **Step 4: Run test to verify it passes**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -v"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/builder/templates/auth_test.go.tmpl src/builder/main.go src/builder/render_test.go
git commit -m "feat: generate login/rate-limit/CSRF tests from scaffold_auth"
```

---

### Task 6: Registration test template

**Files:**
- Create: `src/builder/templates/register_test.go.tmpl`
- Modify: `src/builder/main.go` (wire into `handleScaffoldRegistration`)
- Modify: `src/builder/render_test.go` (add sanity test)

**Interfaces:**
- Consumes: `RegisterPOST(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc` (existing, `register_handler.go.tmpl`), `authTestSchema` (Task 5 — same `handlers` package; `scaffold_registration`'s tool description already requires `scaffold_auth` to have run first, so this ordering dependency is already enforced by the existing tool contract, not new).

- [ ] **Step 1: Write the failing test**

```go
// src/builder/render_test.go — add this function
func TestRegisterTestTemplate_IsValidGo(t *testing.T) {
	data := newData("user", nil)
	renderAndParse(t, "register_test.go.tmpl", data)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -run TestRegisterTestTemplate -v"`
Expected: FAIL — template file does not exist

- [ ] **Step 3: Write minimal implementation**

```go
// src/builder/templates/register_test.go.tmpl
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gova/app/cache"
	"gova/app/db"
	"gova/app/models"
)

func registerBody(name, email, password string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{"name": name, "email": email, "password": password})
	return bytes.NewBuffer(b)
}

func TestRegisterPOST_Success(t *testing.T) {
	testDB := db.OpenTest(t, authTestSchema)
	appCache := cache.New()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", registerBody("Ada", "Ada@Example.com", "correct-horse-battery-staple"))
	rec := httptest.NewRecorder()

	RegisterPOST(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	userModel := models.NewUserModel(testDB.Read, testDB.Write, appCache)
	found, err := userModel.FindByEmail("ada@example.com")
	if err != nil {
		t.Fatalf("expected user findable by lowercased email: %v", err)
	}
	if found.Email != "ada@example.com" {
		t.Errorf("stored email: got %q, want lowercased %q", found.Email, "ada@example.com")
	}
}

func TestRegisterPOST_DuplicateEmailRejected(t *testing.T) {
	testDB := db.OpenTest(t, authTestSchema)
	appCache := cache.New()
	userModel := models.NewUserModel(testDB.Read, testDB.Write, appCache)
	if _, err := userModel.Create("Ada", "ada@example.com", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", registerBody("Ada Two", "ada@example.com", "another-password"))
	rec := httptest.NewRecorder()

	RegisterPOST(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want %d, body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}
```

In `handleScaffoldRegistration`'s `specs` slice, add:

```go
		{"register_test.go.tmpl", "/src/app/handlers/register_test.go"},
```

right after the existing `{"register_handler.go.tmpl", ...}` entry.

- [ ] **Step 4: Run test to verify it passes**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -v"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/builder/templates/register_test.go.tmpl src/builder/main.go src/builder/render_test.go
git commit -m "feat: generate registration tests from scaffold_registration"
```

---

### Task 7: Mobile auth test template

**Files:**
- Create: `src/builder/templates/mobile_auth_test.go.tmpl`
- Modify: `src/builder/main.go` (wire into `handleScaffoldMobileAuth`)
- Modify: `src/builder/render_test.go` (add sanity test)

**Interfaces:**
- Consumes: `MobileLoginPOST`/`MobileMeGET(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc` (existing, `mobile_auth_handler.go.tmpl`), `authTestSchema` (Task 5 — `scaffold_mobile_auth`'s tool description already requires `scaffold_auth` to have run first for the `users` table to exist).

- [ ] **Step 1: Write the failing test**

```go
// src/builder/render_test.go — add this function
func TestMobileAuthTestTemplate_IsValidGo(t *testing.T) {
	renderAndParse(t, "mobile_auth_test.go.tmpl", TemplateData{})
}
```

(uses bare `TemplateData{}`, matching how `handleScaffoldMobileAuth` already calls `renderToFile("mobile_auth_handler.go.tmpl", outPath, TemplateData{})`)

- [ ] **Step 2: Run test to verify it fails**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -run TestMobileAuthTestTemplate -v"`
Expected: FAIL — template file does not exist

- [ ] **Step 3: Write minimal implementation**

```go
// src/builder/templates/mobile_auth_test.go.tmpl
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gova/app/cache"
	"gova/app/db"
	"gova/app/models"
)

const mobileAuthTestSchema = authTestSchema + `
CREATE TABLE mobile_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	expires_at DATETIME NOT NULL
);`

func TestMobileLoginPOST_IssuesToken(t *testing.T) {
	testDB := db.OpenTest(t, mobileAuthTestSchema)
	appCache := cache.New()
	userModel := models.NewUserModel(testDB.Read, testDB.Write, appCache)
	if _, err := userModel.Create("Ada", "ada@example.com", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login_token", loginBody("ada@example.com", "correct-horse-battery-staple"))
	rec := httptest.NewRecorder()

	MobileLoginPOST(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data.Token) != 64 {
		t.Errorf("token: got length %d, want 64 (32 bytes hex-encoded)", len(resp.Data.Token))
	}
}

func TestMobileMeGET_ValidatesBearerToken(t *testing.T) {
	testDB := db.OpenTest(t, mobileAuthTestSchema)
	appCache := cache.New()
	userModel := models.NewUserModel(testDB.Read, testDB.Write, appCache)
	if _, err := userModel.Create("Ada", "ada@example.com", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login_token", loginBody("ada@example.com", "correct-horse-battery-staple"))
	loginRec := httptest.NewRecorder()
	MobileLoginPOST(testDB.Read, testDB.Write, appCache)(loginRec, loginReq)
	var loginResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me_token", nil)
	meReq.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	meRec := httptest.NewRecorder()

	MobileMeGET(testDB.Read, testDB.Write, appCache)(meRec, meReq)

	if meRec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", meRec.Code, http.StatusOK, meRec.Body.String())
	}
}

func TestMobileMeGET_RejectsMissingBearerToken(t *testing.T) {
	testDB := db.OpenTest(t, mobileAuthTestSchema)
	appCache := cache.New()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me_token", nil)
	rec := httptest.NewRecorder()

	MobileMeGET(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileLoginPOST_RateLimited(t *testing.T) {
	testDB := db.OpenTest(t, mobileAuthTestSchema)
	appCache := cache.New()
	userModel := models.NewUserModel(testDB.Read, testDB.Write, appCache)
	if _, err := userModel.Create("Ada", "ada@example.com", "correct-horse-battery-staple"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	for i := 0; i < 5; i++ {
		userModel.RecordFailedAttempt("203.0.113.5")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login_token", loginBody("ada@example.com", "correct-horse-battery-staple"))
	req.Header.Set("CF-Connecting-IP", "203.0.113.5")
	rec := httptest.NewRecorder()

	MobileLoginPOST(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want %d, body: %s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
}
```

In `handleScaffoldMobileAuth`, after the existing `renderToFile("mobile_auth_handler.go.tmpl", outPath, TemplateData{})` call (in the non-idempotent-skip branch, before the final `return`), add:

```go
	testPath := "/src/app/handlers/mobile_auth_test.go"
	if err := renderToFile("mobile_auth_test.go.tmpl", testPath, TemplateData{}); err != nil {
		return errResult(err.Error()), nil
	}
	results = append(results, "Created: "+testPath)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -v"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/builder/templates/mobile_auth_test.go.tmpl src/builder/main.go src/builder/render_test.go
git commit -m "feat: generate mobile bearer-token tests from scaffold_mobile_auth"
```

---

### Task 8: Fold test-writing into gova-writing-plans for bespoke stubs

**Files:**
- Modify: `.claude/skills/gova-writing-plans/SKILL.md`

**Interfaces:**
- None (documentation-only task; no code interface).

- [ ] **Step 1: Add a test-writing step to the Task Structure template**

In the `## Task Structure` section, the example step list currently ends at "Step 5: Commit". Insert a new step before it, so hand-customized code (bespoke `create_handler`/`create_page` stubs, or a scaffolded handler a task customizes beyond its generated behavior) gets a test planned alongside the code:

Replace:

```markdown
- [ ] **Step 3: Customize**

[Exact edits needed — show the code, not a description of the code]

- [ ] **Step 4: Restart and verify**
```

With:

```markdown
- [ ] **Step 3: Customize**

[Exact edits needed — show the code, not a description of the code]

- [ ] **Step 3b: Write a test for the custom behavior** (only if this task hand-writes logic beyond the scaffold — a bespoke `create_handler`/`create_page` stub, or a scaffolded handler customized past its generated behavior; generated CRUD/auth code already has tests from the scaffold call itself)

[Exact test code — same `_test.go` file convention as the generated tests: `httptest` against the handler, `db.OpenTest` for any db-touching test]

- [ ] **Step 4: Restart and verify**
```

- [ ] **Step 2: Update the "Remember" section**

Replace:

```markdown
## Remember
- Exact file paths always
- Complete code in every step — if a step changes code, show the code
- Exact MCP tool calls with exact arguments
- DRY, YAGNI, frequent commits
- Every feature task starts with an MCP scaffold call — never "implement X handler" as a first step
```

With:

```markdown
## Remember
- Exact file paths always
- Complete code in every step — if a step changes code, show the code
- Exact MCP tool calls with exact arguments
- DRY, YAGNI, frequent commits
- Every feature task starts with an MCP scaffold call — never "implement X handler" as a first step
- Generated CRUD/auth code already has tests from its scaffold call — only plan a test-writing step for hand-customized logic (Step 3b)
```

- [ ] **Step 3: Commit**

```bash
git add .claude/skills/gova-writing-plans/SKILL.md
git commit -m "docs: fold test-writing into gova-writing-plans for hand-customized code"
```

---

### Task 9: Wire `go test` into build.md and gova-build-execution verification

**Files:**
- Modify: `.claude/commands/build.md`
- Modify: `.claude/skills/gova-build-execution/implementer-prompt.md`
- Modify: `.claude/skills/gova-build-execution/task-reviewer-prompt.md`

**Interfaces:**
- None (documentation-only task).

- [ ] **Step 1: Add a Tests check to build.md Step 8**

In `## Step 8: Pre-Completion Verification`, the bullet list currently has no test-suite line (it predates this plan). Add one, right after the `**Architecture:**` bullet:

```markdown
- **Tests:** Run `docker compose exec app go test ./...` now and read the output — all passing? A failing test blocks completion the same as a failing build.
```

- [ ] **Step 2: Update implementer-prompt.md's "Your Job" and "Report Format"**

In `## Your Job`, replace:

```markdown
    3. Verify: `docker compose restart app`, check `docker compose logs app`
       for errors, confirm the page/endpoint behaves as specified
```

With:

```markdown
    3. Verify: `docker compose restart app`, check `docker compose logs app`
       for errors, confirm the page/endpoint behaves as specified, and run
       `docker compose exec app go test ./...` — all passing?
```

In `## Report Format`, replace:

```markdown
    - What you verified and how (restart output, logs, manual check)
```

With:

```markdown
    - What you verified and how (restart output, logs, manual check,
      `go test` output — paste the pass/fail summary line)
```

- [ ] **Step 3: Update task-reviewer-prompt.md's Verification section**

Replace the entire `## Verification` section:

```markdown
    ## Verification

    There is no test suite in this stack. The implementer already restarted
    the app and reported log/behavior evidence for exactly this code. Do not
    re-run `docker compose restart` to confirm their report. If reading the
    code raises a specific doubt no existing evidence answers, name it in
    your report as something the controller should verify manually.
```

With:

```markdown
    ## Verification

    The implementer already restarted the app and ran `go test ./...`,
    reporting the pass/fail summary for exactly this code. Do not re-run
    `docker compose restart` or the full suite to confirm their report — if a
    specific line in the diff raises a doubt no existing evidence answers,
    run only the focused test that answers it (`go test ./handlers/... -run
    TestName -v`), never the whole suite. A missing or skipped test for
    hand-customized logic (see gova-writing-plans Step 3b) is a spec-gap
    finding, not a quality nit — generated scaffold code has tests from its
    scaffold call; hand-written logic without a test does not meet the plan.
```

- [ ] **Step 4: Commit**

```bash
git add .claude/commands/build.md .claude/skills/gova-build-execution/implementer-prompt.md .claude/skills/gova-build-execution/task-reviewer-prompt.md
git commit -m "docs: wire go test into build.md and gova-build-execution verification"
```

---

### Task 10: End-to-end smoke verification and TODO.md cleanup

**Files:**
- None created — this task proves Tasks 1-9 work together against the real running system, then updates `TODO.md`.

**Interfaces:**
- Consumes: everything from Tasks 1-9.

- [ ] **Step 1: Rebuild the images with the new builder code**

Run: `docker compose up -d --build`
Expected: both `app` and `mcp` containers report `Up`.

- [ ] **Step 2: Scaffold a throwaway resource through the real MCP tool**

Using the `gova-builder` MCP tools in this session: call `execute_sql` to create a `widgets` table (`CREATE TABLE widgets (id INTEGER PRIMARY KEY, title TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`), then `scaffold_list(name='widget', fields=['title:string'])`.

Expected output includes `Created: /src/app/models/Widget_test.go` and `Created: /src/app/handlers/widget_list_test.go` alongside the existing four files.

- [ ] **Step 3: Scaffold auth + registration + mobile auth through the real MCP tools**

Call `scaffold_auth`, then `scaffold_registration`, then `scaffold_mobile_auth`.

Expected output includes `Created: /src/app/handlers/auth_test.go`, `Created: /src/app/handlers/register_test.go`, `Created: /src/app/handlers/mobile_auth_test.go`.

- [ ] **Step 4: Run the full backend suite for real**

Run: `docker compose exec app go test ./... -v`
Expected: PASS — every test from Tasks 1, 3, 4, 5, 6, 7 (`TestOpenTest_WriteVisibleToRead`, `TestWidgetModel_CRUD`, `TestWidgetListGET`, `TestLoginPOST_*`, `TestRegisterPOST_*`, `TestMobileLoginPOST_*`, `TestMobileMeGET_*`) passes against the real generated code, not just the render-validity harness.

- [ ] **Step 5: Also run the builder's own suite**

Run: `docker compose exec app sh -c "cd /src/builder && go test ./... -v"`
Expected: PASS.

- [ ] **Step 6: Clean up the scaffolded throwaway resource**

This repo's own `src/app/` is the template — it ships empty (aside from infra). The `widget`/auth files scaffolded in Steps 2-3 were only there to prove the templates work end-to-end; they don't belong in the committed template.

```bash
git status --short src/app/
```

Expected: shows only newly-created, untracked files under `src/app/models/`, `src/app/handlers/`, `src/app/static/` from Steps 2-3 (nothing from Tasks 1-9, which only touched `src/builder/`, `src/app/db/`, and the `.claude/` skill/command files already committed in their own tasks).

```bash
git clean -fd src/app/models/ src/app/handlers/ src/app/static/
```

Then run `execute_sql` again to `DROP TABLE widgets; DROP TABLE users; DROP TABLE rate_limits; DROP TABLE mobile_tokens;` to leave `/data/app.db` clean too.

Expected: `git status --short src/app/` now shows nothing.

- [ ] **Step 7: Replace the TODO.md placeholder**

```markdown
# TODO
```

(empty — the backend test suite item is now shipped, not pending)

- [ ] **Step 8: Commit**

```bash
git add TODO.md
git commit -m "chore: backend test suite shipped, clear TODO.md placeholder"
```

---

## Self-Review

**1. Spec coverage:** Every Component in `docs/superpowers/specs/2026-07-13-backend-test-suite-design.md` maps to a task — `testutil.go` (Task 1), the five `_test.go.tmpl` files (Tasks 3-7), the `gova-writing-plans` update (Task 8), the `build.md`/`gova-build-execution` wiring (Task 9), and `TODO.md` (Task 10). The design's `render_test.go`-equivalent harness (implied by "verified against sample data" in discussion) is made explicit as Task 2, since Tasks 3-7 all depend on it existing first.

**2. Placeholder scan:** No TBD/TODO/"add appropriate" language in any step; every code block is complete, runnable Go or exact file diffs.

**3. Type consistency:** `db.OpenTest(t *testing.T, schema string) *DB` (Task 1) is called identically in every later task. `sqlType`/`testArgs` (Task 3) are reused verbatim by Task 4 (same `funcMap`, no redefinition). `authTestSchema`/`loginBody` (Task 5) are reused verbatim by Tasks 6 and 7 via same-package visibility — confirmed no naming collisions, since `scaffold_registration`/`scaffold_mobile_auth` both already require `scaffold_auth` to run first per their existing tool descriptions.

**4. CRUD completeness:** N/A — this plan's deliverable is tests, not a CRUD feature; the model/handler tests it generates already cover Create/Find/GetAll/Delete per Task 3/4.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-13-backend-test-suite.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
