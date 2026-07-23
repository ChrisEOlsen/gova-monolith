# GOVA API Manifest + Auto-Routing — Design

**Date:** 2026-07-23
**Status:** Approved
**Scope:** Build 2 of 3 in the "API-first for native clients" effort

---

## Problem

When it is time to build the iOS app, `gova-ios` does not *know* the API — it
*guesses* it. Its `/export:mobile` step reads the monolith's Go struct files to
infer field types, greps `main.go` for `r.Get(...)` route registrations, parses
JS modules for `get()`/`post()`/`del()` call sites, and calls `inspect_app`,
which returns prose. It reverse-engineers a contract out of source that was
never meant to be one. Every ambiguity in that source becomes a guess on the
iOS side, and a wrong guess is a broken screen.

Build 1 fixed the *wire* — the bytes on the socket are now strict and correct
(non-null lists, pinned timestamps, nullable columns serialized as JSON `null`,
`/api/v1`). But nothing *describes* that wire. The nullability that Build 1's
`PRAGMA` introspection discovered dies inside the generator: it shapes the Go
struct, but is never published anywhere a client can read it. iOS still has to
re-derive "is this field optional?" by cross-reading the SQL schema.

Two defects from the original analysis remain:

- **#5 — routes are hand-wired.** Scaffold tools print "Next: wire route in
  `main.go`" and the LLM edits `main.go` by hand. Paths drift, and there is no
  canonical list of what endpoints exist.
- **#11 — `inspect_app` returns prose.** Filename globs and a route regex,
  formatted as text. iOS parses it as text.

This build publishes a single machine-readable contract and makes that contract
the source of truth for routing, so the description and the actual routes can
never diverge. Build 3 then rewrites `gova-ios`'s `/export:mobile` to read the
manifest instead of parsing source — turning iOS translation from inference
into lookup.

---

## Decisions

| Area | Decision |
|---|---|
| Routing mechanism | `api.json` is the source of truth; a generated `routes_gen.go` and the served `_manifest` are both derived from it |
| Routing scope | All route-producing tools self-register — `scaffold_*` **and** `create_handler`/`create_page` (which gain `method`+`path` args) |
| Surface-change signal | `api_version` stays a hand-set semver; a manifest content `hash` is added and echoed by `_version` as `manifest_hash` |
| `inspect_app` | Returns structured JSON: `manifest` + `on_disk` scan + `divergence[]` |
| Idempotency | Upsert by key — endpoints by `(method, path)`, models by `name`. Re-scaffolding replaces; a conflicting claim errors |
| Auth enforcement | Unified on a mount-time `RequireAuth` wrap driven by the manifest's `auth` flag (replaces `create_handler`'s inline check) |

Each was chosen over alternatives during brainstorming; rationale is inline
below.

---

## §1 Source of truth — `src/app/api.json`

A single JSON document at the app-module root, owned by the builder, read by the
app.

```json
{
  "api_version": "1.0.0",
  "hash": "sha256:9f2a1c…",
  "generated_at": "2026-07-23T00:00:00Z",
  "models": [
    {
      "name": "project",
      "table": "projects",
      "fields": [
        { "name": "id",         "type": "int",       "nullable": false },
        { "name": "name",       "type": "string",    "nullable": false },
        { "name": "notes",      "type": "string",    "nullable": true  },
        { "name": "created_at", "type": "timestamp", "nullable": false }
      ]
    }
  ],
  "endpoints": [
    {
      "method": "GET",
      "path": "/api/v1/projects",
      "handler": "ProjectListGET",
      "deps": ["read", "write", "cache"],
      "auth": false,
      "model": "project",
      "kind": "list"
    }
  ]
}
```

### Field semantics

- **`models[].fields[].type`** — one of `int`, `string`, `boolean`, `float`,
  `timestamp`. `timestamp` is the manifest name for `created_at`'s
  `models.Time`; it tells iOS to decode with `.iso8601`. `nullable` is exactly
  the value Build 1's `PRAGMA table_info` introspection produced. **This record
  is the payload that finally carries nullability to iOS**, closing the
  original leak #3 end to end.
