# Manifest Full-Contract (Build B-emit) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use gova-build-execution to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make gova-monolith emit a complete API contract in `api.json` — request/response body schemas, semantic format hints, and foreign-key relationships — and serve it faithfully at `/api/v1/_manifest`.

**Architecture:** Extend the builder's manifest types with a lightweight `BodySchema` and two additive `ModelField` hints (`format`, `references`). The `fields` DSL gains semantic tokens (`datetime`/`date`/`time`/`json`/`email`) and `ref:<model>`, folded into a **base storage type + metadata** so all existing code-gen helpers are untouched. Scaffold tools derive request/response schemas from model+kind; `create_handler` authors declare theirs. Generated create/update echo the full written object. The app's read-only manifest mirror is extended so the served endpoint matches the file.

**Tech Stack:** GOVA Monolith — Go/chi, SQLite, vanilla JS, Tailwind. **This build modifies the builder/factory internals (`src/builder/*`) and one app-side reader (`src/app/handlers/manifest.go`) — not app-feature files.** The "MCP-scaffold-tool-first" rule does not apply; these are generator internals and infrastructure. Verification = `go test` in `src/builder` and `src/app`, plus a rebuilt-mcp-image live scaffold.

## Global Constraints

- **Body-schema shape is closed:** `{shape: "object"|"list"|"empty", model?: string, fields?: []ModelField}`. Not JSON Schema.
- **Semantic tokens fold into base type + metadata.** `datetime|date|time|json|email` → `Type:"string"` + `Format:<hint>`; `ref:<model>` → `Type:"int"` + `Ref:<model>`. The manifest `Type` stays within the existing closed set `{int,string,boolean,float,timestamp}` (plus `password`→`string` in the model). **Do not add new values to `goTypeFor`, `expectedSQLType`, `sqlType`, `nullTypeFor`, scan/test helpers — they must not need changes.** If one seems to, you've mis-mapped the base type.
- **`ref:<model>` is validated:** the referenced model must already exist in `api.json`, else the tool errors and writes nothing (parent scaffolded before child).
- **Auth endpoints get NO request/response schemas** (fixed contract, already consumed by pre-committed Swift). Leave `Request`/`Response` nil for auth-kind endpoints.
- **Echo:** generated create/update return the full written object via the model's `Find`, not `{id}`/`{ok}`.
- **Wire contract unchanged:** still `{ok,data,...}` envelope via `jsonOK`/`jsonList`. RFC3339 `models.Time` untouched.
- **mcp image is `go:embed`-ed at image-build time:** after any `src/builder` change, `docker compose up -d --build` (not restart). App-side (`src/app`) changes need only `docker compose restart app`.
- **Determinism:** `manifestHash` marshals models+endpoints; the new fields flow into the hash automatically — every schema change becomes a hash change. Do not exclude them.

---

### Task 1: Manifest + Field types and the DSL

**Files:**
- Modify: `src/builder/manifest.go` — add `BodySchema`; extend `ModelField` and `Endpoint`; extend `fieldsToModel`; add `parseBodySchemaArg` + `validateRefsAt`.
- Modify: `src/builder/main.go` — extend `Field`; add `semanticFormats`; rewrite `parseFields`.
- Create/extend: `src/builder/manifest_test.go` — round-trip + fieldsToModel + validateRefs tests. `src/builder/parse_test.go` (new) — parseFields cases.

**Interfaces:**
- Produces: `BodySchema` type; `ModelField.Format`/`.References`; `Endpoint.Summary`/`.Request`/`.Response`; `Field.Format`/`.Ref`; `parseBodySchemaArg`, `validateRefsAt`. Tasks 2 and 4 consume these.
- Consumes: nothing new.

- [ ] **Step 1: Extend the manifest types** (`src/builder/manifest.go`)

Replace the `ModelField` and `Endpoint` structs and add `BodySchema` above `Endpoint`:

