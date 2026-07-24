# scaffold_resource — Full CRUD — Design

**Date:** 2026-07-23
**Status:** Approved
**Scope:** Build 3b of 3 (the "3" build split into 3a/3b/3c) in the "API-first for native clients" effort. Monolith-side.

---

## Problem

Today the only resource scaffolder is `scaffold_list`, which generates a
**list-only** endpoint (`GET /api/v1/{plural}`). The model it emits already has
`Find`, `Create`, and `Delete` methods, but there are no detail, create, update,
or delete *handlers* and no routes for them — and the model has no `Update`
method at all. So a scaffolded resource exposes reading a page of rows and
nothing else.

Native clients need the full surface. Build 3a made `gova-ios` consume the
manifest and generate screens from it; a list screen with no detail view, no
create/edit form, and no delete is a thin app. The endpoints that would back
those screens don't exist to be manifested.

This build adds `scaffold_resource`, generating the complete CRUD surface —
list, detail, create, update, delete — all self-registering into the manifest
(Build 2), so 3a's export surfaces them with **no change** (it already groups
every endpoint by model regardless of kind). The list endpoint also gains
whitelisted sort and equality-filter query parameters.

`scaffold_list` stays for genuinely read-only / reference-data resources.

---

## Decisions

| Area | Decision |
|---|---|
| Tool | New `scaffold_resource(name, fields)`; `scaffold_list` unchanged and kept |
| Endpoints | 5: list / detail / create / update / delete, all `auth:false` (public) |
| Auth | No auth flag on the tool — resources are public; protect per-endpoint via the manifest afterward (Build 2 made auth declarative) |
| Update semantics | PUT full-replace (not PATCH) |
| Sort/filter | `?sort=` (± direction) and `?filter=field:value`, columns whitelisted against the model's own fields, values parameterized; unknown column → 422 |
| Sort/filter code | Shared, hand-written, unit-tested `models/query.go` — not regenerated per resource |
| Frontend | List page + create form only (same as `scaffold_list`); no bespoke detail/edit/delete web UI |
| Not-found | detail & update on a missing id → 404; delete is idempotent |

---

## §1 New tool `scaffold_resource`

`scaffold_resource(name, fields)` — same arguments as `scaffold_list`. It runs the
same schema-introspection guard (`applySchema`, Build 1) and generates:

**Model** (`models/{Pascal}.go`): the existing model **plus** an `Update` method
and a sort/filter-aware `GetPage` (see §2). Generated with a `CRUD` template flag
set true.

**Handlers** (`handlers/{name}_resource.go` + `_test.go`): all five handlers
(§4).

**Frontend** (`static/pages/{plural}.html` + `static/js/{plural}.js`): the same
list page `scaffold_list` produces (list view + the `// @inject-forms` marker for
`add_js_form`). No detail/edit/delete UI.

**Manifest** (Build 2 `updateManifest`): five endpoints —

| Method + path | Handler | deps | kind |
|---|---|---|---|
| `GET /api/v1/{plural}` | `{P}ListGET` | read,write,cache | `list` |
| `GET /api/v1/{plural}/{id}` | `{P}DetailGET` | read,write,cache | `detail` |
| `POST /api/v1/{plural}` | `{P}CreatePOST` | read,write,cache | `create` |
| `PUT /api/v1/{plural}/{id}` | `{P}UpdatePUT` | read,write,cache | `update` |
| `DELETE /api/v1/{plural}/{id}` | `{P}DeleteDELETE` | read,write,cache | `delete` |

All `auth:false`, `model:{name}`. The `{id}` segment is chi's native path param.
The list path and the `/{id}` paths are distinct `(method,path)` keys, so no
manifest conflict. `routes_gen.go` already emits `r.Get("…/{id}", …)`; chi routes
it. The five `kind` values are already in Build 2's manifest vocabulary and 3a's
export groups endpoints by model regardless of kind — **no 3a change is
required**.

