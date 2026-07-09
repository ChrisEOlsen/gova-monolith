# GOVA Monolith — Design Spec
**Date:** 2026-05-15
**Stack:** Go · Vanilla JS · SQLite

---

## Overview

A template repository for AI-driven web applications. Same philosophy as GOTHA (MCP tools render deterministic templates, AI calls tools instead of writing code) but with a pure JSON API backend and Vanilla ES module frontend instead of HTMX + Alpine.js + Templ.

**One container. One binary. One SQLite file.** No Redis, no MySQL, no Nginx, no build step for the frontend.

---

## Architecture

### Request Flow

```
Browser → GET /static/pages/projects.html  → Go serves static file
Browser → GET /static/js/projects.js       → Go serves static file
JS init → GET /api/projects                → Go JSON handler → JS renders DOM
JS form → POST /api/projects_create        → Go JSON handler → JS updates list
```

Go owns two concerns only:
1. Serve static files from `static/` (HTML, JS, CSS)
2. Handle `/api/*` routes returning JSON

No server-side HTML rendering. No templating engine. No compile step for views.

### Stack

| Layer | Technology |
|---|---|
| Language | Go 1.23 |
| Router | chi |
| Database | SQLite (WAL mode) |
| Sessions | Signed cookies (HMAC-SHA256) |
| Cache | In-process sync.Map |
| Frontend | Vanilla ES modules (no bundler) |
| CSS | Tailwind CLI standalone |
| Deployment | Cloudflare Tunnel |

### Comparison to GOTHA

| Layer | GOTHA | GOVA |
|---|---|---|
| Templates | Templ (compiled to Go) | None |
| Interactivity | HTMX + Alpine.js | Vanilla ES modules |
| API format | HTML fragments | JSON |
| Page serving | Templ handlers | Static `.html` files |
| Build steps | `run_templ` + `build_css` | `build_css` only |

---

## Go Backend Structure

```
src/app/
  main.go
  go.mod / go.sum
  .air.toml
  db/db.go          ← WAL mode, read/write connection split (identical to GOTHA)
  cache/cache.go    ← sync.Map, 5-min TTL (identical to GOTHA)
  middleware/
    auth.go         ← passive session read + RequireAuth (identical to GOTHA)
    csrf.go         ← validates X-CSRF-Token header (not form field)
    security.go     ← security headers (identical to GOTHA)
  handlers/
    json.go         ← shared jsonOK() and jsonError() helpers (generated once at init)
    home.go         ← serves static/pages/home.html
    ...generated    ← JSON handlers only, no HTML rendering
  models/
    .gitkeep        ← generated models land here
  static/
    pages/          ← HTML shells (served as raw files)
    js/
      lib/
        api.js      ← shared fetch wrapper (CSRF, error handling)
        auth.js     ← requireAuth(), redirectIfAuthed() helpers
      ...generated  ← one .js module per feature
    css/
      input.css     ← Tailwind source
      style.css     ← compiled output
```

### Handler Pattern

All handlers return JSON. No exception.

```go
func ProjectsGET(db *sql.DB, c *cache.Cache) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        items, err := models.ProjectGetAll(db, c)
        if err != nil {
            jsonError(w, "failed to load", 500)
            return
        }
        jsonOK(w, items)
    }
}
```

### JSON Envelope

```json
{ "data": [...], "ok": true }
{ "error": "message", "ok": false }
```

### Critical Handler Rules

- No raw SQL in handlers — use model methods only
- No HTML rendering in handlers — always `Content-Type: application/json`
- Handler signature: `func XxxGET(db *sql.DB, cache *cache.Cache) http.HandlerFunc`

---

## Frontend Structure

### HTML Shell Pattern

Every page is a static `.html` file. Go serves it directly with no rendering.

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Projects</title>
  <link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
  <nav>...</nav>
  <main id="app">Loading...</main>
  <script type="module" src="/static/js/projects.js"></script>
</body>
</html>
```

### api.js — Shared Fetch Wrapper

CSRF token is set as a readable cookie by Go on first request. `api.js` reads it once and attaches it as a header to every mutating request.

```js
const csrf = document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? '';

export async function get(path) {
  const res = await fetch(path);
  return res.json();
}

export async function post(path, body) {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
    body: JSON.stringify(body),
  });
  return res.json();
}

export async function del(path) {
  const res = await fetch(path, {
    method: 'DELETE',
    headers: { 'X-CSRF-Token': csrf },
  });
  return res.json();
}
```

### auth.js — Auth Helpers

```js
import { get } from './api.js';

export async function requireAuth() {
  const res = await get('/api/auth/me');
  if (!res.ok) window.location.href = '/static/pages/login.html';
  return res.data;
}