```go
type ModelField struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	Format     string `json:"format,omitempty"`     // semantic hint: datetime-local, date, time, json, email
	References string `json:"references,omitempty"`  // FK target model name
}

// BodySchema is the closed shape describing an endpoint's request or response
// body. Either Model (fields inherited from that model) or Fields (inline) is
// set, never both for a given schema.
type BodySchema struct {
	Shape  string       `json:"shape"`            // "object" | "list" | "empty"
	Model  string       `json:"model,omitempty"`
	Fields []ModelField `json:"fields,omitempty"`
}

type Endpoint struct {
	Method   string      `json:"method"`
	Path     string      `json:"path"`
	Handler  string      `json:"handler"`
	Deps     []string    `json:"deps"`
	Auth     bool        `json:"auth"`
	Model    string      `json:"model,omitempty"`
	Kind     string      `json:"kind"`
	Summary  string      `json:"summary,omitempty"`
	Request  *BodySchema `json:"request,omitempty"`
	Response *BodySchema `json:"response,omitempty"`
}
```

- [ ] **Step 2: Carry Format/References in `fieldsToModel`** (`src/builder/manifest.go`)

Replace the per-field append in `fieldsToModel` so it copies the new metadata:

```go
func fieldsToModel(name, table string, fields []Field) Model {
	out := make([]ModelField, 0, len(fields)+2)
	out = append(out, ModelField{Name: "id", Type: "int", Nullable: false})
	for _, f := range fields {
		typ := f.Type
		if typ == "password" {
			typ = "string"
		}
		out = append(out, ModelField{
			Name: f.Name, Type: typ, Nullable: f.Nullable,
			Format: f.Format, References: f.Ref,
		})
	}
	out = append(out, ModelField{Name: "created_at", Type: "timestamp", Nullable: false})
	return Model{Name: name, Table: table, Fields: out}
}
```

- [ ] **Step 3: Add `parseBodySchemaArg` and `validateRefsAt`** (`src/builder/manifest.go`, which already imports `encoding/json` and `fmt`)

```go
// parseBodySchemaArg parses a create_handler schema argument. Empty input means
// "no schema declared" (nil, nil). A non-empty value must be valid JSON with a
// recognized shape.
func parseBodySchemaArg(raw string) (*BodySchema, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var bs BodySchema
	if err := json.Unmarshal([]byte(raw), &bs); err != nil {
		return nil, fmt.Errorf("invalid schema JSON: %w", err)
	}
	switch bs.Shape {
	case "object", "list", "empty":
	default:
		return nil, fmt.Errorf("schema shape must be object|list|empty, got %q", bs.Shape)
	}
	return &bs, nil
}

// validateRefsAt fails if any field references a model not yet in the manifest.
// A dangling reference is a stated-fact violation — the parent must be
// scaffolded before its child.
func validateRefsAt(apiPath string, fields []Field) error {
	need := false
	for _, f := range fields {
		if f.Ref != "" {
			need = true
		}
	}
	if !need {
		return nil
	}
	m, err := readManifestAt(apiPath)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(m.Models))
	for _, mm := range m.Models {
		known[mm.Name] = true
	}
	for _, f := range fields {
		if f.Ref != "" && !known[f.Ref] {
			return fmt.Errorf("field %q references model %q which is not scaffolded yet — scaffold the parent resource first", f.Name, f.Ref)
		}
	}
	return nil
}

// validateRefs is the production entry point (against the live manifest path).
func validateRefs(fields []Field) error { return validateRefsAt(manifestFilePath, fields) }
```

(Confirm `strings` is imported in `manifest.go` — it is, per the existing `strings.Join` usage.)

- [ ] **Step 4: Extend `Field` and rewrite `parseFields`** (`src/builder/main.go`)

Replace the `Field` struct and `parseFields`:

```go
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
```

- [ ] **Step 5: Tests** — `src/builder/parse_test.go` (new) and additions to `src/builder/manifest_test.go`

`parse_test.go`:

```go
package main

import "testing"

func TestParseFields_Semantic(t *testing.T) {
	got := parseFields([]string{"title:string", "remind_at:datetime", "due:date", "meta:json"})
	if got[1].Type != "string" || got[1].Format != "datetime-local" {
		t.Errorf("remind_at: got type=%q format=%q, want string/datetime-local", got[1].Type, got[1].Format)
	}
	if got[2].Format != "date" {
		t.Errorf("due: got format=%q, want date", got[2].Format)
	}
	if got[3].Format != "json" {
		t.Errorf("meta: got format=%q, want json", got[3].Format)
	}
	if got[0].Format != "" {
		t.Errorf("plain string field must have no format, got %q", got[0].Format)
	}
}

func TestParseFields_Ref(t *testing.T) {
	got := parseFields([]string{"category_id:ref:log_category"})
	if got[0].Type != "int" || got[0].Ref != "log_category" {
		t.Errorf("ref: got type=%q ref=%q, want int/log_category", got[0].Type, got[0].Ref)
	}
}
```

