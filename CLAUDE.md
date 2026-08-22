> **Automated Workflow:** This project uses `/build` to build from `SEED.md` and `/launch` to deploy. Run `/build` to start.

# Claude Code Context: GOVA Monolith

You are the **Lead Architect** of a GOVA Monolith. Your goal is to build robust, secure web applications using the provided MCP "Factory" tools.

## Mandatory Scaffolding Rule

**For feature handlers and JS pages, call the MCP tool FIRST — before writing any code.**

The sequence is always:
**MCP tool → generated file → customize generated file**

NEVER (for feature files):
- Write a feature handler from scratch, then call MCP tools
- Skip `scaffold_list` because "it's simpler to just write it"
- Create a feature `.js` module without calling `create_page` or `scaffold_list` first

**Exception — infrastructure files are written manually** (created once at init, not per-feature):
- `middleware/*.go` — app-wide plumbing
- `db/`, `cache/` — core infrastructure
- `handlers/json.go` — shared JSON helpers
- `static/js/lib/*.js` — shared libs (api.js, auth.js)
- Shared utility JS modules imported by other modules (e.g. `static/js/utils.js`)

Subagents must confirm at the start of each task:
> "Which MCP tool scaffolds this?" → call it → then customize.
> If it's infrastructure, document why no scaffold tool applies.

---

## No Git Worktrees for Builds

**Never use `superpowers:using-git-worktrees` (or any worktree) for gova-monolith build work.** Work directly on a feature branch in the main checkout instead (`git checkout -b build/<app-name>`).

Why: the `gova-builder` MCP server and the SQLite db are singleton, path-bound infrastructure — the `mcp` container's bind mounts (`./src:/src`, `./data:/data` in `docker-compose.yml`) point at one absolute path, set once at `docker compose up`. A worktree lives at a different path, so MCP tool calls issued from inside it would write to the wrong checkout unless the container's mounts are retargeted — and retargeting kills the running `docker exec` stdio session, forcing a disruptive manual `/mcp` reconnect mid-build. Two worktrees can't both point the one container at themselves either, so worktree-level parallelism for MCP scaffold work was never actually achievable here.

Branch isolation (keeping the build off `main` until reviewed) is still worth having — get it via a plain feature branch, not a worktree.

---

## The Golden Recipe

### 1. Database First
- Think: What data do I need?
- Action: Use `execute_sql` to create the table.
- Rule: ALWAYS use `id INTEGER PRIMARY KEY` (no AUTOINCREMENT).
- Example:
  ```sql
  CREATE TABLE projects (
      id INTEGER PRIMARY KEY,
      name TEXT NOT NULL,
      status TEXT DEFAULT 'active',
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP
  );
  ```

### 2. Scaffold the Backbone
- **Option A (Standard List, read-only):** `scaffold_list(name='project', fields=['name:string', 'status:string'])`
  — registers `GET` only. If the page needs to create rows, use Option B instead.
- **Option B (Full CRUD):** `scaffold_resource(name='project', fields=[...])` — list/detail/create/update/delete, all self-registered.
- **Option C (Custom):** `create_model(name='project', ...)` + `create_page(filename='projects', path='/projects', ...)` + `create_handler(...)` for its JSON endpoints
- **Option D (Auth — optional):** `scaffold_auth()` → `scaffold_registration()`

> **Auth is optional.** Skip Option D for public sites. `middleware.Auth` is passive — it reads a session cookie if present but never blocks on its own. Protect specific API endpoints with `middleware.RequireAuth`. Protect pages by setting `auth: true` on the page, which wraps its route in
`middleware.RequirePageAuth` (a 303 to `/login`); the generated JS module also calls
`requireAuth()`. The two are complementary — see **API Manifest & Routing** for why the
page guard is a courtesy and the endpoint guard is the real boundary.

### 3. Add Forms
- Use `add_js_form(page='projects', api_endpoint='/api/v1/projects', ...)` to inject creation forms.
- **A creation form needs a POST route to post to.** `scaffold_resource`
  (Option B) registers one; **`scaffold_list` does not** — it is read-only and
  registers `GET` alone. Adding a form to a `scaffold_list` page produces a
  405 (`method_not_allowed`) on submit. Either scaffold the resource instead,
  or add the POST yourself with `create_handler`.
