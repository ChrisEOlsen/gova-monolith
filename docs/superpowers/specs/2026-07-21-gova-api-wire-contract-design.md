# GOVA API Wire Contract — Design

**Date:** 2026-07-21
**Status:** Approved
**Scope:** Build 1 of 3 in the "API-first for native clients" effort

---

## Problem

`gova-monolith` builds web apps. `gova-ios` translates them into SwiftUI apps by
*reverse-engineering* the running web app: `/export:mobile` reads Go structs under
`src/app/models/`, regexes `r.Get(...)` route registrations out of `main.go`, and
parses JS modules for `get()`/`post()`/`del()` call sites. Nothing in the monolith
publishes a contract, so every ambiguity in the web app becomes guesswork on the
iOS side.

Worse, several wire-format decisions are made implicitly by Go and papered over by
forgiving JavaScript. The web client survives them; a strictly-typed Swift decoder
does not. The monolith emits responses that only work because the only consumer is
lenient.

### Concrete defects this build fixes

1. **Empty list serializes as `null`.** `handlers/json.go` declares
   `Data any \`json:"data,omitempty"\``. A model's `GetAll()` returns a nil slice
   when there are zero rows; a nil slice inside a non-nil interface is not "empty"
   for `omitempty` purposes, so it marshals as `{"ok":true,"data":null}`. Web
   survives via `res.data ?? []` (`list_page.js.tmpl`). Swift decoding `[Item]`
   from `null` throws and the whole screen fails.

2. **Timestamp format undefined.** `CreatedAt time.Time` marshals with Go's default,
   RFC3339 *Nano* (`2026-07-21T18:45:00.123456789Z`). Swift's built-in `.iso8601`
   decoding strategy rejects fractional seconds. JS `new Date(...)` accepts anything,
   so the web never noticed.

3. **Nullability lost.** `model.go.tmpl` emits every field as a non-optional Go type.
   The builder never inspects the table it was told to model, so a `TEXT` column with
   no `NOT NULL` produces a Go `string` that panics-by-decode-failure on the client.
   `gova-ios`'s CLAUDE.md currently instructs the iOS agent to go read the SQL schema
   and infer this itself.

4. **Errors are bare strings.** `jsonError(w, "failed to load", 500)` gives a client
   no way to distinguish an expired session from a validation failure from a conflict.
   No per-field validation detail exists at all.

5. **Unbounded lists.** `GetAll()` returns every row, cached 5 minutes. A mobile
   client on cellular pulls the entire table on every screen open.

6. **No versioning.** Paths are unversioned `/api/...`. The web deploys instantly;
   App Store builds do not. One breaking change bricks every installed app with no
   diagnostic beyond a decode error.

Builds 2 and 3 (API manifest + auto-routing; resource scaffold + unified auth) depend
on the field metadata and route shape established here. This build is the foundation.

---

## Decisions

| Area | Decision |
|---|---|
| Envelope | Flat siblings — `error` stays a string, `code`/`fields`/`meta` added alongside |
| Timestamps | RFC3339, UTC, second precision, via a custom `models.Time` type |
| Nullability | `PRAGMA table_info` introspection, with a mismatch guard that fails the tool |
| Pagination | Always-on offset paging; default 50, max 200; `meta` carries total |
| Versioning | `/api/v1` path prefix plus a `_version` endpoint for client assertion |

Each was chosen over alternatives; rationale is inline in the sections below.

---

## §1 Envelope — `src/app/handlers/json.go`

```go
type envelope struct {
	OK     bool              `json:"ok"`
	Data   any               `json:"data,omitempty"`
	Meta   *Meta             `json:"meta,omitempty"`
	Error  string            `json:"error,omitempty"`
	Code   string            `json:"code,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

type Meta struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}
```

Success:

```json
{ "ok": true, "data": [ ... ], "meta": { "limit": 50, "offset": 0, "total": 123 } }
```

Error:

```json
{ "ok": false, "error": "Name is required",
  "code": "validation_failed", "fields": { "name": "required" } }