- **`endpoints[].handler`** — the exact Go symbol the route mounts (e.g.
  `ProjectListGET`). It must name a real exported function in the `handlers`
  package, or `routes_gen.go` will not compile. The tool that writes the record
  is the same tool that generates the handler, so the name is always accurate.
- **`endpoints[].deps`** — the dependency arguments the handler constructor
  takes, drawn from a closed set: `read` (`database.Read`), `write`
  (`database.Write`), `cache` (`appCache`). Order in the array is the argument
  order. Examples from existing templates: a list handler is
  `["read","write","cache"]`; `MobileLogoutDELETE(database.Write)` is
  `["write"]`; `LogoutPOST()` is `[]`.
- **`endpoints[].auth`** — `true` means the client must be authenticated. It is
  the iOS-facing truth *and* the enforcement signal: `routes_gen.go` wraps the
  route in `middleware.RequireAuth` when it is `true` (see §2 and §6).
- **`endpoints[].model`** — the related model `name`, or omitted for endpoints
  with no model (e.g. auth). Lets iOS associate an endpoint with the struct it
  returns.
- **`endpoints[].kind`** — a semantic tag from a closed set: `list`, `detail`,
  `create`, `delete`, `auth_login`, `auth_logout`, `auth_me`, `register`,
  `mobile_login`, `mobile_logout`, `mobile_me`, `custom`. Gives iOS the intent
  of the endpoint without pattern-matching the path.
- **`hash`** — `sha256:` over a canonical serialization of `models` and
  `endpoints` (keys sorted, `generated_at` excluded), so it is stable across
  regenerations that change nothing. Recomputed on every mutation.
- **`generated_at`** — RFC3339 UTC timestamp of the last mutation. Informational;
  excluded from the hash.

### Mutation semantics — upsert with conflict detection

Every route-producing tool performs a read-modify-write on `api.json`:

1. Read the current document (treat a missing file as the empty manifest
   `{api_version, hash, generated_at, models:[], endpoints:[]}`).
2. **Upsert models by `name`** and **endpoints by `(method, path)`**:
   replace an existing record with the same key, else append.
3. **Conflict rule:** if an incoming endpoint's `(method, path)` already exists
   but names a *different* `handler`, the tool errors and writes nothing —
   neither `api.json` nor any generated file. This prevents one scaffold from
   silently clobbering another's route. (Re-running the *same* scaffold, which
   produces the same handler for the same path, is a no-op upsert, not a
   conflict — this is what makes re-scaffolding idempotent.)
4. Recompute `hash`, set `generated_at`, write `api.json`.
5. Regenerate `routes_gen.go` from the full endpoint set (§2).

There is **no removal tool.** Upsert only; `api.json` grows. Removing a resource
is out of scope (documented in §9). `api.json` is committed to the repo — it is
real source, not a build artifact.

---

## §2 Derived artifact — `src/app/handlers/routes_gen.go`

Regenerated in full from `api.json` after every mutation. Endpoints are emitted
sorted by `(path, method)` so the file is byte-stable across regenerations that
do not change the set — clean diffs, no spurious churn.

```go
// Code generated from api.json by gova-builder. DO NOT EDIT.
package handlers

import (
	"github.com/go-chi/chi/v5"
	"gova/app/cache"
	"gova/app/db"
	"gova/app/middleware"
)

// RegisterGenerated mounts every scaffolded API route. main.go calls this once;
// it is never hand-edited. Framework endpoints (_version, _manifest) are wired
// directly in main.go, not here.
func RegisterGenerated(r chi.Router, database *db.DB, appCache *cache.Cache) {
	r.Get("/api/v1/projects", ProjectListGET(database.Read, database.Write, appCache))
	r.With(middleware.RequireAuth).Post("/api/v1/projects", ProjectCreatePOST(database.Read, database.Write, appCache))
}
```

- **`deps` drives the constructor call.** `read`→`database.Read`,
  `write`→`database.Write`, `cache`→`appCache`, in array order. `[]` emits
  `LogoutPOST()`.