- Routes are registered automatically — scaffolds and `create_handler` update api.json + routes_gen.go; `create_page` and the page-emitting scaffolds update api.json + pages_gen.go.
- Edit `.js` files to add custom behavior.
- Edit `.html` files to adjust layout and structure.
- Keep Go handler logic in `handlers/`. HTML in `static/pages/`. JS in `static/js/`.

### 4. CSS Compiles Automatically
- `entrypoint.sh` recompiles Tailwind CSS on every `docker compose restart app` — no MCP step needed.
- If you changed JS/HTML classes without any Go handler change, restart once to see them: `docker compose restart app`.

---

## Testing

Scaffold tools generate tests alongside code — see the Tool Cheat Sheet above for which ones. Nothing extra to do for that code beyond letting the scaffold call run.

- **Generated page routes test themselves.** `pages_gen_test.go` is regenerated
  with `pages_gen.go` and asserts every registered page actually serves its
  shell through a real chi router. Do not hand-edit it; if it fails, a page is
  registered but unreachable.
- **Hand-customized logic gets its own test.** If a task customizes a scaffolded handler beyond its generated behavior, or implements a bespoke `create_handler`/`create_page` stub, write a test for it — same `_test.go` convention (`httptest` against the handler, `db.OpenTest` for anything touching the db). See `gova-writing-plans` Step 3b.
- **Verify:** `docker compose exec app go test ./...` — required alongside `docker compose logs app`, not instead of it.
- **No JS testing.** Blocked by Critical Constraint 4 (no Node/npm — every standard JS test runner needs Node). Client-side code stays manually/browser-verified.
- **Test db:** `db.OpenTest(t, schema)` opens a temp-file SQLite db (`t.TempDir()`), never `/data/app.db`.

---

## Critical Constraints

1. **No Raw SQL in handlers.** Use model methods only.
   - Correct: `model.GetPage(limit, offset)`
   - Wrong: `db.Query("SELECT * FROM projects")`

2. **No HTML rendering in Go handlers.** All handlers return JSON.
   - Correct: `jsonOK(w, items)`
   - Wrong: `fmt.Fprintf(w, "<li>%s</li>", name)`
   - Serving a static shell is not rendering: `pages_gen.go` hands an inert
     `.html` file to `http.ServeFile` and interpolates nothing. Page HTML is
     never built in Go.

3. **JS Safety — Non-Negotiable:**
   - `NEVER`: `element.innerHTML = userValue` ← XSS vector
   - `ALWAYS`: `element.textContent = userValue` (for plain text)
   - `ALWAYS`: `createElement` + `setAttribute` (for structured HTML)
   - `NEVER`: `eval()` or `new Function()` with any external data
   - `ALWAYS`: Use `api.js` for all fetch — never write raw `fetch()` calls
   - `NEVER`: `console.log()` with tokens, passwords, or session data

4. **No Node.js / NPM.** Tailwind CLI standalone only. `api.js` and `auth.js` are in `static/js/lib/` — do not add CDN script tags.

5. **Security Built-in:**
   - **CSRF:** Double-submit cookie. `api.js` reads `csrf_token` cookie and sends `X-CSRF-Token` header automatically.
   - **Sessions:** Signed HMAC-SHA256 cookie. `middleware.SetSession(w, userID, 24*time.Hour)` on login. `middleware.ClearSession(w)` on logout.
   - **Auth (API):** `jsonError(w, "unauthorized", 401)` for unauthenticated requests — never redirect from an API handler.
   - **Auth (Pages):** Call `requireAuth()` at the top of protected JS modules.
   - **Rate Limiting:** Login uses `rate_limits` table (5 attempts / 15 min per IP).

---

## API Wire Contract

Every JSON response uses one envelope:

```json
{ "ok": true, "data": [ ... ], "meta": { "limit": 50, "offset": 0, "total": 123 } }
{ "ok": false, "error": "Name is required", "code": "validation_failed", "fields": { "name": "required" } }
```