Add to `manifest_test.go`:

```go
func TestFieldsToModel_CarriesFormatAndRef(t *testing.T) {
	fields := []Field{
		{Name: "remind_at", Type: "string", Format: "datetime-local"},
		{Name: "category_id", Type: "int", Ref: "log_category"},
	}
	m := fieldsToModel("reminder", "reminders", fields)
	// id, remind_at, category_id, created_at
	if m.Fields[1].Format != "datetime-local" {
		t.Errorf("format not carried: %q", m.Fields[1].Format)
	}
	if m.Fields[2].References != "log_category" {
		t.Errorf("references not carried: %q", m.Fields[2].References)
	}
}

func TestValidateRefsAt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "api.json")
	os.WriteFile(p, []byte(`{"api_version":"1.0.0","models":[{"name":"log_category","table":"log_categories","fields":[]}],"endpoints":[]}`), 0644)
	if err := validateRefsAt(p, []Field{{Name: "category_id", Type: "int", Ref: "log_category"}}); err != nil {
		t.Errorf("known ref should pass: %v", err)
	}
	if err := validateRefsAt(p, []Field{{Name: "x_id", Type: "int", Ref: "nope"}}); err == nil {
		t.Error("unknown ref should fail")
	}
	if err := validateRefsAt(p, []Field{{Name: "title", Type: "string"}}); err != nil {
		t.Errorf("no refs should pass: %v", err)
	}
}
```

(Ensure `manifest_test.go` imports `os`, `path/filepath`, `testing` — add any missing.)

- [ ] **Step 6: Build and test**

Run: `docker compose exec -T app sh -c 'cd /src/builder && go test ./...'`
Expect: green, including the new parse/manifest tests. No changes to `goTypeFor`/`schema.go`/render tests should be needed by this task (Type stays in the existing set).

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat(builder): manifest types + fields DSL for formats and refs

BodySchema type; ModelField.format/references; Endpoint.summary/request/response.
fields DSL gains datetime/date/time/json/email (-> format) and ref:<model>
(-> references), folded into base type + metadata so code-gen helpers are
untouched. Adds parseBodySchemaArg and ref validation.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Derive and attach body schemas to scaffolded endpoints

**Files:**
- Modify: `src/builder/manifest.go` — add `writableFields`, `resourceRequest`, `resourceResponse`; change `resourceEndpoints(name)` → `resourceEndpoints(m Model)`.
- Modify: `src/builder/main.go` — `handleScaffoldResource` passes the model and calls `validateRefs`; `handleScaffoldList` attaches a list response and calls `validateRefs`.
- Modify: `src/builder/manifest_test.go` — assert derived schemas per kind.

**Interfaces:**
- Consumes: `BodySchema`, `Model`, `validateRefs` (Task 1).
- Produces: `resourceEndpoints(Model)` now carries request/response. Task 6 verifies live.

- [ ] **Step 1: Derivation helpers** (`src/builder/manifest.go`)

```go
// writableFields is a model's fields minus the auto columns id and created_at —
// the body a client sends on create/update.
func writableFields(m Model) []ModelField {
	out := make([]ModelField, 0, len(m.Fields))
	for _, f := range m.Fields {
		if f.Name == "id" || f.Name == "created_at" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func resourceRequest(m Model, kind string) *BodySchema {
	switch kind {
	case "create", "update":
		return &BodySchema{Shape: "object", Fields: writableFields(m)}
	default:
		return nil
	}
}

func resourceResponse(m Model, kind string) *BodySchema {
	switch kind {
	case "list":
		return &BodySchema{Shape: "list", Model: m.Name}
	case "detail", "create", "update":
		return &BodySchema{Shape: "object", Model: m.Name}
	case "delete":
		return &BodySchema{Shape: "object", Fields: []ModelField{{Name: "ok", Type: "boolean"}}}
	default:
		return nil
	}
}
```

- [ ] **Step 2: `resourceEndpoints` takes the model and attaches schemas** (`src/builder/manifest.go`)

Replace `resourceEndpoints`:

```go
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
```

- [ ] **Step 3: Wire `handleScaffoldResource`** (`src/builder/main.go`)

