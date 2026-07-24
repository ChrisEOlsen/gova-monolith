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
- **Option A (Standard List):** `scaffold_list(name='project', fields=['name:string', 'status:string'])`
- **Option B (Custom):** `create_model(name='project', ...)` + `create_page(filename='projects', ...)`
- **Option C (Auth — optional):** `scaffold_auth()` → `scaffold_registration()`

> **Auth is optional.** Skip Option C for public sites. `middleware.Auth` is passive — it reads a session cookie if present but never blocks on its own. Protect specific API endpoints with `middleware.RequireAuth`. Protect pages client-side by calling `requireAuth()` at the top of the JS module.

### 3. Add Forms
- Use `add_js_form(page='projects', api_endpoint='/api/v1/projects', ...)` to inject creation forms.
- Routes are registered automatically — scaffold and create_handler/create_page update api.json and routes_gen.go.
- Edit `.js` files to add custom behavior.
- Edit `.html` files to adjust layout and structure.
- Keep Go handler logic in `handlers/`. HTML in `static/pages/`. JS in `static/js/`.

### 4. CSS Compiles Automatically
- `entrypoint.sh` recompiles Tailwind CSS on every `docker compose restart app` — no MCP step needed.
- If you changed JS/HTML classes without any Go handler change, restart once to see them: `docker compose restart app`.

---

## Testing

Scaffold tools generate tests alongside code — see the Tool Cheat Sheet above for which ones. Nothing extra to do for that code beyond letting the scaffold call run.

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
- **Codes:** `unauthorized`, `forbidden`, `not_found`, `conflict`,
  `validation_failed`, `rate_limited`, `internal`.
- **Timestamps** are RFC3339, UTC, second precision — via `models.Time`. Never
  use a bare `time.Time` in a model struct.
- **Lists are paginated by default:** `?limit=` (1–200, default 50) and
  `?offset=`. Use `jsonList(w, items, Meta{...})`, not `jsonOK`.
- **All API routes live under `/api/v1/`.**
- `GET /api/v1/_version` reports `api_version` and `min_client_version`.

Helpers in `handlers/json.go`: `jsonOK`, `jsonList`, `jsonError`,
`jsonErrorCode`, `jsonValidationError`.

---

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
| `create_model` | Data layer; table must exist first. Validates `fields` against the real table via `PRAGMA table_info`; a mismatch fails the call. Nullable columns become Go pointers. | Yes — CRUD roundtrip |
| `create_handler` | Single custom JSON endpoint stub. Takes `method` + `path`; self-registers the route into `api.json` and `routes_gen.go` — no manual wiring in `main.go`. | No — implement the TODO, then write its test yourself (`gova-writing-plans` Step 3b) |
| `create_page` | Full page: `.html` shell + `.js` module + Go handler stub. Takes `path` (method is always `GET`); self-registers the route into `api.json` and `routes_gen.go` — no manual wiring in `main.go`. | No — same as `create_handler` |
| `scaffold_list` | Non-personalized list: model + JSON handler + `.html` + `.js`. Validates `fields` against the real table via `PRAGMA table_info`; a mismatch fails the call. Nullable columns become Go pointers. — read-only; use `scaffold_resource` for full CRUD | Yes — CRUD + list-handler tests |
| `scaffold_resource` | Full CRUD: model + list/detail/create/update/delete handlers + list page, all self-registered. List supports `?sort=`/`?filter=` (whitelisted). Table must exist first. Public by default. | Yes — model CRUD + resource handler tests |
| `scaffold_auth` | Full auth — cookie **and** bearer (web + mobile) in one run: users + rate_limits + mobile_tokens tables, login/logout/me + login_token/logout_token/me_token handlers, all 6 routes self-registered. Run scaffold_registration after for a registration endpoint. | Yes — login, rate-limit, CSRF tests |
| `scaffold_registration` | Registration endpoint — run after `scaffold_auth` | Yes — registration, duplicate-email tests |
| `add_js_form` | Inject creation form into existing `.js` module | No — JS isn't tested (see Testing below) |

---

## Custom / Escape Hatch Pattern

When `scaffold_list` doesn't fit (filtered views, detail pages, dashboards):

```
1. execute_sql       → create the table
2. create_model      → generate the model
3. create_page       → html shell + js module + handler stub
4. create_handler    → POST/DELETE handler stubs as needed
5. edit handlers/    → implement TODO logic using model methods
6. edit static/js/   → fetch data, render DOM (never innerHTML for user data)
7. add_js_form       → inject form at // @inject-forms marker
8. docker compose restart app → recompiles CSS, rebuilds the Go binary
```

Steps 3 and 4 register their own routes — `create_page` and `create_handler` update
`api.json` and regenerate `routes_gen.go`. Never hand-wire a route in `main.go`.

---

## Frontend Patterns

**JS module structure:**
```js
import { get, post, del } from '/static/js/lib/api.js';
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