```

**Why flat siblings over a nested `error` object or bare HTTP semantics:** `error`
remains a plain string, so `api.js`, every JS template, `gova-ios`'s
`APIClient.swift`, and the documented `res.error ?? '...'` pattern all keep working
untouched. The machine-readable `code` is additive. A nested error object or an
RFC 7807 migration would force a rewrite of every consumer for no gain this build
can use.

### Null-data fix — two independent guards

- **Source:** `model.go.tmpl` initializes `items := []T{}` rather than `var items []T`,
  so a zero-row query returns an empty slice.
- **Boundary:** `jsonOK` reflects on `data`; if it is a nil slice, it substitutes an
  empty slice before marshaling.

The second guard is not redundant. It covers hand-written `create_handler` bodies,
which the template fix cannot reach.

### Error codes

Fixed set, derived from HTTP status:

| Status | Code |
|---|---|
| 401 | `unauthorized` |
| 403 | `forbidden` |
| 404 | `not_found` |
| 409 | `conflict` |
| 422 | `validation_failed` |
| 429 | `rate_limited` |
| 500 (and any unmapped status) | `internal` |

### Helpers

`jsonError(w, msg, status)` **keeps its exact existing signature** and now derives
`code` from `status` automatically. Every already-generated handler and every
`CLAUDE.md` example remains valid with no edit.

Added:

- `jsonErrorCode(w, code, msg, status)` — explicit code when the status is ambiguous
- `jsonValidationError(w, fields)` — emits 422 with `code: "validation_failed"` and
  the per-field map; `error` is set to a human-readable summary of the first field
- `jsonList(w, items, meta)` — success envelope with `meta` populated

---

## §2 Timestamps — new `src/app/models/time.go`

Hand-written infrastructure file (permitted by the CLAUDE.md infrastructure
exception — created once, not per-feature).

```go
type Time time.Time
```

Implements:

- `MarshalJSON` → `time.Time(t).UTC().Format(time.RFC3339)`, quoted
- `UnmarshalJSON` → parses RFC3339
- `sql.Scanner` — accepts `time.Time`, `string`, and `[]byte` source values
- `driver.Valuer` — for inserts

**The `sql.Scanner` implementation is load-bearing.** Every generated model does
`rows.Scan(&item.CreatedAt)`; without `Scanner`, swapping the field type from
`time.Time` to `models.Time` breaks every scan path in the codebase.

`model.go.tmpl` emits `CreatedAt Time` (the type is in the same `models` package).
Swift then decodes with the stock `.iso8601` strategy and no custom `DateFormatter`.

Sub-second precision is lost. This is acceptable: SQLite's `CURRENT_TIMESTAMP`,
the only timestamp source in generated schemas, has one-second resolution anyway.

---

## §3 Nullable fields — builder introspection

### Introspection and guard

`create_model` and `scaffold_list` run `PRAGMA table_info(<plural_name>)` against
`/data/app.db` before rendering anything. This is safe because the Golden Recipe
already mandates `execute_sql` first — `scaffold_list` does not create tables, so
the schema always exists by the time a model is generated.

For each entry in the `fields` argument:

- Column missing from the table → tool errors, generates nothing, and reports the
  actual column list
- Column type incompatible with the declared field type → same
- `notnull = 0` → the field is nullable

The `fields` argument thus becomes a *declaration of intent validated against the
schema*, not a second source of truth that can silently drift from it. Keeping the
argument (rather than deriving fields wholly from the table) preserves the explicit
shape in implementation plans and allows a model over a subset of columns.

### Generated output

A nullable field becomes a Go pointer type, marshals as JSON `null`, and maps to a
Swift optional:

```go
Notes *string `json:"notes"`
```

### Scan strategy

Nullable fields scan through explicit `sql.Null*` temporaries:

```go
var notesNull sql.NullString
if err := rows.Scan(&item.ID, &item.Name, &notesNull, &item.CreatedAt); err != nil {
	return nil, 0, err
}
if notesNull.Valid {
	item.Notes = &notesNull.String
}
```

**Why not scan directly into `**string`:** `database/sql`'s `convertAssign` does
handle pointer-to-pointer destinations for primitives, but its interaction with
custom `sql.Scanner` types (i.e. a nullable `*Time`) is not clearly specified.
`sql.Null*` temporaries — including `sql.NullTime` — behave identically for every
supported type. The generated code is more verbose; nobody reads generated model
internals, and correctness outranks brevity here.

Two new funcMap helpers in `builder/main.go`: `scanDecls` (emit the temporaries) and
`scanAssigns` (emit the post-scan pointer assignments).

### Other model methods

Nullability changes every method that touches the column, not just `GetPage`:

- **`Find(id)`** uses the same `sql.Null*` temporaries and pointer assignments as
  `GetPage`. Both share the `scanDecls`/`scanAssigns` helpers.
- **`Create(...)`** takes a pointer parameter for each nullable field
  (`Create(name string, notes *string)`). A `nil` argument inserts SQL `NULL`;
  `database/sql` binds a nil pointer to `NULL` directly, so no `sql.Null*` wrapper
  is needed on the insert path. The `createParams` and `insertArgs` funcMap helpers
  gain pointer awareness.
- **`Delete(id)`** is unaffected.

Password fields remain non-nullable by construction — `bcrypt` hashing in `Create`
assumes a value is present, and a nullable password column is not a shape this
scaffold supports. The mismatch guard rejects a `password`-typed field declared
against a column with `notnull = 0`.

---

## §4 Pagination

### Model

`GetAll()` is **replaced** by:

```go
func (m *XModel) GetPage(limit, offset int) ([]X, int, error)
```

returning the page, the total row count, and an error. The only generated caller is
the list handler, so nothing is orphaned. `model_test.go.tmpl` and the
`model.GetAll()` example in `CLAUDE.md` update alongside it.

Clamping happens in the handler, not the model: `limit` defaults to 50, minimum 1,
maximum 200; `offset` minimum 0. Non-numeric input falls back to the default.

### Cache

Key becomes `{plural}:page:{limit}:{offset}`, storing `{items, total}`. The existing
`m.cache.Bust("{plural}:")` prefix sweep in `Create`/`Delete` already invalidates
every page under that prefix — no change needed to invalidation logic.

### Handler

`list_handler.go.tmpl` reads and clamps `limit`/`offset` from the query string,
calls `GetPage`, and responds via `jsonList(w, items, meta)`.

---

## §5 Route surface — `/api/v1`

All generated API paths move under `/api/v1/`, auth routes included:

```
GET    /api/v1/projects
POST   /api/v1/projects
DELETE /api/v1/projects/{id}
POST   /api/v1/auth/login
POST   /api/v1/auth/login_token
```

This touches the "Next: wire route in main.go" instruction strings emitted by every
scaffold tool, plus the paths hardcoded in `login.js.tmpl`, `register.js.tmpl`,
`list_page.js.tmpl`, and `mobile_auth_handler.go.tmpl`.

**Routes remain hand-pasted into `main.go` in this build.** Auto-registration is
Build 2. This build replaces the prose comment block in `main.go` with a
`// @gova-routes` marker so Build 2 has a stable anchor to write against — zero
behavioral risk now, and it removes a blocking prerequisite later.