After `fields, applyErr := applySchema(...)` and its error check, add the ref validation; then change the endpoint construction to pass the model. The relevant tail of `handleScaffoldResource` becomes:

```go
	if err := validateRefs(fields); err != nil {
		return errResult(err.Error()), nil
	}
	data := newData(name, fields)
	data.CRUD = true
	data.Title = toPascal(toPlural(name))

	// ... (fileSpec rendering loop unchanged) ...

	model := fieldsToModel(name, toPlural(name), fields)
	if err := updateManifest([]Model{model}, resourceEndpoints(model)); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}
```

(Only two edits: insert the `validateRefs` block after the `applySchema` check, and change `resourceEndpoints(name)` → `resourceEndpoints(model)`.)

- [ ] **Step 4: Wire `handleScaffoldList`** (`src/builder/main.go`)

Insert the same `validateRefs(fields)` block after `applySchema`, and give the list endpoint a response schema:

```go
	model := fieldsToModel(name, toPlural(name), fields)
	endpoint := Endpoint{
		Method: "GET", Path: "/api/v1/" + toPlural(name),
		Handler: toPascal(name) + "ListGET",
		Deps:    []string{"read", "write", "cache"},
		Auth:    false, Model: name, Kind: "list",
		Response: resourceResponse(model, "list"),
	}
	if err := updateManifest([]Model{model}, []Endpoint{endpoint}); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}
```

- [ ] **Step 5: Tests** — add to `src/builder/manifest_test.go`

```go
func TestResourceEndpoints_Schemas(t *testing.T) {
	m := fieldsToModel("reminder", "reminders", []Field{
		{Name: "title", Type: "string"},
		{Name: "remind_at", Type: "string", Format: "datetime-local"},
	})
	eps := resourceEndpoints(m)
	byKind := map[string]Endpoint{}
	for _, e := range eps {
		byKind[e.Kind] = e
	}
	// create: request is writable fields (no id/created_at), response is the object.
	cr := byKind["create"]
	if cr.Request == nil || cr.Request.Shape != "object" {
		t.Fatalf("create request shape: %+v", cr.Request)
	}
	for _, f := range cr.Request.Fields {
		if f.Name == "id" || f.Name == "created_at" {
			t.Errorf("create request must not include auto column %q", f.Name)
		}
	}
	if cr.Response == nil || cr.Response.Model != "reminder" || cr.Response.Shape != "object" {
		t.Errorf("create response should be object/model reminder: %+v", cr.Response)
	}
	// list response is a list of the model; delete response is {ok}.
	if byKind["list"].Response.Shape != "list" {
		t.Errorf("list response shape: %+v", byKind["list"].Response)
	}
	if byKind["delete"].Response.Fields[0].Name != "ok" {
		t.Errorf("delete response should be {ok}: %+v", byKind["delete"].Response)
	}
	// format hint survives into the create request body.
	var sawFmt bool
	for _, f := range cr.Request.Fields {
		if f.Name == "remind_at" && f.Format == "datetime-local" {
			sawFmt = true
		}
	}
	if !sawFmt {
		t.Error("create request lost the datetime-local format hint")
	}
}
```

- [ ] **Step 6: Build and test**

Run: `docker compose exec -T app sh -c 'cd /src/builder && go test ./...'` — green.

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat(builder): scaffolded endpoints carry derived request/response schemas

resourceEndpoints derives per-kind body schemas from the model; scaffold_list
gains a list response; both validate ref fields against existing models.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Create/update echo the full written object

**Files:**
- Modify: `src/builder/templates/resource_handlers.go.tmpl` — create/update return the object via `Find`.
- Modify: `src/builder/templates/resource_handlers_test.go.tmpl` — assert the echoed object.
- Modify: `src/builder/render_test.go` — update any golden expectation that asserts the old `{id}`/`{ok}` create/update bodies.

**Interfaces:**
- Consumes: the model's existing `Find(id)` (present on every generated model, CRUD or not).
- Produces: create/update responses are `{shape:object, model}` — matching Task 2's declared response.

- [ ] **Step 1: Echo in create** (`resource_handlers.go.tmpl`)

Replace the body of `{{.PascalName}}CreatePOST`'s success path — change the final lines from:

```go
		id, err := model.Create({{structCallArgs .Fields "req."}})
		if err != nil {
			jsonError(w, "failed to create", 500)
			return
		}
		jsonOK(w, map[string]int64{"id": id})
```