- **`auth:true` emits the `r.With(middleware.RequireAuth)` wrap.** This is the
  single, declarative auth-enforcement point (see §6).
- **Imports are conditional.** `middleware` is imported only if any endpoint has
  `auth:true`; `db`/`cache`/`chi` are always needed. The generator emits exactly
  the imports the body uses, so the file always compiles with no unused imports.
- **A committed empty `routes_gen.go`** (a valid `RegisterGenerated` with an
  empty body and only the always-needed imports) ships in the repo, so a fresh
  app compiles before anything is scaffolded. The first scaffold overwrites it.

Generation uses a new builder template, `routes_gen.go.tmpl`, and a small amount
of generator logic to compute the import set and the per-endpoint call
expression. The template output is validated by `renderAndParse` (parses as Go)
in the builder suite, exactly like every other template.

---

## §3 `main.go` — one line, forever

The `@gova-routes` marker block (from Build 1) is replaced:

```go
	// API — framework endpoints
	r.Get("/api/v1/_version",  handlers.VersionGET())
	r.Get("/api/v1/_manifest", handlers.ManifestGET())

	// Generated API routes (source of truth: api.json → handlers/routes_gen.go)
	handlers.RegisterGenerated(r, database, appCache)
```

`_version` and `_manifest` stay hand-wired framework endpoints — they exist
before any resource is scaffolded, so they do not belong in the generated set.
`RegisterGenerated` owns everything scaffolded. **`main.go` is never edited for
a route again** — the defect #5 fix.

The `@gova-routes` marker is removed; Build 3 and beyond target
`RegisterGenerated`/`api.json`, not a text marker.

---

## §4 New and changed app endpoints

### `GET /api/v1/_manifest` — new, `handlers/manifest.go`

`ManifestGET` reads `./api.json` fresh on each request (the file is tiny and the
endpoint is low-traffic), and serves its parsed contents through the standard
envelope:

```json
{ "ok": true, "data": { "api_version": "...", "hash": "...", "models": [...], "endpoints": [...] } }
```

If `api.json` does not exist yet (fresh app, nothing scaffolded), it serves an
empty manifest (`models:[]`, `endpoints:[]`) with `ok:true`, not a 404 — a fresh
app has an empty but valid contract.

**Why read per-request rather than `go:embed`:** `api.json` lives at the app
module root, which the `handlers` package cannot reach with `//go:embed` (embed
cannot cross into a parent directory). A per-request `os.ReadFile("./api.json")`
is consistent with how the app already serves `./static`, needs no build-time
step, and is always fresh. The cost is one small disk read per manifest call,
which is negligible.

### `GET /api/v1/_version` — changed, `handlers/version.go`

Gains `manifest_hash`, read from `api.json`'s `hash` field (empty string if
`api.json` is absent). The two existing constants are unchanged:

```json
{ "ok": true, "data": { "api_version": "1.0.0", "min_client_version": "1.0.0", "manifest_hash": "sha256:9f2a…" } }
```

A human still bumps `api_version` on a breaking change; `manifest_hash` gives a
client or CI an automatic signal that *any* part of the surface changed.

---

## §5 `inspect_app` → structured JSON

`inspect_app` stops returning prose. It returns JSON with three parts:

```json
{
  "manifest": { "models": [...], "endpoints": [...] },
  "on_disk": {
    "models":   ["Project.go"],
    "handlers": ["project_list.go", "routes_gen.go"],
    "pages":    ["projects.html"],
    "js":       ["projects.js"]
  },
  "divergence": [
    "api.json lists model 'task' but src/app/models/Task.go is missing"
  ]
}
```

- **`manifest`** — the parsed `api.json` (empty arrays if absent).
- **`on_disk`** — a scan of the actual files (the current prose scan, restructured).
- **`divergence`** — human-readable mismatches: a model or endpoint in `api.json`
  whose backing file is missing, or a generated file present with no manifest
  record. Empty array when consistent.

This keeps `inspect_app`'s original purpose — "what exists, so I don't create
duplicates" — but machine-readable, and it catches a hand-deleted file that
`api.json` still lists (the failure mode that a blind "just serve the manifest"
would miss).