- **`data` is never `null` for a list.** Models initialize slices non-nil and
  `jsonOK`/`jsonList` normalize as a second guard. A typed client decoding an
  array must never see `null`.
- **`error` is always a plain string.** `code` and `fields` are additive.
- **Codes:** `unauthorized`, `forbidden`, `not_found`, `method_not_allowed`,
  `conflict`, `validation_failed`, `rate_limited`, `unavailable`, `internal`.
  - `codeForStatus` derives these from the status. Its default is split by
    class: **any unenumerated 4xx is `validation_failed`**, because a 4xx is by
    definition something about the request, and only 5xx falls through to
    `internal`. A single `default: internal` told every caller that their own
    malformed body was this server's fault — and `jsonError(w, "…", 400)` is the
    shortest helper, so that was the common case.
- **Timestamps** are RFC3339, UTC, second precision — via `models.Time`. Never
  use a bare `time.Time` in a model struct.
  - **Declare a DATETIME column as `timestamp`, never as `string`.**
    `updated_at:string` generates a Go `string` holding SQLite's native
    `2026-08-15 19:40:07`, which puts **two timestamp formats on one JSON
    object** beside `created_at`'s RFC3339. A browser parses both; a typed
    client's `.iso8601` decoder rejects the row. A nullable timestamp becomes
    `*models.Time`, scanned through `models.NullTime`.
  - Field types are `string`, `int`, `float`, `boolean`, `password`,
    `timestamp`. An unrecognised type is now an error rather than a silent
    `string`.
- **Unmatched routes answer in the envelope too.** `main.go` installs
  `handlers.NotFoundHandler()` and `handlers.MethodNotAllowedHandler()` as chi's
  fallbacks. Without them chi replies in plain text, so the two failures a client
  is most likely to hit — a mistyped path and a wrong verb — were the two that
  broke the contract, and `not_found`/`method_not_allowed` were unreachable
  through routing at all. The fallbacks are scoped to `/api/`: a browser that
  mistypes a *page* URL still gets the ordinary 404 it knows how to render,
  the same split `RequireAuth` and `RequirePageAuth` make.
- **Lists are paginated by default:** `?limit=` (1–200, default 50) and
  `?offset=`. Use `jsonList(w, items, Meta{...})`, not `jsonOK`.
- **All API routes live under `/api/v1/`.**
- `GET /api/v1/_version` reports `api_version` and `min_client_version`.

Helpers in `handlers/json.go`: `jsonOK`, `jsonList`, `jsonError`,
`jsonErrorCode`, `jsonValidationError`.

---

## API Manifest & Routing

`src/app/api.json` is the machine-readable source of truth for the served
surface — every model (with field types and nullability), every endpoint
(method, path, handler, auth, kind), and every page (path, file, title). It is
committed source, not a build artifact.

- **Models are registered by whatever creates them.** Every scaffold **and
  `create_model`** upsert into `models`. `create_model` registers no route, so
  it touches `models` alone — but it does register, and a model missing from the
  manifest while endpoints reference it is a hole nothing goes looking for.
- **Routes are automatic.** Scaffold tools and `create_handler` upsert into
  `endpoints` and regenerate `src/app/handlers/routes_gen.go`. `main.go` mounts
  them with one `handlers.RegisterGenerated(...)` call. **Never hand-wire a route
  in main.go, and never edit `routes_gen.go` (it is generated).**
- **Pages are automatic too, and live in their own table.** `create_page` and
  every scaffold that emits an `.html` shell upsert into `pages` and regenerate
  `src/app/handlers/pages_gen.go` (plus its companion `pages_gen_test.go`).
  `main.go` mounts them with one `handlers.RegisterPages(r)` call.
- **`pages` is separate from `endpoints` on purpose.** A page has no method
  beyond GET, no request or response body and no deps, so it is not part of the
  API surface a native client consumes. Keeping the two apart also keeps the
  namespaces disjoint: `create_handler` requires `/api/v1/`, `create_page`
  refuses `/api/`, so neither can shadow the other. Resource pages are always
  plural (`/projects`) and the auth pages take their singular verb (`/login`,
  `/register`), so a resource named `login` lands at `/logins` and cannot
  collide.