to:

```go
		id, err := model.Create({{structCallArgs .Fields "req."}})
		if err != nil {
			jsonError(w, "failed to create", 500)
			return
		}
		item, err := model.Find(id)
		if err != nil {
			jsonError(w, "created but failed to load", 500)
			return
		}
		jsonOK(w, item)
```

- [ ] **Step 2: Echo in update** (`resource_handlers.go.tmpl`)

In `{{.PascalName}}UpdatePUT`, replace the success tail — from:

```go
		if err := model.Update(id, {{structCallArgs .Fields "req."}}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				jsonError(w, "not found", 404)
				return
			}
			jsonError(w, "failed to update", 500)
			return
		}
		jsonOK(w, map[string]int64{"id": id})
```

to:

```go
		if err := model.Update(id, {{structCallArgs .Fields "req."}}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				jsonError(w, "not found", 404)
				return
			}
			jsonError(w, "failed to update", 500)
			return
		}
		item, err := model.Find(id)
		if err != nil {
			jsonError(w, "updated but failed to load", 500)
			return
		}
		jsonOK(w, item)
```

- [ ] **Step 3: Assert the echo in the generated test** (`resource_handlers_test.go.tmpl`)

Change the create and update assertions to require the echoed object. `created_at` is present in the full object and never in the old `{id}` map, so it is a mutation-proof marker. Replace:

```go
	// Create.
	if rec := do(http.MethodPost, "/api/v1/{{.PluralName}}", `{{testJSON .Fields}}`); rec.Code != 200 {
		t.Errorf("create: got %d, body %s", rec.Code, rec.Body.String())
	}

	// Update: existing and missing.
	if rec := do(http.MethodPut, "/api/v1/{{.PluralName}}/1", `{{testJSON .Fields}}`); rec.Code != 200 {
		t.Errorf("update: got %d, body %s", rec.Code, rec.Body.String())
	}
```

with:

```go
	// Create: echoes the full written object (created_at only appears on the object).
	if rec := do(http.MethodPost, "/api/v1/{{.PluralName}}", `{{testJSON .Fields}}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"created_at"`) {
		t.Errorf("create: want 200 with echoed object, got %d, body %s", rec.Code, rec.Body.String())
	}

	// Update: echoes the full written object.
	if rec := do(http.MethodPut, "/api/v1/{{.PluralName}}/1", `{{testJSON .Fields}}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"created_at"`) {
		t.Errorf("update: want 200 with echoed object, got %d, body %s", rec.Code, rec.Body.String())
	}
```