---

## §6 Tool changes

### The full scaffolders — self-register instead of printing instructions

`scaffold_list`, `scaffold_auth`, `scaffold_registration`, `scaffold_mobile_auth`
currently end by printing `r.Post(...)` lines for the LLM to paste into
`main.go`. They now instead:

1. Upsert their model(s) and endpoint(s) into `api.json` (§1).
2. Regenerate `routes_gen.go` (§2).
3. Report `"Registered N routes; manifest updated."` — no paste instructions.

The exact endpoint records each emits (handler symbol, deps, auth, kind) are
enumerated in the implementation plan, derived from the existing templates:

| Tool | Endpoints registered |
|---|---|
| `scaffold_list` | `GET {list}` → `XListGET`, deps `read,write,cache`, kind `list` |
| `scaffold_auth` | `POST /api/v1/auth/login` (`LoginPOST`, rwc, `auth_login`), `POST /api/v1/auth/logout` (`LogoutPOST`, `[]`, `auth_logout`), `GET /api/v1/auth/me` (`MeGET`, rwc, `auth_me`, `auth:true`) |
| `scaffold_registration` | `POST /api/v1/auth/register` (`RegisterPOST`, rwc, `register`) |
| `scaffold_mobile_auth` | `POST /api/v1/auth/login_token` (`MobileLoginPOST`, rwc, `mobile_login`), `DELETE /api/v1/auth/logout_token` (`MobileLogoutDELETE`, `write`, `mobile_logout`, `auth:true`), `GET /api/v1/auth/me_token` (`MobileMeGET`, rwc, `mobile_me`, `auth:true`) |

### `create_handler` / `create_page` — gain `method` + `path`, self-register

`create_handler` currently takes `name`, `method`, `auth_required` and produces a
stub the LLM wires by hand. It gains a required `path` argument and self-registers
its endpoint (handler `{Pascal}{Method}`, deps `read,write,cache`, `kind: custom`,
`auth` from `auth_required`). `create_page` likewise gains `method`+`path` for the
Go handler stub it emits and registers that endpoint.

### Auth enforcement unified on the route wrap

`create_handler`'s template currently enforces auth *inline*
(`if middleware.UserID(r) == 0 { jsonError(...); return }`). With `routes_gen.go`
now wrapping `auth:true` routes in `middleware.RequireAuth`, that inline check
would be **redundant** and is removed from the template. Result: exactly one
auth-enforcement mechanism, declared in the manifest, applied at the mount. The
generated handler body no longer carries an auth guard.

`middleware.RequireAuth` must return the envelope-shaped 401
(`{"ok":false,"error":"unauthorized","code":"unauthorized"}`) — which Build 1's
final-fix wave already ensured. No middleware change is needed here beyond
confirming that behavior.

---

## §7 Testing

**Builder suite (`src/builder`, host):**
- `manifest_test.go` — upsert adds a new record; upsert replaces a same-key
  record without duplicating; a conflicting `(method,path)`→different-handler
  errors and writes nothing; `hash` is stable across a no-op regeneration and
  changes when the set changes; a missing `api.json` is treated as the empty
  manifest.
- `render_test.go` (extended) — `routes_gen.go.tmpl` renders valid Go for: an
  empty endpoint set, a set with `auth:true` (asserts the `RequireAuth` wrap and
  the `middleware` import present), a set without auth (asserts `middleware` import
  absent — no unused import), and mixed `deps` including `[]` and `["write"]`.
  Deterministic ordering asserted (same set → same bytes).
- `handler.go.tmpl` render assertion updated — the inline auth check is gone.

**App suite (`src/app/handlers`, Docker):**
- `manifest_test.go` — `ManifestGET` serves a present `api.json` through the
  envelope; serves an empty manifest (not 404) when absent; the served `data`
  round-trips the models/endpoints.
- `version_test.go` (extended) — `manifest_hash` is present and equals
  `api.json`'s `hash`; empty string when `api.json` is absent.