**Idempotency & conflict:** re-running `scaffold_resource` upserts the same five
endpoints (no-op). A resource previously scaffolded with `scaffold_list` (which
registered `GET /api/v1/{plural}` → `{P}ListGET`) then re-scaffolded with
`scaffold_resource` produces the **same** list handler symbol `{P}ListGET`, so the
list endpoint upserts cleanly rather than conflicting; the four new endpoints are
added.

---

## §2 Shared query infrastructure — `models/query.go` (hand-written)

The sort/filter validation is the SQL-injection surface, so it is written once,
by hand, and unit-tested — never regenerated per resource. New infrastructure
file (permitted by CLAUDE.md's infrastructure exception, like `models/time.go`).

```go
package models

var ErrInvalidQuery = errors.New("invalid query parameter")

// QueryOpts carries validated-at-the-boundary list options. Empty fields mean
// "not requested" (default ordering, no filter).
type QueryOpts struct {
	Sort        string // e.g. "name" or "-created_at"; "" = default
	FilterField string // "" = no filter
	FilterValue string
}

// orderByClause returns a safe "ORDER BY <col> ASC|DESC" for a sort spec whose
// column is in allowed. A leading '-' means DESC. "" → the default
// "ORDER BY created_at DESC". An unknown column → ErrInvalidQuery.
func orderByClause(sort string, allowed []string) (string, error) { ... }

// filterField returns the validated column name (safe to interpolate) for a
// filter whose column is in allowed, or ErrInvalidQuery. The value is always
// bound as a ? parameter by the caller, never interpolated.
func filterField(field string, allowed []string) (string, error) { ... }
```

**Why interpolating the column is safe:** `allowed` is a fixed list of the
model's real column names generated at scaffold time (a Go string slice literal),
not user input. `orderByClause`/`filterField` return a column only if it is
*exactly equal* to a member of that list; anything else is `ErrInvalidQuery`. So
the string placed into the SQL is always one of a known-safe set. Filter *values*
are never interpolated — they are bound `?` parameters.

`models/query_test.go` covers: valid asc/desc sort; unknown sort column →
`ErrInvalidQuery`; empty sort → default `created_at DESC`; valid filter field;
unknown filter field → `ErrInvalidQuery`; and that a would-be-injection column
string (`"name; DROP TABLE"`) is rejected because it is not exactly in `allowed`.

---

## §3 Model changes — `model.go.tmpl`

### `GetPage` gains sort/filter (signature change — coordinated)

New signature, used by **both** tools:

```go
func (m *XModel) GetPage(limit, offset int, opts QueryOpts) ([]X, int, error)
```

The model declares its own column whitelist (generated literal):

```go
var xAllowedColumns = []string{"id", <fields...>, "created_at"}
```

`GetPage` calls `orderByClause(opts.Sort, xAllowedColumns)` and, when
`opts.FilterField != ""`, `filterField(opts.FilterField, xAllowedColumns)`;
either returning `ErrInvalidQuery` propagates out unchanged. The filter, when
present, is applied to **both** the `COUNT` and the `SELECT`, so `meta.total`
reflects the filtered set. The `ORDER BY created_at DESC` literal is replaced by
the validated `orderBy` string.

**Cache key must include sort/filter** — otherwise two different queries share a
cache entry (a correctness bug). New key:
`{plural}:page:{limit}:{offset}:{sort}:{filterField}:{filterValue}`. The existing
`Bust("{plural}:")` prefix sweep still invalidates every variant.

### `Update` method (gated by the `CRUD` flag)

Emitted only when the template data's `CRUD` flag is true (so `scaffold_list`
models stay lean):

```go
func (m *XModel) Update(id int64, <fields...>) error
```

Full replace: `UPDATE {plural} SET field1 = ?, field2 = ? WHERE id = ?`. A
`password`-typed field is re-hashed with bcrypt (mirroring `Create`). Nullable
fields take pointer parameters, binding `nil` → SQL NULL, exactly as `Create`
does. After `Exec`, if `RowsAffected() == 0` the id did not exist → return
`sql.ErrNoRows` (the handler maps it to 404). On success, `Bust("{plural}:")`.