(`strings` is already imported in this template's output.)

- [ ] **Step 4: Update `render_test.go` expectations**

Run the builder tests (next step). If `render_test.go` asserts the old create/update response substrings (`map[string]int64{"id": id}` etc.), update those expectations to the echoed form (`model.Find(id)` / `jsonOK(w, item)`). Change only assertions that broke because of this task's template edit; do not alter unrelated goldens.

- [ ] **Step 5: Build and test**

Run: `docker compose exec -T app sh -c 'cd /src/builder && go test ./...'` — green (templates parse; render tests match).

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(builder): create/update echo the full written object

Generated create/update re-read via Find and return the full row, making the
declared response:{object,model} truthful and saving native clients a re-fetch.
Generated test asserts the echoed object.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: `create_handler` declares its own contract

**Files:**
- Modify: `src/builder/main.go` — add `request_schema`/`response_schema`/`summary` args to the `create_handler` tool registration and `handleCreateHandler`.
- Create/extend: a builder test asserting a custom endpoint's schemas land in the manifest and malformed JSON fails.

**Interfaces:**
- Consumes: `parseBodySchemaArg` (Task 1); `Endpoint.Summary/Request/Response`.
- Produces: custom endpoints in `api.json` now carry declared body + semantics. Task 6 verifies live.

- [ ] **Step 1: Add the tool args** (`src/builder/main.go`, `create_handler` registration ~line 475)

Add three optional string args to the `mcp.NewTool("create_handler", ...)` block, and expand the description:

```go
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
```

- [ ] **Step 2: Wire `handleCreateHandler`** (`src/builder/main.go`)

After the existing arg reads and validation, parse the schemas and attach them to the endpoint:

```go
	reqSchema, err := parseBodySchemaArg(strArg(req, "request_schema"))
	if err != nil {
		return errResult("request_schema: " + err.Error()), nil
	}
	respSchema, err := parseBodySchemaArg(strArg(req, "response_schema"))
	if err != nil {
		return errResult("response_schema: " + err.Error()), nil
	}
	summary := strArg(req, "summary")

	// ... existing renderToFile(...) ...

	endpoint := Endpoint{
		Method: strings.ToUpper(method), Path: path,
		Handler: toPascal(name) + strings.ToUpper(method),
		Deps:    []string{"read", "write", "cache"},
		Auth:    authRequired, Kind: "custom",
		Summary: summary, Request: reqSchema, Response: respSchema,
	}
```

Where `strArg` is a tiny helper (add near the top of `main.go` if not present):

```go
func strArg(req mcp.CallToolRequest, key string) string {
	v, _ := req.Params.Arguments[key].(string)
	return v
}
```

(If reading the args inline like the existing `name, _ := req.Params.Arguments["name"].(string)` is preferred over `strArg`, do that instead — just keep it consistent with the surrounding code.)

- [ ] **Step 3: Test** — add `src/builder/create_handler_test.go` (new)

Test at the manifest layer (the tool handler needs an MCP request; instead assert the schema-parse + upsert path directly):

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCustomEndpointSchemaRoundtrip(t *testing.T) {
	dir := t.TempDir()
	api := filepath.Join(dir, "api.json")
	handlers := filepath.Join(dir, "handlers")
	os.MkdirAll(handlers, 0755)

	reqS, err := parseBodySchemaArg(`{"shape":"object","fields":[{"name":"note","type":"string"}]}`)
	if err != nil {
		t.Fatalf("parse request: %v", err)
	}
	respS, _ := parseBodySchemaArg(`{"shape":"object","fields":[{"name":"ok","type":"boolean"}]}`)
	ep := Endpoint{Method: "POST", Path: "/api/v1/todos/{id}/archive", Handler: "TodoArchivePOST",
		Deps: []string{"read", "write", "cache"}, Kind: "custom", Summary: "Archive a todo",
		Request: reqS, Response: respS}

	if err := updateManifestAt(api, handlers, time.Unix(0, 0).UTC(), nil, []Endpoint{ep}); err != nil {
		t.Fatalf("update: %v", err)
	}
	m, _ := readManifestAt(api)
	if m.Endpoints[0].Summary != "Archive a todo" || m.Endpoints[0].Request == nil || m.Endpoints[0].Response.Fields[0].Name != "ok" {
		t.Errorf("custom endpoint lost schema/summary: %+v", m.Endpoints[0])
	}
}

func TestParseBodySchemaArg_Errors(t *testing.T) {
	if _, err := parseBodySchemaArg(""); err != nil {
		t.Errorf("empty should be nil,nil: %v", err)
	}
	if _, err := parseBodySchemaArg(`{bad json`); err == nil {
		t.Error("malformed JSON should error")
	}
	if _, err := parseBodySchemaArg(`{"shape":"weird"}`); err == nil {
		t.Error("unknown shape should error")
	}
}
```

(If `updateManifestAt` needs a real `routes_gen.go.tmpl` on disk to regenerate routes, the temp `handlers` dir write may fail template lookup — in that case assert via `readManifestAt` after a direct `writeManifestAt`, or guard the routes step. Adjust to whatever the existing `manifest_test.go` does for isolated manifest writes.)

- [ ] **Step 4: Build and test**

Run: `docker compose exec -T app sh -c 'cd /src/builder && go test ./...'` — green.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(builder): create_handler declares request/response schema + summary

Custom endpoints can now carry a declared body shape and a one-line semantics
summary, closing the kind:custom black hole for native clients. Malformed schema
JSON fails the tool.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: App serves the enriched manifest faithfully

**Files:**
- Modify: `src/app/handlers/manifest.go` — mirror the new fields so `/api/v1/_manifest` reflects the file.
- Modify: `src/app/handlers/manifest_test.go` — assert a schema/format/reference round-trips through the endpoint.

**Interfaces:**
- Consumes: the `api.json` shape from Tasks 1–4.
- Produces: `/api/v1/_manifest` serves request/response/format/references/summary.

- [ ] **Step 1: Mirror the fields** (`src/app/handlers/manifest.go`)

The app decodes `api.json` into these structs and re-marshals via `jsonOK`, so any field absent here is dropped from the served manifest. Extend `ModelField` and `Endpoint`, add `BodySchema` (mirror of the builder's, read-only):

```go
type ModelField struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	Format     string `json:"format,omitempty"`
	References string `json:"references,omitempty"`
}