### Cross-repo impact

`scaffold_mobile_auth`'s endpoints become `/api/v1/auth/login_token`,
`/api/v1/auth/logout_token`, `/api/v1/auth/me_token`. `gova-ios`'s `CLAUDE.md`
Step 2 documents the old paths verbatim and must be patched in the same change.
This is a documentation-only edit in that repo — no Swift source is touched, since
`APIClient` takes full paths from its callers.

---

## §6 Version endpoint

New `src/app/handlers/version.go`:

```go
const (
	APIVersion       = "1.0.0"
	MinClientVersion = "1.0.0"
)
```

Served at `GET /api/v1/_version`:

```json
{ "ok": true, "data": { "api_version": "1.0.0", "min_client_version": "1.0.0" } }
```

A path prefix alone does nothing for stale App Store builds — it only makes a future
`/api/v2` possible. The assertion signal is what turns an opaque decode failure into
an actionable "update required" prompt.

Both constants are bumped by hand in this build. Build 2's manifest is the natural
owner once it can hash the route and model set. The iOS launch-time assertion is
Build 3 work; this build only guarantees the endpoint exists to assert against.

---

## §7 `api.js`

One fix. `get()` currently discards the server's response body on any non-2xx status
other than 401, synthesizing `{ok: false, error: 'HTTP 500'}` — which would throw
away the new `code` and `fields`. It changes to attempt `res.json()` first and
synthesize only if parsing fails.

