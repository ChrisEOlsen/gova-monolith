# scaffold_resource — Full CRUD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `scaffold_resource` MCP tool that generates the full CRUD surface (list/detail/create/update/delete) for a resource — self-registering into the manifest — with whitelisted sort/filter on the list endpoint, so native clients (via Build 3a's export) get detail/edit/delete screens.

**Architecture:** The sort/filter injection-safety logic is a shared, hand-written, unit-tested `models/query.go`. The model template gains a sort/filter-aware `GetPage` and a `CRUD`-gated `Update`. A new resource-handlers template emits the five handlers. `scaffold_resource` wires it together and registers five endpoints via Build 2's `updateManifest`. `scaffold_list` stays, adapted to the new `GetPage` signature.

**Tech Stack:** Go 1.25 (`net/http`, `chi` v5, `database/sql`, `mattn/go-sqlite3`), `text/template` codegen, `mark3labs/mcp-go`, Docker Compose. Two modules: `src/app` (tested in Docker), `src/builder` (tested on host).

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-07-23-scaffold-resource-design.md` — authoritative.
- **Branch:** `build/scaffold-resource`. Never a git worktree (MCP container bind-mounts are path-bound).
- **`scaffold_list` stays and keeps working.** `scaffold_resource` is a new, separate tool. Both share the model template.
- **5 endpoints**, all `auth:false` (public): `GET /{plural}` (list), `GET /{plural}/{id}` (detail), `POST /{plural}` (create), `PUT /{plural}/{id}` (update), `DELETE /{plural}/{id}` (delete). Kinds: `list/detail/create/update/delete`. Deps `read,write,cache` for all.
- **Sort/filter:** `?sort=` (leading `-` = desc), `?filter=field:value`. Columns whitelisted against the model's own fields (`id` + fields + `created_at`); values are always bound `?` params; whitelisted column names are the only strings interpolated. Unknown column → `ErrInvalidQuery` → handler returns **422** (`validation_failed`). Absent sort → `ORDER BY created_at DESC`.
- **Client-input errors use 422, never 400.** Build 1's `codeForStatus` maps 422→`validation_failed` but has NO 400 mapping (400 falls through to `internal`, mislabeling). So a non-numeric id, a malformed body, and a bad sort/filter column all return 422.
- **Update is PUT full-replace**, `sql.ErrNoRows` on a missing id (→ 404). **Delete is idempotent** (missing id → 200).
- **`GetPage` signature changes** to `GetPage(limit, offset int, opts QueryOpts)` — the coordinated edit to `scaffold_list`'s handler (pass `models.QueryOpts{}`) is mandatory.
- **Frontend:** list page + `add_js_form` create marker only (same as `scaffold_list`). No detail/edit/delete web UI.
- **No new third-party deps.** No Node/npm.
- Two suites, both required: `docker compose exec app go test ./...` (app) and `cd src/builder && go test ./...` (builder, host).
- **Builder/template changes require `docker compose up -d --build`** before the MCP tools reflect them (templates are `go:embed`-ed at image-build time) — matters for the Task 6 end-to-end only.

---

## File Structure

**New files:**

| File | Responsibility |
|---|---|
| `src/app/models/query.go` | `ErrInvalidQuery`, `QueryOpts`, `orderByClause`, `filterField`, `contains` — the shared, injection-safe sort/filter logic |
| `src/app/models/query_test.go` | Unit tests for the above |
| `src/builder/templates/resource_handlers.go.tmpl` | The five CRUD handlers |
| `src/builder/templates/resource_handlers_test.go.tmpl` | Generated handler tests |

**Modified files:**

| File | Change |
|---|---|
| `src/builder/templates/model.go.tmpl` | `GetPage(…, opts)` + whitelist var + sort/filter SQL + cache key; `CRUD`-gated `Update` |
| `src/builder/templates/model_test.go.tmpl` | `GetPage` calls pass `QueryOpts{}`; `CRUD`-gated Update + bad-sort tests |
| `src/builder/templates/list_handler.go.tmpl` | `GetPage(limit, offset, models.QueryOpts{})` |
| `src/builder/main.go` | `CRUD bool` in `TemplateData`; funcMap `updateSet`, `structCallArgs`, `testJSON`; new `scaffold_resource` tool + handler |
| `src/builder/render_test.go` | Render assertions for the new/changed templates |
| `src/builder/manifest_test.go` | `scaffold_resource` registers the 5 endpoints (via `updateManifestAt` helpers) |
| `CLAUDE.md` | `scaffold_resource` cheat-sheet + sort/filter contract |
| `../gova-ios/CLAUDE.md` | Note: resources now expose full CRUD in the manifest |

---

## Task 1: Shared query infrastructure — `models/query.go`

**Files:**
- Create: `src/app/models/query.go`
- Test: `src/app/models/query_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (package `models`): `ErrInvalidQuery` (`error`), `QueryOpts{Sort, FilterField, FilterValue string}`, `orderByClause(sort string, allowed []string) (string, error)`, `filterField(field string, allowed []string) (string, error)`. Task 2's generated `GetPage` calls these.

- [ ] **Step 1: Write the failing test**

Create `src/app/models/query_test.go`:

```go
package models

import (
	"errors"
	"testing"
)

func TestOrderByClause_Default(t *testing.T) {
	got, err := orderByClause("", []string{"id", "name", "created_at"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "ORDER BY created_at DESC" {
		t.Errorf("default: got %q", got)
	}
}

func TestOrderByClause_AscAndDesc(t *testing.T) {
	allowed := []string{"id", "name", "created_at"}
	asc, err := orderByClause("name", allowed)
	if err != nil || asc != "ORDER BY name ASC" {
		t.Errorf("asc: got %q err %v", asc, err)
	}
	desc, err := orderByClause("-created_at", allowed)
	if err != nil || desc != "ORDER BY created_at DESC" {
		t.Errorf("desc: got %q err %v", desc, err)
	}
}

func TestOrderByClause_UnknownColumnRejected(t *testing.T) {
	_, err := orderByClause("bogus", []string{"id", "name"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("unknown column: want ErrInvalidQuery, got %v", err)
	}
}

func TestOrderByClause_InjectionRejected(t *testing.T) {
	// A would-be injection string is not exactly a whitelisted column, so it
	// is rejected — the whitelist is the security boundary.
	_, err := orderByClause("name; DROP TABLE users", []string{"id", "name"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("injection: want ErrInvalidQuery, got %v", err)
	}
	_, err = orderByClause("-name; DROP TABLE users", []string{"id", "name"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("injection desc: want ErrInvalidQuery, got %v", err)
	}
}

func TestFilterField_Valid(t *testing.T) {
	got, err := filterField("status", []string{"id", "status", "created_at"})
	if err != nil || got != "status" {
		t.Errorf("valid filter: got %q err %v", got, err)
	}
}

func TestFilterField_UnknownRejected(t *testing.T) {
	_, err := filterField("bogus", []string{"id", "status"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("unknown filter: want ErrInvalidQuery, got %v", err)
	}
	_, err = filterField("status = 1 OR 1=1", []string{"id", "status"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("injection filter: want ErrInvalidQuery, got %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `docker compose exec app go test ./models/... -run 'TestOrderBy|TestFilterField' -v`
Expected: FAIL — `undefined: orderByClause`, `undefined: filterField`, `undefined: ErrInvalidQuery`.

- [ ] **Step 3: Write the implementation**

Create `src/app/models/query.go`:

```go
package models

import (
	"errors"
	"strings"
)

// ErrInvalidQuery is returned when a sort or filter names a column that is not
// in the model's whitelist. Handlers map it to HTTP 422.
var ErrInvalidQuery = errors.New("invalid query parameter")

// QueryOpts carries list options validated at the boundary. Empty fields mean
// "not requested": empty Sort → default ordering, empty FilterField → no filter.
type QueryOpts struct {
	Sort        string // "name" (asc) or "-name" (desc); "" = default
	FilterField string // "" = no filter
	FilterValue string
}

// orderByClause returns a safe "ORDER BY <col> ASC|DESC" for a sort spec whose
// column is in allowed. A leading '-' means DESC. "" → "ORDER BY created_at
// DESC". A column not exactly present in allowed → ErrInvalidQuery.
//
// The returned column is always a member of allowed (a generated literal of the
// model's real columns), so interpolating it into SQL is safe. Values are never
// handled here — filter values are bound as ? parameters by the caller.
func orderByClause(sort string, allowed []string) (string, error) {
	if sort == "" {
		return "ORDER BY created_at DESC", nil
	}
	col := sort
	dir := "ASC"
	if strings.HasPrefix(sort, "-") {
		col = sort[1:]
		dir = "DESC"
	}
	if !contains(allowed, col) {
		return "", ErrInvalidQuery
	}
	return "ORDER BY " + col + " " + dir, nil
}