type BodySchema struct {
	Shape  string       `json:"shape"`
	Model  string       `json:"model,omitempty"`
	Fields []ModelField `json:"fields,omitempty"`
}

type Endpoint struct {
	Method   string      `json:"method"`
	Path     string      `json:"path"`
	Handler  string      `json:"handler"`
	Deps     []string    `json:"deps"`
	Auth     bool        `json:"auth"`
	Model    string      `json:"model,omitempty"`
	Kind     string      `json:"kind"`
	Summary  string      `json:"summary,omitempty"`
	Request  *BodySchema `json:"request,omitempty"`
	Response *BodySchema `json:"response,omitempty"`
}
```

- [ ] **Step 2: Test the round-trip** (`src/app/handlers/manifest_test.go`)

Add a test that writes a manifest with a custom-endpoint schema + a formatted/ref field and asserts they survive the endpoint (follow the existing `writeTempManifest` + `ManifestGET` pattern; the existing test shows how it reads `./api.json` via CWD — reuse that harness):

```go
func TestManifestGET_ServesEnrichedContract(t *testing.T) {
	dir := writeTempManifestDir(t, `{"api_version":"1.0.0","hash":"sha256:x","models":[
	  {"name":"reminder","table":"reminders","fields":[
	    {"name":"remind_at","type":"string","nullable":false,"format":"datetime-local"},
	    {"name":"category_id","type":"int","nullable":false,"references":"log_category"}]}],
	  "endpoints":[
	    {"method":"POST","path":"/api/v1/reminders","handler":"ReminderCreatePOST","deps":["read"],"auth":false,"kind":"create",
	     "request":{"shape":"object","fields":[{"name":"remind_at","type":"string","nullable":false,"format":"datetime-local"}]},
	     "response":{"shape":"object","model":"reminder"}}]}`)
	_ = dir
	rec := httptest.NewRecorder()
	ManifestGET().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/_manifest", nil))
	body := rec.Body.String()
	for _, want := range []string{`"format":"datetime-local"`, `"references":"log_category"`, `"request"`, `"response"`} {
		if !strings.Contains(body, want) {
			t.Errorf("served manifest missing %s\nbody: %s", want, body)
		}
	}
}
```

(If the existing test harness changes CWD via `t.Chdir` to read `./api.json`, mirror that here — name the helper to match whatever exists; the point is the four substrings must appear in the served body. Add `strings` to imports if missing.)

- [ ] **Step 3: Restart and test** (app-side — restart, not rebuild)

Run: `docker compose exec -T app go test ./handlers/...` — green.
Run: `docker compose restart app`; `docker compose logs app | tail -20` shows no errors.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat(app): serve enriched manifest fields at /_manifest

Mirror format/references/summary/request/response into the app's read-only
manifest structs so the served endpoint matches api.json instead of silently
dropping the new contract fields.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Rebuild mcp image and verify end-to-end, then revert

**Files:** none committed — this task rebuilds the image, runs a live scaffold, inspects, and reverts the scaffold artifacts, proving the tools emit the enriched contract.

**Interfaces:**
- Consumes: everything from Tasks 1–5.
- Produces: evidence; a clean tree.

- [ ] **Step 1: Rebuild the mcp image** (templates + generator are `go:embed`-ed at image-build time)

Run: `docker compose up -d --build`
Wait until both containers are healthy.

- [ ] **Step 2: Live scaffold a parent + child with a datetime and a ref**

Using the MCP tools (via the running `mcp` container / `/mcp` session), in order:

```
execute_sql(query="CREATE TABLE log_categories (id INTEGER PRIMARY KEY, title TEXT NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);")
scaffold_resource(name='log_category', fields=['title:string'])
execute_sql(query="CREATE TABLE reminders (id INTEGER PRIMARY KEY, title TEXT NOT NULL, remind_at TEXT NOT NULL, category_id INTEGER NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);")
scaffold_resource(name='reminder', fields=['title:string', 'remind_at:datetime', 'category_id:ref:log_category'])
create_handler(name='reminder_snooze', method='POST', path='/api/v1/reminders/{id}/snooze', request_schema='{"shape":"object","fields":[{"name":"minutes","type":"int"}]}', response_schema='{"shape":"object","model":"reminder"}', summary='Snooze a reminder by N minutes')
```

Also prove the ref guard: `scaffold_resource(name='orphan', fields=['x_id:ref:nonexistent'])` must ERROR with the "not scaffolded yet" message and write nothing.

- [ ] **Step 3: Inspect the emitted contract**

```bash
docker compose restart app   # pick up the regenerated routes_gen.go + api.json
curl -s http://localhost:8080/api/v1/_manifest | python3 -m json.tool | tee /tmp/manifest.json
```

Confirm in the output:
- `reminder` model: `remind_at` has `"format":"datetime-local"`; `category_id` has `"references":"log_category"`.
- `POST /api/v1/reminders` (create): `request.shape=="object"` with `remind_at` carrying the format; `response=={shape:object, model:reminder}`.
- `GET /api/v1/reminders` (list): `response.shape=="list"`.
- `DELETE …/{id}`: `response` is `{ok}`.
- `POST …/snooze` (custom): `summary=="Snooze a reminder by N minutes"`, `request` and `response` present.

- [ ] **Step 4: Prove the echo live**

```bash
# create a category and a reminder, confirm the create response is the full object (has created_at), not {id}
curl -s -X POST http://localhost:8080/api/v1/log_categories -H 'Content-Type: application/json' -d '{"title":"home"}'
curl -s -X POST http://localhost:8080/api/v1/reminders -H 'Content-Type: application/json' \
  -d '{"title":"trash","remind_at":"2026-07-27T11:45","category_id":1}' | tee /tmp/create.json