`post`, `put`, and `del` already return the parsed body unmodified, so `code` and
`fields` reach callers with no change. That minimal client churn is the direct
payoff of the flat-sibling envelope decision.

---

## §8 Testing

New test files:

- `src/app/handlers/json_test.go` — nil slice renders as `[]` not `null`;
  status-to-code mapping across the full table; `meta` shape; validation `fields`
  passthrough; `jsonError`'s preserved signature still produces a code
- `src/app/models/time_test.go` — marshal output has no fractional seconds and is
  UTC-normalized; `Scan` roundtrips from `time.Time`, `string`, and `[]byte`;
  `Valuer` output survives a roundtrip
- `src/builder/pragma_test.go` — introspection against a temp SQLite database:
  nullability detected correctly per column, and the mismatch guard errors without
  writing files when a declared field is absent or type-incompatible

Extended:

- `src/builder/render_test.go` — golden assertions that rendered templates contain
  `*string` for a nullable field, `GetPage`, `/api/v1`, and `Time` rather than
  `time.Time`
- `model_test.go.tmpl` — paging math across a page boundary, total count
  correctness, nullable field roundtrip through `Create` and `GetPage`
- `list_handler_test.go.tmpl` — limit and offset clamping at both bounds, empty
  table returns `[]` with `total: 0`, `meta` matches the requested window

Verification commands:

```bash
docker compose exec app go test ./...
cd src/builder && go test ./...
```

Both are required. The builder suite is where template regressions surface and is
currently the thinner of the two.

---

## §9 Scope boundary

Applications already scaffolded from this template have pre-v1 route paths and
`GetAll`-based models. **No migration tooling is provided — regenerate them.** This
is a template repository; the generated apps are outputs, not dependents.

Explicitly out of scope for this build, deferred to Builds 2 and 3:

- API manifest generation and the `_manifest` endpoint (Build 2)
- Automatic route registration into `main.go` (Build 2)
- Structured JSON output from `inspect_app` (Build 2)
- Detail, update, and delete endpoint scaffolding (Build 3)
- Server-side sort and filter parameters (Build 3)
- Unified cookie-plus-bearer auth in `scaffold_auth` (Build 3)
- Rewriting `gova-ios`'s `/export:mobile` to consume the manifest (Build 3)
- The iOS launch-time version assertion and upgrade prompt (Build 3)

---

## Files touched

**`gova-monolith`**

| File | Change |
|---|---|
| `src/app/handlers/json.go` | New envelope fields, code mapping, nil-slice guard, new helpers |
| `src/app/handlers/version.go` | New — version constants and handler |
| `src/app/handlers/json_test.go` | New |
| `src/app/models/time.go` | New — `Time` with Marshal/Unmarshal/Scan/Value |
| `src/app/models/time_test.go` | New |
| `src/app/main.go` | `// @gova-routes` marker, `_version` route registration |
| `src/app/static/js/lib/api.js` | `get()` preserves the server error body |
| `src/builder/main.go` | PRAGMA introspection, mismatch guard, `scanDecls`/`scanAssigns` funcMap helpers |
| `src/builder/render_test.go` | Extended golden assertions |
| `src/builder/pragma_test.go` | New |
| `src/builder/templates/model.go.tmpl` | `Time`, nullable pointers, `GetPage`, non-nil slice init |
| `src/builder/templates/model_test.go.tmpl` | Paging and nullable coverage |
| `src/builder/templates/list_handler.go.tmpl` | Query param clamping, `jsonList` |
| `src/builder/templates/list_handler_test.go.tmpl` | Clamping and meta coverage |
| `src/builder/templates/list_page.js.tmpl` | `/api/v1` path |
| `src/builder/templates/js_form.js.tmpl` | `/api/v1` path, surfaces `fields` on validation errors |
| `src/builder/templates/login.js.tmpl` | `/api/v1` path |
| `src/builder/templates/register.js.tmpl` | `/api/v1` path |
| `src/builder/templates/mobile_auth_handler.go.tmpl` | `/api/v1` paths |
| `CLAUDE.md` | `GetPage` replaces `GetAll` in examples; envelope and error-code documentation |

**`gova-ios`**

| File | Change |
|---|---|
| `CLAUDE.md` | Step 2 mobile auth endpoint paths updated to `/api/v1/` |