- **Pages are served by file path, never by request input.** `pages_gen.go`'s
  `pageFile` helper takes a literal base name from the generated table and
  applies `filepath.Base` as a second guard, so nothing a caller sends can reach
  the filesystem.
- **A page's `auth: true` wraps its route in `middleware.RequirePageAuth`** — a
  303 to `/login`, not the JSON `RequireAuth`, because answering a browser
  navigation with a JSON 401 body is worse than not guarding it.
  - **It is a courtesy, not a boundary.** The shell is inert HTML and every
    datum on it comes from an `/api/v1/` endpoint, so those are where `auth:
    true` protects anything. What the page wrap buys is removing the flash: a
    signed-out visitor is bounced by the server on the cookie alone, rather than
    rendering the whole page and waiting for its JS module's `requireAuth()`.
  - This flag used to render **nothing at all** — written into `api.json`, read
    by no generator, and reading exactly like a security control. The generated
    `pages_gen_test.go` now asserts, per page, that a guarded path redirects
    when signed out.
- **Per-endpoint auth is declarative.** An endpoint's `auth: true` makes
  `routes_gen.go` wrap it in `middleware.RequireAuth`. Handlers do not check auth
  inline.
- **Served at `GET /api/v1/_manifest`.** `GET /api/v1/_version` also reports a
  `manifest_hash` so a client or CI can detect any surface change — pages are in
  the hash, so adding or moving one is visible there too.
- **`inspect_app` returns JSON** — `{manifest, on_disk, divergence}` — and flags
  files that drifted from the manifest.
- **No removal tool.** `api.json` is upsert-only; to remove a resource, edit
  `api.json` and re-run a scaffold, or regenerate.

### Resource list querying (scaffold_resource)

A `scaffold_resource` list endpoint accepts, beyond `?limit=`/`?offset=`:
- `?sort=<col>` (ascending) or `?sort=-<col>` (descending)
- `?filter=<col>:<value>` — equality on a column

`<col>` is whitelisted against the model's real columns (`id`, its fields,
`created_at`); an unknown column returns **422** (`validation_failed`). Filter
values are always bound parameters. The whitelist/validation lives in the shared,
hand-written `models/query.go`. Create/update validation is coarse (malformed body
→ 422, model/DB error → 500); per-field 422 is a deferred enhancement.

---

## Infrastructure

| Layer | Detail |
|---|---|
| **Web server** | Go `net/http` via chi in `src/app/main.go`. No Nginx. |
| **Go app** | Rebuilt by restarting the container (`docker compose restart app`). |
| **SQLite** | WAL mode at `/data/app.db` (Docker volume). |
| **Sessions** | Signed cookie (`gova_session`). No database hit per request. |
| **Cache** | In-process cache in `cache/cache.go`. Lost on restart — that's fine. |

> **mcp image rebuilds:** the `mcp` container embeds `src/builder/templates` via `//go:embed` at IMAGE BUILD time, not at container start. After editing anything under `src/builder/` (templates or generator code), a plain `docker compose restart` reruns the stale binary and silently generates old-shape code from the running MCP tools. Rebuild the image instead: `docker compose up -d --build`.

---

## Tool Cheat Sheet