# assert: body contains "created_at" and the written title, not just {"id":...}
```

- [ ] **Step 5: Revert the scaffold artifacts to a clean tree**

The scaffold wrote models/handlers/pages/js + mutated `api.json`/`routes_gen.go` and created tables in `/data/app.db`. Restore the committed state (the build's code changes are already committed; only the live-scaffold artifacts are discarded):

```bash
git status                     # review what the scaffold generated
git checkout -- src/app/api.json src/app/handlers/routes_gen.go
git clean -fd src/app/models src/app/handlers src/app/static/pages src/app/static/js
# reset the scratch db so the demo tables don't linger
docker compose exec -T app sh -c 'rm -f /data/app.db /data/app.db-wal /data/app.db-shm' || true
docker compose restart app
git status                     # expect: clean (only the committed Task 1-5 changes present, no scaffold leftovers)
```

Verify `git status` is clean of scaffold artifacts and `docker compose logs app` is error-free.

- [ ] **Step 6: Record verification in the ledger** (no commit — evidence only)

Append the curl evidence summary to `.gova-build/progress.md`.

---

## Self-Review

**1. Spec coverage:**
- Body schemas (concern 1) → T1 types, T2 derivation, T5 served, T6 verified. ✅
- Custom endpoint bodies + semantics (concern 2) → T4 (`request_schema`/`response_schema`/`summary`). ✅
- Format hints (concern 3) → T1 (`datetime` etc. → `format`), carried through T2/T5, verified T6. ✅
- Relationships (concern 4) → T1 (`ref:<model>` → `references` + validation), verified T6. ✅
- Echo fix → T3. ✅
- Auth endpoints excluded → no task touches `authEndpoints` schemas; constraint stated. ✅
- Served-manifest faithfulness (integration risk found while reading) → T5. ✅
- mcp image rebuild → T6 Step 1; constraint stated. ✅

**2. Placeholder scan:** No TBD/TODO. Every code step shows complete code. The two "adjust to the existing harness" notes (T4 Step 3, T5 Step 2) are explicit about the fallback and the invariant to assert, not vague. ✅

**3. Naming consistency:** `BodySchema{Shape,Model,Fields}`, `ModelField.Format/References`, `Field.Format/Ref`, `Endpoint.Summary/Request/Response`, `resourceEndpoints(Model)`, `validateRefs`, `parseBodySchemaArg` used identically across tasks. Manifest field JSON tags (`format`,`references`,`summary`,`request`,`response`) match between builder (T1) and app mirror (T5). ✅

**4. CRUD completeness:** Not a feature build; the CRUD surface is the generator's existing five endpoints, all covered by the derivation (T2) and echo (T3). ✅