export async function redirectIfAuthed() {
  const res = await get('/api/auth/me');
  if (res.ok) window.location.href = '/static/pages/home.html';
}
```

Protected pages call `requireAuth()` at module init. Login page calls `redirectIfAuthed()`.

### JS Safety Rules — Non-Negotiable

```
NEVER:  element.innerHTML = userValue        ← XSS vector
ALWAYS: element.textContent = userValue       (for plain text)
ALWAYS: createElement + setAttribute         (for structured HTML)
NEVER:  eval() or new Function() with any external data
ALWAYS: import api.js for all fetch — never write raw fetch() calls
NEVER:  console.log() with sensitive data (tokens, passwords, session info)
```

All MCP-scaffolded JS templates enforce these rules. Never bypass them when editing generated files.

---

## CSRF Strategy

**Double-submit cookie pattern.**

1. Go sets a `csrf_token` cookie (readable, not HttpOnly) on `GET /api/auth/csrf`
2. `api.js` reads the cookie once at module load
3. Every POST/PUT/DELETE sends `X-CSRF-Token: <value>` header
4. Go middleware validates header matches cookie
5. Token rotates on login

**Why this is safe:**
- Session cookie is `HttpOnly` + `Secure` + `SameSite=Strict` — XSS cannot steal the session
- `SameSite=Strict` means cross-origin requests never carry the session cookie (primary CSRF defense)
- CSRF token is defense-in-depth
- Handlers only accept `Content-Type: application/json` — browsers cannot send that from cross-origin forms

---

## Auth Flow

### Endpoints

```
GET  /api/auth/csrf      → set csrf_token cookie, return {ok:true}
POST /api/auth/login     {email, password}         → {ok:true} + session cookie
POST /api/auth/logout    {}                         → {ok:true} + clear session
POST /api/auth/register  {name, email, password}   → {ok:true} + session cookie
GET  /api/auth/me        → {data:{id,name,email}, ok:true} or 401
```

### Security

- Passwords: bcrypt
- Sessions: HMAC-SHA256 signed cookie (`HttpOnly`, `Secure`, `SameSite=Strict`)
- Rate limiting: `rate_limits` SQLite table — 5 attempts / 15 min per IP
- Auth is optional — `middleware.Auth` is passive (reads cookie if present, never blocks)
- Protect individual handlers with `middleware.RequireAuth`
- Get current user: `middleware.UserID(r)`

---

## MCP Builder Tools

The `gova-builder` MCP server runs inside Docker and exposes these tools. **Always call a tool before editing the generated file — never write handlers or JS modules from scratch.**

| Tool | When to use |
|---|---|
| `inspect_app` | **Before scaffolding** — shows existing models, handlers, JS pages, routes |
| `execute_sql` | Create/alter tables — always before `create_model` |
| `create_model` | Data layer — table must exist first |
| `create_handler` | Single custom JSON endpoint stub |
| `create_page` | Full page: `.html` shell + `.js` module + Go handler stub |
| `scaffold_list` | Non-personalized list: model + JSON GET handler + `.html` + `.js` |
| `scaffold_auth` | Auth system: users table, User model, login/logout/me endpoints |
| `scaffold_registration` | Registration endpoint — run after `scaffold_auth` |
| `add_js_form` | Inject form + fetch logic into an existing `.js` module |
| `build_css` | Compile Tailwind: `input.css` → `style.css` |
| `run_linter` | `go vet` + SQL injection + XSS pattern checks |

### Scaffolding Sequence

**Standard list feature:**
```
execute_sql → create_model → scaffold_list → add_js_form → build_css → wire main.go
```

**Custom page:**
```
execute_sql → create_model → create_page → create_handler (POST/DELETE) → add_js_form → build_css → wire main.go
```

**Auth (one time):**
```
scaffold_auth → scaffold_registration → build_css → wire main.go
```

---

## Harness Layer

### Files

| File | Status |
|---|---|
| `SEED.md` | Identical to GOTHA |
| `CHECKLIST.md` | Updated (remove run_templ, rename gova-builder) |
| `README.md` | Updated stack table |
| `CLAUDE.md` / `GEMINI.md` / `AGENTS.md` | Rewritten for GOVA patterns |
| `.claude/commands/build.md` | Updated (no run_templ, add_js_form, stronger scaffold rule) |
| `.claude/commands/launch.md` | Identical to GOTHA |
| `.claude/commands/security/analyze.md` | Expanded with JS audit pass |
| `install-claude.sh` | Updated container/server name |
| `install-gemini.sh` | Updated container/server name |
| `install-opencode.sh` | Updated container/server name |

### Mandatory Scaffolding Rule (in build command + all context files)

```
You MUST call the appropriate MCP tool BEFORE writing any Go handler or JS module.
This is not optional. This is not a suggestion.

The sequence is always:
  MCP tool → generated file → customize generated file

NEVER:
  ✗ Write a handler from scratch, then call MCP tools
  ✗ Skip scaffold_list because "it's simpler to just write it"
  ✗ Create a .js file manually without calling create_page or scaffold_list first

If you are about to create a file in handlers/ or static/js/ without having
just called an MCP tool — STOP. Call the tool first.

Subagents must confirm at the start of each task:
  "Which MCP tool scaffolds this?" → call it → then customize.
```

### Security Audit — JS Pass (addition to existing Go audit)

| Threat | Pattern | Severity |
|---|---|---|
| XSS | `innerHTML` assigned any variable | Critical |
| Code injection | `eval()` / `new Function()` with external data | Critical |
| Missing CSRF | `fetch(POST/DELETE)` without X-CSRF-Token header | High |
| Auth bypass | Protected page JS missing `requireAuth()` call | High |
| Data exposure | `console.log()` with tokens/passwords/session data | Medium |

Go-side Templ XSS checks from GOTHA are removed — `encoding/json` auto-escapes all output, making them irrelevant.

---

## Infrastructure

| Layer | Detail |
|---|---|
| Web server | Go `net/http` via chi — no Nginx |
| Hot reload | `air` watches `src/app/` |
| SQLite | WAL mode at `/data/app.db` (Docker volume) |
| Sessions | Signed cookie (`gova_session`) — no DB hit per request |
| Cache | In-process `sync.Map` — lost on restart, that's fine |
| CSRF | Readable cookie + header validation |
| Deployment | Cloudflare Tunnel (`cloudflared`) |