// filterField validates a filter column against allowed and returns the safe
// column name (to be interpolated), or ErrInvalidQuery. The filter value is
// bound as a ? parameter by the caller — never interpolated.
func filterField(field string, allowed []string) (string, error) {
	if !contains(allowed, field) {
		return "", ErrInvalidQuery
	}
	return field, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `docker compose exec app go test ./models/... -v`
Expected: PASS — the new query tests plus every existing models test (`time_test.go` etc.).

- [ ] **Step 5: Commit**

```bash
git add src/app/models/query.go src/app/models/query_test.go
git commit -m "feat: shared injection-safe sort/filter query helpers"
```

---

## Task 2: Model template — sort/filter GetPage + Update

**Files:**
- Modify: `src/builder/main.go` — `TemplateData` gains `CRUD bool`; funcMap gains `updateSet`
- Modify: `src/builder/templates/model.go.tmpl`
- Modify: `src/builder/templates/model_test.go.tmpl`
- Modify: `src/builder/templates/list_handler.go.tmpl`
- Modify: `src/builder/render_test.go`

**Interfaces:**
- Consumes: `models.QueryOpts`, `orderByClause`, `filterField`, `models.ErrInvalidQuery` (Task 1).
- Produces, on every generated model `X`:
  - `GetPage(limit, offset int, opts QueryOpts) ([]X, int, error)` — sort/filter-aware. Both tools call this.
  - `Update(id int64, <fields...>) error` — emitted only when the template's `.CRUD` is true. Returns `sql.ErrNoRows` if the id doesn't exist.
  - `xAllowedColumns` package var (the whitelist).

- [ ] **Step 1: Add `CRUD` to `TemplateData` and the `updateSet` funcMap helper**

In `src/builder/main.go`, add a field to the `TemplateData` struct (alongside `HasPassword`, `AuthRequired`, etc.):

```go
	CRUD bool
```

Add to `funcMap` (near `placeholders`/`createParams`):

```go
	// updateSet emits "field1 = ?, field2 = ?" for an UPDATE statement.
	"updateSet": func(fields []Field) string {
		parts := make([]string, len(fields))
		for i, f := range fields {
			parts[i] = f.Name + " = ?"
		}
		return strings.Join(parts, ", ")
	},
```

- [ ] **Step 2: Write the failing render tests**

Add to `src/builder/render_test.go`:

```go
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
```

(`sampleFieldsWithNullable()` from Build 1 returns `title:string`, `notes:string?`, `count:int`, `score:int?` — hence the `Update` signature above.)

- [ ] **Step 3: Run to verify it fails**

Run: `cd src/builder && go test ./... -run 'TestModelTemplate_GetPage|TestModelTemplate_Update|TestModelTestTemplate_CRUD' -v`
Expected: FAIL — the template still emits the old `GetPage(limit, offset)` and no `Update`.

- [ ] **Step 4: Rewrite `GetPage` and add `Update` in `model.go.tmpl`**

Replace the `GetPage` function in `src/builder/templates/model.go.tmpl` (from `// GetPage returns…` through its closing brace) with:

```gotemplate
// {{.Name}}AllowedColumns is the whitelist for sort/filter — the model's real
// columns. orderByClause/filterField reject anything not in this list, so the
// only column names ever placed into SQL come from here.
var {{.Name}}AllowedColumns = []string{"id", {{range .Fields}}"{{.Name}}", {{end}}"created_at"}

// GetPage returns one window of rows plus the total (of the filtered set).
// Callers clamp limit and offset — see handlers/paging.go. Sort/filter columns
// are validated against {{.Name}}AllowedColumns; an unknown column yields
// ErrInvalidQuery (handlers map it to 422).
func (m *{{.PascalName}}Model) GetPage(limit, offset int, opts QueryOpts) ([]{{.PascalName}}, int, error) {
	orderBy, err := orderByClause(opts.Sort, {{.Name}}AllowedColumns)
	if err != nil {
		return nil, 0, err
	}
	where := ""
	args := []any{}
	if opts.FilterField != "" {
		col, ferr := filterField(opts.FilterField, {{.Name}}AllowedColumns)
		if ferr != nil {
			return nil, 0, ferr
		}
		where = " WHERE " + col + " = ?"
		args = append(args, opts.FilterValue)
	}

	cacheKey := fmt.Sprintf("{{.PluralName}}:page:%d:%d:%s:%s:%s", limit, offset, opts.Sort, opts.FilterField, opts.FilterValue)
	if hit, ok := m.cache.Get(cacheKey); ok {
		var page {{.Name}}Page
		if err := json.Unmarshal(hit, &page); err == nil {
			return page.Items, page.Total, nil
		}
	}

	var total int
	if err := m.readDB.QueryRow("SELECT COUNT(*) FROM {{.PluralName}}"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := "SELECT id, {{joinNames .Fields}}, created_at FROM {{.PluralName}}" + where + " " + orderBy + " LIMIT ? OFFSET ?"
	rows, err := m.readDB.Query(query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	// Initialized non-nil: a nil slice marshals as JSON null, which breaks
	// strictly-typed clients decoding an array.
	items := []{{.PascalName}}{}
	for rows.Next() {
		var item {{.PascalName}}
{{scanDecls .Fields "\t\t"}}		if err := rows.Scan(&item.ID, {{scanTargets .Fields "&item."}}, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
{{scanAssigns .Fields "item." "\t\t"}}		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if data, err := json.Marshal({{.Name}}Page{Items: items, Total: total}); err == nil {
		m.cache.Set(cacheKey, data, 5*time.Minute)
	}
	return items, total, nil
}
```

Then, immediately after the `Delete` method's closing brace at the end of the file, add the `CRUD`-gated `Update`:

```gotemplate
{{if .CRUD}}
func (m *{{.PascalName}}Model) Update(id int64, {{createParams .Fields}}) error {
	{{- range .Fields}}{{if eq .Type "password"}}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	{{end}}{{end}}
	res, err := m.writeDB.Exec(
		"UPDATE {{.PluralName}} SET {{updateSet .Fields}} WHERE id = ?",
		{{insertArgs .Fields}}, id,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	m.cache.Bust("{{.PluralName}}:")
	return nil
}
{{end}}
```

Note: `sql`, `fmt`, `time`, `json` are already imported by the template; `bcrypt` is conditionally imported when `.HasPassword`.

- [ ] **Step 5: Update `list_handler.go.tmpl` for the new signature**

In `src/builder/templates/list_handler.go.tmpl`, change the `GetPage` call:

```gotemplate
		items, total, err := model.GetPage(limit, offset, models.QueryOpts{})
```

(`models` is already imported in that template.)

- [ ] **Step 6: Update `model_test.go.tmpl`**

In `src/builder/templates/model_test.go.tmpl`, change every `GetPage(...)` call to pass `QueryOpts{}` (the test is in package `models`, so unqualified). For example `m.GetPage(50, 0)` → `m.GetPage(50, 0, QueryOpts{})` and `m.GetPage(50, 50)` → `m.GetPage(50, 50, QueryOpts{})`.

Then append CRUD-gated tests just before the final closing of the test function's file (after the existing `Test{{.PascalName}}Model_CRUD` function):

```gotemplate
{{if .CRUD}}
func Test{{.PascalName}}Model_Update(t *testing.T) {
	testDB := db.OpenTest(t, `CREATE TABLE {{.PluralName}} (
		id INTEGER PRIMARY KEY,
		{{- range .Fields}}
		{{.Name}} {{sqlType .Type}}{{sqlNotNull .}},
		{{- end}}
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	m := New{{.PascalName}}Model(testDB.Read, testDB.Write, cache.New())

{{testDecls .Fields "\t"}}	id, err := m.Create({{testArgs .Fields}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Update the existing row — should succeed.
	if err := m.Update(id, {{testArgs .Fields}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	// Update a missing row — should report sql.ErrNoRows.
	if err := m.Update(999999, {{testArgs .Fields}}); err != sql.ErrNoRows {
		t.Errorf("Update(missing): got %v, want sql.ErrNoRows", err)
	}
}

func Test{{.PascalName}}Model_GetPageRejectsBadSort(t *testing.T) {
	testDB := db.OpenTest(t, `CREATE TABLE {{.PluralName}} (
		id INTEGER PRIMARY KEY,
		{{- range .Fields}}
		{{.Name}} {{sqlType .Type}}{{sqlNotNull .}},
		{{- end}}
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	m := New{{.PascalName}}Model(testDB.Read, testDB.Write, cache.New())
	if _, _, err := m.GetPage(50, 0, QueryOpts{Sort: "bogus_column"}); err != ErrInvalidQuery {
		t.Errorf("GetPage bad sort: got %v, want ErrInvalidQuery", err)
	}
}
{{end}}
```

The CRUD-gated tests use `sql.ErrNoRows` and `ErrInvalidQuery` — add `"database/sql"` to the test template's imports (guarded so it is only needed under `.CRUD`; simplest is to always import it, but an unused import fails to compile for the non-CRUD variant). **To avoid an unused import in the non-CRUD case, import `database/sql` inside a `{{if .CRUD}}` guard in the import block:**

```gotemplate
import (
	"testing"
	{{- if .CRUD}}
	"database/sql"
	{{- end}}

	"gova/app/cache"
	"gova/app/db"
)
```

- [ ] **Step 7: Run the builder suite**

Run: `cd src/builder && go build ./... && go test ./... -v`
Expected: build succeeds; all render tests pass, including the new GetPage/Update assertions and both CRUD/non-CRUD `model_test` variants parsing as valid Go.

- [ ] **Step 8: Confirm the generated app code still compiles (via a scaffold-free build)**

The template edits only take effect when something is scaffolded; but the committed app must still build. Since no generated files changed on disk, just confirm the app module is unaffected:

Run: `docker compose exec app go build ./...`
Expected: succeeds (Task 1's `query.go` is present; no generated models exist yet on the clean tree).

- [ ] **Step 9: Commit**

```bash
git add src/builder/main.go src/builder/templates/model.go.tmpl \
        src/builder/templates/model_test.go.tmpl src/builder/templates/list_handler.go.tmpl \
        src/builder/render_test.go
git commit -m "feat: model template gains sort/filter GetPage and CRUD-gated Update"
```

---

## Task 3: Resource handlers template

**Files:**
- Create: `src/builder/templates/resource_handlers.go.tmpl`
- Create: `src/builder/templates/resource_handlers_test.go.tmpl`
- Modify: `src/builder/main.go` — funcMap `structCallArgs`, `testJSON`
- Modify: `src/builder/render_test.go` — render assertions

**Interfaces:**
- Consumes: the model's `GetPage(…, QueryOpts)`, `Find`, `Create`, `Update`, `Delete` (Task 2); `models.ErrInvalidQuery`; Build 1 envelope helpers (`jsonOK`, `jsonError`, `jsonList`, `Meta`, `queryInt`, paging consts).
- Produces: five `http.HandlerFunc` constructors named `{P}ListGET`, `{P}DetailGET`, `{P}CreatePOST`, `{P}UpdatePUT`, `{P}DeleteDELETE`, each `(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc`.

- [ ] **Step 1: Add the `structCallArgs` and `testJSON` funcMap helpers**

In `src/builder/main.go` `funcMap`:

```go
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
```

- [ ] **Step 2: Write the failing render test**

Add to `src/builder/render_test.go`:

```go
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
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd src/builder && go test ./... -run 'TestResourceHandlers' -v`
Expected: FAIL — templates don't exist yet (`getTemplate` error → render fails).

- [ ] **Step 4: Write `resource_handlers.go.tmpl`**

Create `src/builder/templates/resource_handlers.go.tmpl`:

```gotemplate
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"gova/app/cache"
	"gova/app/models"
)

type {{.Name}}Request struct {
	{{- range .Fields}}
	{{toPascal .Name}} {{goFieldType .}} `json:"{{.Name}}"`
	{{- end}}
}

func parse{{.PascalName}}ID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// {{.PascalName}}ListGET handles GET /api/v1/{{.PluralName}}
// Query: ?limit=<1..200>&offset=<0..>&sort=<[-]col>&filter=<col:value>
func {{.PascalName}}ListGET(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := queryInt(r, "limit", defaultPageLimit, 1, maxPageLimit)
		offset := queryInt(r, "offset", 0, 0, maxPageOffset)
		opts := models.QueryOpts{Sort: r.URL.Query().Get("sort")}
		if f := r.URL.Query().Get("filter"); f != "" {
			if k, v, ok := strings.Cut(f, ":"); ok {
				opts.FilterField, opts.FilterValue = k, v
			}
		}
		model := models.New{{.PascalName}}Model(readDB, writeDB, appCache)
		items, total, err := model.GetPage(limit, offset, opts)
		if err != nil {
			if errors.Is(err, models.ErrInvalidQuery) {
				jsonError(w, "invalid sort/filter column; allowed: id, {{joinNames .Fields}}, created_at", 422)
				return
			}
			jsonError(w, "failed to load", 500)
			return
		}
		jsonList(w, items, Meta{Limit: limit, Offset: offset, Total: total})
	}
}

// {{.PascalName}}DetailGET handles GET /api/v1/{{.PluralName}}/{id}
func {{.PascalName}}DetailGET(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse{{.PascalName}}ID(r)
		if err != nil {
			jsonError(w, "invalid id", 422)
			return
		}
		model := models.New{{.PascalName}}Model(readDB, writeDB, appCache)
		item, err := model.Find(id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				jsonError(w, "not found", 404)
				return
			}
			jsonError(w, "failed to load", 500)
			return
		}
		jsonOK(w, item)
	}
}

// {{.PascalName}}CreatePOST handles POST /api/v1/{{.PluralName}}
func {{.PascalName}}CreatePOST(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req {{.Name}}Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", 422)
			return
		}
		model := models.New{{.PascalName}}Model(readDB, writeDB, appCache)
		id, err := model.Create({{structCallArgs .Fields "req."}})
		if err != nil {
			jsonError(w, "failed to create", 500)
			return
		}
		jsonOK(w, map[string]int64{"id": id})
	}
}

// {{.PascalName}}UpdatePUT handles PUT /api/v1/{{.PluralName}}/{id}
func {{.PascalName}}UpdatePUT(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse{{.PascalName}}ID(r)
		if err != nil {
			jsonError(w, "invalid id", 422)
			return
		}
		var req {{.Name}}Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", 422)
			return
		}
		model := models.New{{.PascalName}}Model(readDB, writeDB, appCache)
		if err := model.Update(id, {{structCallArgs .Fields "req."}}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				jsonError(w, "not found", 404)
				return
			}
			jsonError(w, "failed to update", 500)
			return
		}
		jsonOK(w, map[string]int64{"id": id})
	}
}

// {{.PascalName}}DeleteDELETE handles DELETE /api/v1/{{.PluralName}}/{id}
func {{.PascalName}}DeleteDELETE(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse{{.PascalName}}ID(r)
		if err != nil {
			jsonError(w, "invalid id", 422)
			return
		}
		model := models.New{{.PascalName}}Model(readDB, writeDB, appCache)
		if err := model.Delete(id); err != nil {
			jsonError(w, "failed to delete", 500)
			return
		}
		jsonOK(w, map[string]bool{"ok": true})
	}
}
```

**Import note:** the import block above already includes `"strings"` (used by `strings.Cut` in the list handler). `renderAndParse` only syntax-checks; the real compile is Task 6, so the imports must be correct as written now.

- [ ] **Step 5: Write `resource_handlers_test.go.tmpl`**

Create `src/builder/templates/resource_handlers_test.go.tmpl`:

```gotemplate
package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gova/app/cache"
	"gova/app/db"
	"gova/app/models"
)

func {{.Name}}ResourceSchema() string {
	return `CREATE TABLE {{.PluralName}} (
		id INTEGER PRIMARY KEY,
		{{- range .Fields}}
		{{.Name}} {{sqlType .Type}}{{sqlNotNull .}},
		{{- end}}
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
}

// {{.Name}}Router mounts the five resource routes so path params resolve.
func {{.Name}}Router(testDB *db.DB, appCache *cache.Cache) chi.Router {
	r := chi.NewRouter()
	r.Get("/api/v1/{{.PluralName}}", {{.PascalName}}ListGET(testDB.Read, testDB.Write, appCache))
	r.Get("/api/v1/{{.PluralName}}/{id}", {{.PascalName}}DetailGET(testDB.Read, testDB.Write, appCache))
	r.Post("/api/v1/{{.PluralName}}", {{.PascalName}}CreatePOST(testDB.Read, testDB.Write, appCache))
	r.Put("/api/v1/{{.PluralName}}/{id}", {{.PascalName}}UpdatePUT(testDB.Read, testDB.Write, appCache))
	r.Delete("/api/v1/{{.PluralName}}/{id}", {{.PascalName}}DeleteDELETE(testDB.Read, testDB.Write, appCache))
	return r
}

func Test{{.PascalName}}ResourceCRUD(t *testing.T) {
	testDB := db.OpenTest(t, {{.Name}}ResourceSchema())
	appCache := cache.New()
	router := {{.Name}}Router(testDB, appCache)
	model := models.New{{.PascalName}}Model(testDB.Read, testDB.Write, appCache)
{{testDecls .Fields "\t"}}	id, err := model.Create({{testArgs .Fields}})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	do := func(method, target, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, target, nil)
		} else {
			r = httptest.NewRequest(method, target, strings.NewReader(body))
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	// Detail: found and not-found.
	if rec := do(http.MethodGet, "/api/v1/{{.PluralName}}/1", ""); rec.Code != 200 {
		t.Errorf("detail found: got %d, body %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/v1/{{.PluralName}}/999999", ""); rec.Code != 404 {
		t.Errorf("detail missing: got %d, want 404", rec.Code)
	}
	// Detail: non-numeric id -> 422.
	if rec := do(http.MethodGet, "/api/v1/{{.PluralName}}/abc", ""); rec.Code != 422 {
		t.Errorf("detail bad id: got %d, want 422", rec.Code)
	}

	// Create.
	if rec := do(http.MethodPost, "/api/v1/{{.PluralName}}", `{{testJSON .Fields}}`); rec.Code != 200 {
		t.Errorf("create: got %d, body %s", rec.Code, rec.Body.String())
	}

	// Update: existing and missing.
	if rec := do(http.MethodPut, "/api/v1/{{.PluralName}}/1", `{{testJSON .Fields}}`); rec.Code != 200 {
		t.Errorf("update: got %d, body %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPut, "/api/v1/{{.PluralName}}/999999", `{{testJSON .Fields}}`); rec.Code != 404 {
		t.Errorf("update missing: got %d, want 404", rec.Code)
	}

	// List: valid sort ok, bogus sort -> 422.
	if rec := do(http.MethodGet, "/api/v1/{{.PluralName}}?sort=-id", ""); rec.Code != 200 {
		t.Errorf("list sort: got %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/v1/{{.PluralName}}?sort=bogus", ""); rec.Code != 422 {
		t.Errorf("list bad sort: got %d, want 422", rec.Code)
	}

	// Delete.
	if rec := do(http.MethodDelete, "/api/v1/{{.PluralName}}/1", ""); rec.Code != 200 {
		t.Errorf("delete: got %d", rec.Code)
	}
	_ = id
}
```

- [ ] **Step 6: Run the builder suite**

Run: `cd src/builder && go build ./... && go test ./... -v`
Expected: build succeeds; the new resource-template render tests pass (valid Go), plus everything from Tasks 1-2.

- [ ] **Step 7: Commit**

```bash
git add src/builder/main.go src/builder/render_test.go \
        src/builder/templates/resource_handlers.go.tmpl \
        src/builder/templates/resource_handlers_test.go.tmpl
git commit -m "feat: resource CRUD handlers template with router-level tests"
```

---

## Task 4: The `scaffold_resource` tool

**Files:**
- Modify: `src/builder/main.go` — register the tool + `handleScaffoldResource`
- Modify: `src/builder/manifest_test.go` — endpoint-registration test

**Interfaces:**
- Consumes: `applySchema`, `fieldsToModel`, `updateManifest`/`updateManifestAt` (Build 1-2), `renderToFile`, `newData`, and the templates from Tasks 2-3.
- Produces: a `scaffold_resource` MCP tool. `resourceEndpoints(name string) []Endpoint` — a pure helper returning the five endpoints, so the registration is unit-testable.

- [ ] **Step 1: Write the failing endpoint-registration test**

Add to `src/builder/manifest_test.go`:

```go
func TestResourceEndpoints_FiveWithKinds(t *testing.T) {
	eps := resourceEndpoints("project")
	if len(eps) != 5 {
		t.Fatalf("got %d endpoints, want 5", len(eps))
	}
	want := map[string]string{
		"GET /api/v1/projects":       "list",
		"GET /api/v1/projects/{id}":  "detail",
		"POST /api/v1/projects":      "create",
		"PUT /api/v1/projects/{id}":  "update",
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd src/builder && go test ./... -run TestResourceEndpoints -v`
Expected: FAIL — `undefined: resourceEndpoints`.

- [ ] **Step 3: Implement `resourceEndpoints` and `handleScaffoldResource`**

Add to `src/builder/main.go`:

```go
// resourceEndpoints returns the five CRUD endpoints scaffold_resource registers.
// The handler symbols must match resource_handlers.go.tmpl exactly.
func resourceEndpoints(name string) []Endpoint {
	p := toPascal(name)
	plural := toPlural(name)
	base := "/api/v1/" + plural
	rwc := []string{"read", "write", "cache"}
	return []Endpoint{
		{Method: "GET", Path: base, Handler: p + "ListGET", Deps: rwc, Model: name, Kind: "list"},
		{Method: "GET", Path: base + "/{id}", Handler: p + "DetailGET", Deps: rwc, Model: name, Kind: "detail"},
		{Method: "POST", Path: base, Handler: p + "CreatePOST", Deps: rwc, Model: name, Kind: "create"},
		{Method: "PUT", Path: base + "/{id}", Handler: p + "UpdatePUT", Deps: rwc, Model: name, Kind: "update"},
		{Method: "DELETE", Path: base + "/{id}", Handler: p + "DeleteDELETE", Deps: rwc, Model: name, Kind: "delete"},
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
	if err := updateManifest([]Model{model}, resourceEndpoints(name)); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}

	return mcp.NewToolResultText(
		strings.Join(results, "\n") +
			"\n\nRegistered full CRUD (list, detail, create, update, delete) for /api/v1/" + toPlural(name) +
			" in api.json + routes_gen.go. Endpoints are public — set auth:true per endpoint in api.json to protect them (requires scaffold_auth).\n" +
			"Add a create form with add_js_form.\n\n" + runPatternChecks(),
	), nil
}
```

Register the tool in `main()` (next to `scaffold_list`'s `s.AddTool(...)`):

```go
	s.AddTool(mcp.NewTool("scaffold_resource",
		mcp.WithDescription("Generate full CRUD for a resource: model (with Update) + list/detail/create/update/delete handlers + list page, and register all 5 routes in api.json + routes_gen.go. List supports ?sort=&filter= (whitelisted columns). Table must exist first (run execute_sql). Endpoints are public; protect per-endpoint via the manifest. Use scaffold_list for read-only resources."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Resource name in snake_case")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
	), handleScaffoldResource)
```

- [ ] **Step 4: Run the builder suite**

Run: `cd src/builder && go build ./... && go test ./... -v`
Expected: build succeeds; `TestResourceEndpoints_FiveWithKinds` passes plus all prior tests.

- [ ] **Step 5: Commit**

```bash
git add src/builder/main.go src/builder/manifest_test.go
git commit -m "feat: scaffold_resource tool generates and registers full CRUD"
```

---

## Task 5: Documentation

**Files:**
- Modify: `CLAUDE.md`
- Modify: `../gova-ios/CLAUDE.md`

**Interfaces:** consumes everything above; produces no code.

- [ ] **Step 1: Add `scaffold_resource` to the monolith CLAUDE.md Tool Cheat Sheet**

In `CLAUDE.md`, in the Tool Cheat Sheet table, add a row after `scaffold_list`:

```markdown
| `scaffold_resource` | Full CRUD: model + list/detail/create/update/delete handlers + list page, all self-registered. List supports `?sort=`/`?filter=` (whitelisted). Table must exist first. Public by default. | Yes — model CRUD + resource handler tests |
```

And update the `scaffold_list` row's description to note it is the read-only option (append: "— read-only; use `scaffold_resource` for full CRUD").

- [ ] **Step 2: Document the sort/filter contract**

In `CLAUDE.md`, under the "API Manifest & Routing" section (added in Build 2), add a short subsection:

```markdown
### Resource list querying (scaffold_resource)

A `scaffold_resource` list endpoint accepts, beyond `?limit=`/`?offset=`:
- `?sort=<col>` (ascending) or `?sort=-<col>` (descending)
- `?filter=<col>:<value>` — equality on a column

`<col>` is whitelisted against the model's real columns (`id`, its fields,
`created_at`); an unknown column returns **422** (`validation_failed`). Filter
values are always bound parameters. The whitelist/validation lives in the shared,
hand-written `models/query.go`. Create/update validation is coarse (malformed body
→ 422, model/DB error → 500); per-field 422 is a deferred enhancement.
```

- [ ] **Step 3: Note full CRUD in the gova-ios CLAUDE.md**

In `../gova-ios/CLAUDE.md`, add a brief note near the manifest forward-pointer (the note Build 3a/Build 2 added):

```markdown
> **Full CRUD (as of the monolith's Build 3b):** a resource scaffolded with
> `scaffold_resource` exposes `list`, `detail`, `create`, `update`, and `delete`
> endpoints (those `kind`s appear in the manifest). `/export:mobile` surfaces them
> under the resource; a future `/build` update can generate detail/edit/delete
> screens from them. (`scaffold_list` resources remain list-only.)
```

- [ ] **Step 4: Verify docs mention the new tool**

```bash
grep -n "scaffold_resource" CLAUDE.md
grep -n "Full CRUD\|scaffold_resource" ../gova-ios/CLAUDE.md
```
Expected: both show the additions.

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: scaffold_resource cheat-sheet and sort/filter contract"
cd ../gova-ios && git add CLAUDE.md && \
  git commit -m "docs: note full-CRUD resource kinds in the monolith manifest" && cd -
```

---

## Task 6: End-to-end verification via MCP

**Files:** none modified — run and observe, then revert the scratch scaffold.

**Interfaces:** consumes everything above.

**Context:** the `mcp` image embeds templates at build time, so **rebuild it first**. Drive the MCP server over stdio via `docker exec -i gove-test-mcp-1 /usr/local/bin/mcp-server` with the JSON-RPC handshake (initialize → notifications/initialized → tools/call), as in prior builds. The scratch DB is a git-ignored bind mount.

- [ ] **Step 1: Rebuild clean**

```bash
docker compose down
rm -f data/app.db data/app.db-wal data/app.db-shm
docker compose up -d --build
until docker compose exec -T app true 2>/dev/null; do sleep 2; done
```

- [ ] **Step 2: Both suites green on the clean build**

```bash
docker compose exec app go test ./...
cd src/builder && go test ./... && cd -
```
Expected: all `ok`.

- [ ] **Step 3: Scaffold a resource with a nullable column via MCP**

Use a JSON-RPC helper (as in Build 2's Task 9). Call `execute_sql`:
```sql
CREATE TABLE projects (id INTEGER PRIMARY KEY, name TEXT NOT NULL, notes TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
```
then `scaffold_resource(name='project', fields=['name:string','notes:string'])`.

Expected: six files created; reports registering full CRUD.

- [ ] **Step 4: Assert the manifest has 5 endpoints and routes_gen mounts them**

```bash
python3 -c "import json;d=json.load(open('src/app/api.json'));print(sorted((e['method'],e['path'],e['kind']) for e in d['endpoints']))"
grep -nE "ProjectListGET|ProjectDetailGET|ProjectCreatePOST|ProjectUpdatePUT|ProjectDeleteDELETE" src/app/handlers/routes_gen.go
git status --short src/app/main.go
```
Expected: five endpoints (list/detail/create/update/delete) with the `{id}` paths; `routes_gen.go` mounts all five; `main.go` **unmodified**.

- [ ] **Step 5: Generated tests compile and pass**

```bash
docker compose restart app
sleep 5
docker compose exec app go test ./...
```
Expected: `ok` including the generated `models/Project_test.go` (Update + bad-sort) and `handlers/project_resource_test.go` (the five-endpoint CRUD test).

- [ ] **Step 6: Exercise the full CRUD live**

```bash
until curl -sf localhost:8080/api/v1/_version >/dev/null 2>&1; do sleep 2; done
echo "== create =="; curl -s -X POST localhost:8080/api/v1/projects -H 'Content-Type: application/json' -d '{"name":"Roof","notes":null}'
echo; echo "== list =="; curl -s "localhost:8080/api/v1/projects"
echo; echo "== detail =="; curl -s "localhost:8080/api/v1/projects/1"
echo; echo "== update =="; curl -s -X PUT localhost:8080/api/v1/projects/1 -H 'Content-Type: application/json' -d '{"name":"Deck","notes":"urgent"}'
echo; echo "== detail after update =="; curl -s "localhost:8080/api/v1/projects/1"
echo; echo "== sort =="; curl -s "localhost:8080/api/v1/projects?sort=-name"
echo; echo "== bad sort (expect 422) =="; curl -s -o /dev/null -w "%{http_code}\n" "localhost:8080/api/v1/projects?sort=bogus"
echo "== filter =="; curl -s "localhost:8080/api/v1/projects?filter=name:Deck"
echo; echo "== delete =="; curl -s -X DELETE localhost:8080/api/v1/projects/1
echo; echo "== detail after delete (expect 404) =="; curl -s -o /dev/null -w "%{http_code}\n" "localhost:8080/api/v1/projects/1"
```
Expected: create → `{"id":1}`; list shows the row (`notes:null` then `"urgent"` after update); detail 200; update 200 and the follow-up detail shows `Deck`/`urgent`; sort 200; **bad sort → 422**; filter returns the matching row; delete → `{"ok":true}`; detail after delete → **404**.

- [ ] **Step 7: Confirm 3a's export surfaces all five**

```bash
cp SEED_FIXTURE.md /tmp/seed.md 2>/dev/null || printf '# Spec\n\n<!-- /export:mobile WRITES BELOW THIS LINE -->\n' > /tmp/seed.md
python3 ../gova-ios/.claude/scripts/export_manifest.py src/app/api.json /tmp/seed.md
sed -n '/Resources → endpoints/,/Screens to generate/p' /tmp/seed.md
```
Expected: under `**project**`, all five endpoints appear (`list`, `detail`, `create`, `update`, `delete`) — proving 3a consumes the richer manifest with no change.

- [ ] **Step 8: Conflict / idempotency check**

Re-run `scaffold_resource(name='project', fields=['name:string','notes:string'])`.
Expected: succeeds (idempotent upsert — same 5 handlers/paths); `api.json` still has exactly 5 project endpoints (no duplicates).

- [ ] **Step 9: Revert the scratch scaffold**

```bash
git checkout -- src/app/api.json src/app/handlers/routes_gen.go
rm -f src/app/models/Project.go src/app/models/Project_test.go \
      src/app/handlers/project_resource.go src/app/handlers/project_resource_test.go \
      src/app/static/pages/projects.html src/app/static/js/projects.js
git status --short
```
Expected: clean tree (only committed empty `api.json`/`routes_gen.go` remain, unmodified).

- [ ] **Step 10: Reset DB and final full suites**

```bash
docker compose down
rm -f data/app.db data/app.db-wal data/app.db-shm
docker compose up -d
until docker compose exec -T app true 2>/dev/null; do sleep 2; done
docker compose exec app go test ./...
cd src/builder && go test ./... && cd -
git status --short
git log --oneline main..HEAD | wc -l
```
Expected: both suites pass on the clean tree; tree clean; the log shows the Build 3b commits.

---

## Verification Summary

| Concern | Where proven |
|---|---|
| Injection-safe sort/filter whitelist | Task 1 (incl. injection strings) |
| GetPage sort/filter integration; cache key varies | Task 2, Task 6 Step 6 |
| Update roundtrip + missing-id → ErrNoRows | Task 2, Task 3, Task 6 |
| scaffold_list still works with new GetPage | Task 2 Step 6-7 |
| Five CRUD handlers, 422 on bad input, 404 on missing | Task 3, Task 6 Step 6 |
| Five endpoints registered with right kinds/handlers | Task 4, Task 6 Step 4 |
| main.go never hand-edited | Task 6 Step 4 |
| 3a export surfaces the full CRUD | Task 6 Step 7 |
| Whole chain on the rebuilt MCP image | Task 6 |