- `routes_gen_test.go` — against the committed sample generated file (or a small
  fixture), `RegisterGenerated` mounts a route that responds, and an `auth:true`
  route returns the envelope 401 when unauthenticated. This is the integration
  proof that the derivation actually wires working routes.

**End-to-end (Task in plan, via the rebuilt MCP server over stdio, as in Build 1
Task 9):** `execute_sql` + `scaffold_list` → assert `api.json` gained the model
and endpoint, `routes_gen.go` mounts it, `main.go` was **not** touched, and
`curl /api/v1/_manifest` returns the resource. Then `create_handler` with a
custom path → assert it appears in the manifest and responds. Then a conflict
attempt → assert it errors and nothing changed. Revert the scratch scaffold.

**Reminder (Build 1 Task 9 finding):** the `mcp` container embeds templates via
`go:embed` at image-build time. The end-to-end task MUST `docker compose up -d
--build` before exercising the tools, or it runs the stale binary. This is now
documented in `CLAUDE.md`.

---

## §8 Documentation

- **`CLAUDE.md` (this repo):** the Golden Recipe and Custom/Escape-Hatch steps
  drop "wire the route in main.go" — routing is now automatic. Add a short "API
  Manifest" subsection: `api.json` is the source of truth, `routes_gen.go` is
  generated (never hand-edit), `create_handler`/`create_page` now take
  `method`+`path`, and per-endpoint auth is declared, not hand-checked. Note
  `_manifest` and the new `manifest_hash` on `_version`.
- **`../gova-ios/CLAUDE.md`:** note that a machine-readable manifest now exists
  at `GET /api/v1/_manifest` and will become the export source in Build 3 — but
  do **not** rewire `/export:mobile` here; that is Build 3's work. This is a
  forward-pointer only.

---

## §9 Scope boundary

Out of scope, deferred:
- **Rewriting `gova-ios`'s `/export:mobile`** to consume the manifest (Build 3).
- **Resource-scaffold / full CRUD** (detail, update, delete endpoints), and
  server-side sort/filter (Build 3).
- **An "unscaffold"/removal tool.** `api.json` is upsert-only; removing a
  resource means editing `api.json` and regenerating by hand, or regenerating
  from scratch. Not automated.
- **Migrating existing scaffolded apps.** Apps built before this change have
  hand-wired routes and no `api.json`. Regenerate them; no migration tooling.
- **Deriving `api_version` from the hash.** `api_version` stays a hand-set
  semver; only the informational `manifest_hash` is automatic.

---

## Files touched

**`gova-monolith`**

| File | Change |
|---|---|
| `src/app/api.json` | New — committed empty manifest as the initial state |
| `src/app/handlers/routes_gen.go` | New — committed empty `RegisterGenerated` |
| `src/app/handlers/manifest.go` | New — `ManifestGET` |
| `src/app/handlers/manifest_test.go` | New |
| `src/app/handlers/routes_gen_test.go` | New — `RegisterGenerated` integration |
| `src/app/handlers/version.go` | Add `manifest_hash` |
| `src/app/handlers/version_test.go` | Cover `manifest_hash` |
| `src/app/main.go` | Replace `@gova-routes` block with `_manifest` route + `RegisterGenerated` call |
| `src/builder/manifest.go` | New — read/upsert/hash `api.json`, regenerate `routes_gen.go` |
| `src/builder/manifest_test.go` | New |
| `src/builder/main.go` | Wire manifest upsert + routes regen into every route-producing tool; `create_handler`/`create_page` gain `method`+`path`; restructure `inspect_app` to JSON; drop route-instruction output strings |
| `src/builder/render_test.go` | `routes_gen.go.tmpl` assertions; updated `handler.go.tmpl` assertion |
| `src/builder/templates/routes_gen.go.tmpl` | New |
| `src/builder/templates/handler.go.tmpl` | Remove inline auth check (route wrap enforces it) |
| `CLAUDE.md` | Manifest + auto-routing docs; drop hand-wiring steps |

**`gova-ios`**

| File | Change |
|---|---|
| `CLAUDE.md` | Forward-pointer to `_manifest` (no `/export:mobile` change yet) |