New funcMap helper `updateSet` (emits `field1 = ?, field2 = ?`); `createParams`,
`insertArgs`, `placeholders`, `joinNames` are reused.

### Coordinated `scaffold_list` update

`scaffold_list`'s existing list handler (`list_handler.go.tmpl`) calls
`GetPage(limit, offset)`. It is updated to `GetPage(limit, offset, models.QueryOpts{})`
— zero-value opts → default `created_at DESC`, no filter. `scaffold_list` gains
no sort/filter query params (it stays simple); it only adapts to the new
signature.

---

## §4 Handlers — `{name}_resource.go`

One generated file with all five handlers, each constructed as
`{P}<Verb>(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc`
(deps `read,write,cache`). All responses use the Build 1 envelope helpers.

- **`{P}ListGET`** — parse `?limit`/`?offset` (clamped, as today) and `?sort`/
  `?filter` into `QueryOpts` (split `filter` on the first `:`), call
  `GetPage(limit, offset, opts)`. `errors.Is(err, models.ErrInvalidQuery)` →
  `jsonError(w, "<message naming allowed columns>", 422)` (→ `validation_failed`
  code via Build 1's status→code mapping; NOTE: `400` is NOT in that mapping and
  would mislabel as `internal`, so client-input errors here use `422`). Other error → 500. Success → `jsonList(w, items, Meta{…})`.
- **`{P}DetailGET`** — `id` from `chi.URLParam`; non-numeric id → 422.
  `Find(id)`; `sql.ErrNoRows` → 404 `not_found`; other error → 500; else
  `jsonOK(w, item)`.