| Tool | When to use | Generates tests? |
|---|---|---|
| `inspect_app` | **Before scaffolding** — existing models, handlers, JS pages, routes | — |
| `execute_sql` | Create tables — always before `create_model` | — |
| `create_model` | Data layer; table must exist first. **Registers the model in `api.json`'s `models` array** — it creates no route, so `endpoints` and `pages` are untouched. Validates `fields` against the real table via `PRAGMA table_info`; a mismatch fails the call. Nullable columns become Go pointers. | Yes — CRUD roundtrip |
| `create_handler` | Single custom JSON endpoint stub. Takes `method` + `path`; self-registers the route into `api.json` and `routes_gen.go` — no manual wiring in `main.go`. | No — implement the TODO, then write its test yourself (`gova-writing-plans` Step 3b) |
| `create_page` | A page: `.html` shell + `.js` module. Takes a **human-facing** `path` (`/dashboard`, `/settings`) and **rejects anything under `/api/`** — that namespace is `create_handler`'s. Registers a row in `api.json`'s `pages` array and regenerates `pages_gen.go`. **No Go handler is created or needed** — the generated `pageFile` helper serves the shell. Use `create_handler` for the JSON endpoints the page's JS calls. | Yes — `pages_gen_test.go` asserts every registered page serves its shell |
| `scaffold_list` | Non-personalized list: model + JSON handler + `.html` + `.js`. Registers `GET /api/v1/<plural>` **and serves the page at `/<plural>`**. Validates `fields` against the real table via `PRAGMA table_info`; a mismatch fails the call. Nullable columns become Go pointers. — read-only; use `scaffold_resource` for full CRUD | Yes — CRUD + list-handler + page-serving tests |
| `scaffold_resource` | Full CRUD: model + list/detail/create/update/delete handlers + list page, all self-registered. The list page is served at `/<plural>`. List supports `?sort=`/`?filter=` (whitelisted). Table must exist first. Public by default. | Yes — model CRUD + resource handler + page-serving tests |
| `scaffold_auth` | Full auth — cookie **and** bearer (web + mobile) in one run: users + rate_limits + mobile_tokens tables, `User` **and `MobileToken`** models, login/logout/me + login_token/logout_token/me_token handlers, all 6 routes self-registered, **and the login page served at `/login`**. Run scaffold_registration after for a registration endpoint. | Yes — login, rate-limit, CSRF, bearer-token expiry/revocation, rate-limit decay tests |
| `scaffold_registration` | Registration endpoint + page — run after `scaffold_auth`. Registers `POST /api/v1/auth/register` and serves the page at `/register`. | Yes — registration, duplicate-email tests |
| `add_js_form` | Inject creation form into existing `.js` module | No — JS isn't tested (see Testing below) |

---

## Custom / Escape Hatch Pattern

When `scaffold_list` doesn't fit (filtered views, detail pages, dashboards):

```
1. execute_sql       → create the table
2. create_model      → generate the model
3. create_page       → html shell + js module, served at a human-facing URL
4. create_handler    → GET/POST/DELETE JSON handler stubs under /api/v1/
5. edit handlers/    → implement TODO logic using model methods
6. edit static/js/   → fetch data, render DOM (never innerHTML for user data)
7. add_js_form       → inject form at // @inject-forms marker
8. docker compose restart app → recompiles CSS, rebuilds the Go binary
```

Steps 3 and 4 register themselves and are the two halves of one page:

- **`create_page` owns the URL a person visits.** Its `path` is human-facing
  (`/dashboard`) and must **not** be under `/api/` — the call is refused if it
  is. It writes a row into `api.json`'s `pages` array and regenerates
  `pages_gen.go`. It creates **no Go handler**: the generated `pageFile` helper
  serves the shell, so there is no TODO to implement and nothing to test by
  hand.
- **`create_handler` owns the data the page fetches.** Its `path` must start
  with `/api/v1/`. It writes into `endpoints` and regenerates `routes_gen.go`,
  and its stub is where your logic goes.

Never hand-wire a route in `main.go`, and never edit `routes_gen.go`,
`pages_gen.go` or `pages_gen_test.go` (all three are generated).

---

## Frontend Patterns

**JS module structure:**
```js
import { get, post, put, del } from '/static/js/lib/api.js';
import { requireAuth } from '/static/js/lib/auth.js'; // protected pages only

const listEl = document.getElementById('item-list');

export async function loadList() {
  const res = await get('/api/v1/items');
  if (!res.ok) { listEl.textContent = 'Failed to load.'; return; }
  renderList(res.data ?? []);
}

function renderList(items) {
  listEl.replaceChildren();
  items.forEach(item => {
    const li = document.createElement('li');
    li.textContent = item.name;    // safe: textContent not innerHTML
    listEl.appendChild(li);
  });
}

// @inject-forms

async function init() {
  await loadList();
}
init();
```

**Error display:**
```js
const errEl = document.createElement('p');
errEl.className = 'text-sm text-red-600';
errEl.textContent = res.error ?? 'Something went wrong.'; // textContent — safe
```