- **`{P}CreatePOST`** — decode the JSON body into a generated request struct
  (fields as `Create`'s params, nullable fields as pointers); decode failure →
  422. `Create(...)`; error → 500; success → `jsonOK(w, map{"id": newID})`.
- **`{P}UpdatePUT`** — `id` from path (422 if non-numeric); decode body → 422;
  `Update(id, ...)`; `sql.ErrNoRows` → 404; other error → 500; success →
  `jsonOK(w, map{"id": id})`.
- **`{P}DeleteDELETE`** — `id` from path (422 if non-numeric); `Delete(id)`;
  error → 500; success → `jsonOK(w, map{"ok": true})`. Idempotent: deleting a
  non-existent id is a success (matches `Delete`'s current behavior).

**Create validation is intentionally coarse** — a malformed body is 422, a model
error is 500. Per-field `422 validation_failed` (via `jsonValidationError`) is a
deferred enhancement: cleanly determining "required" for a string field is fuzzy,
and this build's value is the CRUD surface + safe sort/filter, not a validation
framework. Documented in §7.

`chi` is already a dependency; the resource handler imports it for `URLParam`.

---

## §5 Frontend

`scaffold_resource` generates the same `list_page.html.tmpl` / `list_page.js.tmpl`
output as `scaffold_list` — a list view plus the `// @inject-forms` marker so
`add_js_form` can add a create form. No detail view, edit form, or delete button
in JS/HTML.

Rationale: the arc is API-first; iOS is the full-CRUD consumer and gets
detail/update/delete from the manifest. The web frontend is the least-tested
layer (no JS test runner), and generating correct edit/delete UI is a large,
low-value surface for this build. A developer who wants web CRUD UI uses
`create_page`/`add_js_form`.

---

## §6 Testing

**Shared infra (`src/app/models/query_test.go`, host-in-Docker):** the whitelist
logic per §2 — valid/invalid sort and filter, default ordering, and an
injection-shaped column string rejected.

**Generated model tests (`model_test.go.tmpl`, extended):** `Update` roundtrip
(create → update → Find shows new values); `Update` on a missing id → `sql.ErrNoRows`;
`GetPage` with a valid sort (order changes) and a valid filter (subset + correct
`total`); `GetPage` with an unknown sort column → `ErrInvalidQuery`. Existing
Find/Create/Delete/paging coverage stays.

**Generated handler tests (`{name}_resource_test.go.tmpl`, new):** each of the
five — detail 200 and 404; create 200 with an id; update 200 and 404; delete 200;
list with `?sort=` (order), `?filter=` (subset + meta.total), and
`?sort=bogus` → 422.

**Builder suite (host):** `render_test.go` — the new resource-handler and
resource-model templates render valid Go, including `{id}` route references and
the `CRUD`-gated `Update`; `scaffold_list`'s model still renders valid Go with the
new `GetPage` signature and passes `QueryOpts{}`. A `scaffold_resource` manifest
test asserting exactly the five endpoints with the right kinds/paths/handlers.

**End-to-end (rebuilt MCP, as in prior builds):** `execute_sql` +
`scaffold_resource` → assert `api.json` has 5 endpoints; `routes_gen.go` mounts
all five including the `{id}` and `RequireAuth`-free routes; restart app; live-hit
each — create a row, detail it, update it, list with sort+filter, delete it — and
confirm 3a's export (`export_manifest.py` against this `api.json`) lists all five
under the resource. Then a conflict/idempotency check and revert. **Rebuild the
mcp image first** (`docker compose up -d --build`) — templates are `go:embed`-ed
at image build time.

---

## §7 Documentation

- **`CLAUDE.md`:** add `scaffold_resource` to the Tool Cheat Sheet (full CRUD,
  generates tests; sort/filter on list; public by default, protect via manifest);
  note `scaffold_list` remains for read-only. Document the `?sort=`/`?filter=`
  contract and the whitelist in the API Manifest & Routing / patterns section.
  Note that per-field create validation is coarse (422/500), a deferred
  enhancement.
- **`../gova-ios/CLAUDE.md`:** a brief note that a scaffolded resource now exposes
  detail/create/update/delete (kinds in the manifest), so `/build` can generate
  richer screens — but do NOT rewrite `/build`'s Swift generation here (that is
  downstream iOS work, out of this monolith build's scope).

---

## §8 Scope boundary

Out of scope, deferred:
- **Unified auth** (3c).
- **Per-field `422` create/update validation** — coarse 400/500 for now.
- **Filter operators beyond equality** (`LIKE`, ranges, `IN`) and multi-column
  filtering — single equality filter only.
- **Web detail/edit/delete UI** — API + list/create page only.
- **`PATCH` partial update** — PUT full-replace only.
- **Migrating existing apps** — regenerate; no migration tooling. Apps scaffolded
  with `scaffold_list` keep working (the coordinated `GetPage` change ships with
  the template, so regenerating picks it up).
- **Rewriting gova-ios `/build`** to generate detail/edit/delete Swift screens —
  a later iOS build; the manifest now carries the endpoints for it.

---

## Files touched (`gova-monolith`, plus one gova-ios doc note)

| File | Change |
|---|---|
| `src/app/models/query.go` | New — `ErrInvalidQuery`, `QueryOpts`, `orderByClause`, `filterField` |
| `src/app/models/query_test.go` | New |
| `src/builder/templates/model.go.tmpl` | `GetPage(…, opts)` + whitelist; `CRUD`-gated `Update`; cache key includes sort/filter |
| `src/builder/templates/model_test.go.tmpl` | Update + sort/filter coverage |
| `src/builder/templates/list_handler.go.tmpl` | Pass `models.QueryOpts{}` to the new `GetPage` |
| `src/builder/templates/resource_handlers.go.tmpl` | New — the five CRUD handlers |
| `src/builder/templates/resource_handlers_test.go.tmpl` | New |
| `src/builder/main.go` | New `scaffold_resource` tool + handler; `updateSet` funcMap helper; `CRUD` flag in template data; register 5 endpoints |
| `src/builder/render_test.go` | New-template render assertions; `scaffold_list` still-valid assertion |
| `src/builder/manifest_test.go` or a new builder test | `scaffold_resource` registers the 5 endpoints |
| `CLAUDE.md` | `scaffold_resource` cheat-sheet + sort/filter contract |
| `../gova-ios/CLAUDE.md` | Brief note: resources now expose full CRUD in the manifest |
