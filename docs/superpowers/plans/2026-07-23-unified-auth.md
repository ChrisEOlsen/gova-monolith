# Unified Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `scaffold_auth` emit both cookie (web) and bearer (mobile) auth in one run — all tables, all handlers, all six endpoints — and remove the now-redundant `scaffold_mobile_auth` tool, so "set up auth" is a single command.

**Architecture:** `handleScaffoldAuth` gains the `mobile_tokens` DDL, renders the two (fully static) mobile-auth templates alongside the cookie files, and registers all six auth endpoints via a testable `authEndpoints()` helper. `scaffold_mobile_auth`'s tool registration and handler are deleted; its templates stay. A gova-ios documentation sweep replaces `scaffold_mobile_auth` references with `scaffold_auth`.

**Tech Stack:** Go 1.25 (`net/http`, `chi`, `database/sql`, `mattn/go-sqlite3`), `text/template` codegen, `mark3labs/mcp-go`, Docker Compose. Builder tested on host; app tested in Docker.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-07-23-unified-auth-design.md` — authoritative.
- **Branch:** `build/unified-auth`. Never a git worktree (MCP container bind-mounts are path-bound).
- **Six endpoints from one `scaffold_auth` run:** `POST /api/v1/auth/login` (`LoginPOST`, rwc, kind `auth_login`), `POST /api/v1/auth/logout` (`LogoutPOST`, `[]`, `auth_logout`), `GET /api/v1/auth/me` (`MeGET`, rwc, **auth:true**, `auth_me`), `POST /api/v1/auth/login_token` (`MobileLoginPOST`, rwc, `mobile_login`), `DELETE /api/v1/auth/logout_token` (`MobileLogoutDELETE`, `["write"]`, `mobile_logout`), `GET /api/v1/auth/me_token` (`MobileMeGET`, rwc, `mobile_me`).
- **The three token endpoints are `auth:false`** (self-enforcing bearer — Build 2's Critical fix). Only cookie `me` is `auth:true`. The `user` model is unchanged (`id`, `name`, `email`, `created_at`; **no** `password`).
- **`scaffold_mobile_auth` is removed** (tool registration + `handleScaffoldMobileAuth`). **Keep** `mobile_auth_handler.go.tmpl` and `mobile_auth_test.go.tmpl` — both are fully static (zero template fields), so they render identically to any data.
- **DDL order matters:** `users` before `mobile_tokens` (FK `mobile_tokens.user_id → users(id)`), in one exec. Idempotent (`CREATE TABLE IF NOT EXISTS`).
- **No handler logic changes** — `auth.go` and `mobile_auth.go` already coexist. This is a tool merge.
- No new third-party deps. No Node/npm.
- Two suites: `cd src/builder && go test ./...` (builder, host) and `docker compose exec app go test ./...` (app, Docker). **Builder/template changes need `docker compose up -d --build`** before the MCP tools reflect them (Task 3 e2e only).

---

## File Structure

**Modified files:**

| File | Change |
|---|---|
| `src/builder/main.go` | `authEndpoints()` helper; `handleScaffoldAuth` adds `mobile_tokens` DDL + renders the two mobile files + registers all 6 endpoints; **delete** `scaffold_mobile_auth` tool registration + `handleScaffoldMobileAuth` |
| `src/builder/manifest_test.go` | `TestAuthEndpoints_SixWithKinds` |
| `CLAUDE.md` | Cheat sheet + Golden Recipe: `scaffold_auth` covers mobile; drop `scaffold_mobile_auth` |
| `../gova-ios/.claude/commands/build.md` | Remove the `scaffold_mobile_auth` plan step |
| `../gova-ios/.claude/commands/prep.md` | `scaffold_mobile_auth` → `scaffold_auth` |
| `../gova-ios/.claude/commands/export-mobile.md` | `scaffold_mobile_auth` → `scaffold_auth` |
| `../gova-ios/CLAUDE.md` | Setup note, translation Step 2, MCP-tools cheat sheet → `scaffold_auth` |

No new templates; the two mobile templates are retained unchanged.

---

## Task 1: Unify `scaffold_auth`, remove `scaffold_mobile_auth`

**Files:**
- Modify: `src/builder/main.go`
- Test: `src/builder/manifest_test.go`

**Interfaces:**
- Consumes: `Endpoint`/`Model` types, `updateManifest`, `renderToFile`, `newData` (Build 1-2).
- Produces: `authEndpoints() []Endpoint` — the six endpoints, pure/testable (mirrors `resourceEndpoints` from Build 3b).

- [ ] **Step 1: Write the failing test**

Add to `src/builder/manifest_test.go`:

```go
func TestAuthEndpoints_SixWithKinds(t *testing.T) {
	eps := authEndpoints()
	if len(eps) != 6 {
		t.Fatalf("got %d endpoints, want 6", len(eps))
	}
	type want struct {
		handler string
		kind    string
		auth    bool
		deps    int
	}
	expect := map[string]want{
		"POST /api/v1/auth/login":          {"LoginPOST", "auth_login", false, 3},
		"POST /api/v1/auth/logout":         {"LogoutPOST", "auth_logout", false, 0},
		"GET /api/v1/auth/me":              {"MeGET", "auth_me", true, 3},
		"POST /api/v1/auth/login_token":    {"MobileLoginPOST", "mobile_login", false, 3},
		"DELETE /api/v1/auth/logout_token": {"MobileLogoutDELETE", "mobile_logout", false, 1},
		"GET /api/v1/auth/me_token":        {"MobileMeGET", "mobile_me", false, 3},
	}
	for _, e := range eps {
		key := e.Method + " " + e.Path
		w, ok := expect[key]
		if !ok {
			t.Errorf("unexpected endpoint %s", key)
			continue
		}
		if e.Handler != w.handler {
			t.Errorf("%s: handler %q want %q", key, e.Handler, w.handler)
		}
		if e.Kind != w.kind {
			t.Errorf("%s: kind %q want %q", key, e.Kind, w.kind)
		}
		if e.Auth != w.auth {
			t.Errorf("%s: auth %v want %v", key, e.Auth, w.auth)
		}
		if len(e.Deps) != w.deps {
			t.Errorf("%s: deps len %d want %d", key, len(e.Deps), w.deps)
		}
		if e.Model != "" {
			t.Errorf("%s: auth endpoints carry no model, got %q", key, e.Model)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd src/builder && go test ./... -run TestAuthEndpoints -v`
Expected: FAIL — `undefined: authEndpoints`.

- [ ] **Step 3: Add the `authEndpoints()` helper**

In `src/builder/main.go` (near `resourceEndpoints`):

```go
// authEndpoints returns the six endpoints scaffold_auth registers — the cookie
// set (login/logout/me) and the bearer set (login_token/logout_token/me_token).
// The three token endpoints are auth:false: they self-enforce the bearer token
// in the handler; a session-cookie RequireAuth wrap would 401 them.
func authEndpoints() []Endpoint {
	rwc := []string{"read", "write", "cache"}
	return []Endpoint{
		{Method: "POST", Path: "/api/v1/auth/login", Handler: "LoginPOST", Deps: rwc, Kind: "auth_login"},
		{Method: "POST", Path: "/api/v1/auth/logout", Handler: "LogoutPOST", Deps: []string{}, Kind: "auth_logout"},
		{Method: "GET", Path: "/api/v1/auth/me", Handler: "MeGET", Deps: rwc, Auth: true, Kind: "auth_me"},
		{Method: "POST", Path: "/api/v1/auth/login_token", Handler: "MobileLoginPOST", Deps: rwc, Kind: "mobile_login"},
		{Method: "DELETE", Path: "/api/v1/auth/logout_token", Handler: "MobileLogoutDELETE", Deps: []string{"write"}, Kind: "mobile_logout"},
		{Method: "GET", Path: "/api/v1/auth/me_token", Handler: "MobileMeGET", Deps: rwc, Kind: "mobile_me"},
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd src/builder && go test ./... -run TestAuthEndpoints -v`
Expected: PASS.

- [ ] **Step 5: Add `mobile_tokens` to the `scaffold_auth` DDL**

In `handleScaffoldAuth`, extend the `ddl` string — append the `mobile_tokens` table after `rate_limits` (order matters: `users` first for the FK):

```go
	ddl := `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS rate_limits (
	ip TEXT NOT NULL,
	attempts INTEGER DEFAULT 0,
	locked_until DATETIME,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (ip)
);
CREATE TABLE IF NOT EXISTS mobile_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	expires_at DATETIME NOT NULL
);`
```

And update the first result line accordingly:

```go
	results := []string{"Created tables: users, rate_limits, mobile_tokens"}
```

- [ ] **Step 6: Render the two mobile files and register all six endpoints**

In `handleScaffoldAuth`'s `specs` slice, add the two mobile files (they are fully static, so `data` renders them fine):

```go
	specs := []fileSpec{
		{"user_model.go.tmpl", "/src/app/models/User.go"},
		{"auth_handler.go.tmpl", "/src/app/handlers/auth.go"},
		{"auth_test.go.tmpl", "/src/app/handlers/auth_test.go"},
		{"logout_handler.go.tmpl", "/src/app/handlers/logout.go"},
		{"login_page.html.tmpl", "/src/app/static/pages/login.html"},
		{"login.js.tmpl", "/src/app/static/js/login.js"},
		{"mobile_auth_handler.go.tmpl", "/src/app/handlers/mobile_auth.go"},
		{"mobile_auth_test.go.tmpl", "/src/app/handlers/mobile_auth_test.go"},
	}
```

Replace the inline three-endpoint `endpoints := []Endpoint{ … }` slice with `authEndpoints()`:

```go
	if err := updateManifest([]Model{userModel}, authEndpoints()); err != nil {
		return errResult("manifest update failed: " + err.Error()), nil
	}
```

Update the final registration result line:

```go
	results = append(results, "\nRegistered full auth — cookie (login, logout, me) and bearer (login_token, logout_token, me_token) — plus the user model in api.json + routes_gen.go.")
```

- [ ] **Step 7: Delete `scaffold_mobile_auth`**

Remove the tool registration in `main()`:

```go
	s.AddTool(mcp.NewTool("scaffold_mobile_auth",
		mcp.WithDescription("…"),
	), handleScaffoldMobileAuth)
```

And delete the entire `func handleScaffoldMobileAuth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) { … }` function.

After deletion, `handleScaffoldMobileAuth` no longer exists and nothing references it. If its removal leaves an unused import (e.g. `os` was used only there — check: `os.Stat` was used in `handleScaffoldMobileAuth`; confirm whether `os` is still used elsewhere in `main.go` before removing the import), fix the import block so the file compiles. **Run `go build` to confirm before proceeding.**

- [ ] **Step 8: Build and run the full builder suite**

Run: `cd src/builder && go build ./... && go vet ./... && go test ./... -v`
Expected: build succeeds (no unused imports, no dangling `handleScaffoldMobileAuth` reference); `TestAuthEndpoints_SixWithKinds` passes; the existing `render_test.go` mobile-template checks (`TestMobileAuthTestTemplate_IsValidGo` and any mobile handler render test) still pass (templates retained); all prior tests green.

- [ ] **Step 9: Confirm `scaffold_mobile_auth` is gone from the tool surface**

Run: `cd src/builder && grep -n "scaffold_mobile_auth\|handleScaffoldMobileAuth" main.go`
Expected: no output (both the registration and the handler are gone). The templates remain:
Run: `ls src/builder/templates/mobile_auth_handler.go.tmpl src/builder/templates/mobile_auth_test.go.tmpl`
Expected: both listed.

- [ ] **Step 10: Commit**

```bash
git add src/builder/main.go src/builder/manifest_test.go
git commit -m "feat: scaffold_auth emits cookie+bearer; remove scaffold_mobile_auth"
```

---

## Task 2: Documentation — monolith + gova-ios sweep

**Files:**
- Modify: `CLAUDE.md` (monolith)
- Modify: `../gova-ios/.claude/commands/build.md`, `../gova-ios/.claude/commands/prep.md`, `../gova-ios/.claude/commands/export-mobile.md`, `../gova-ios/CLAUDE.md`

**Interfaces:** consumes everything above; produces no code.

- [ ] **Step 1: Update the monolith CLAUDE.md**

1. **Tool Cheat Sheet:** remove the `| scaffold_mobile_auth | … |` row entirely. Update the `scaffold_auth` row's description to: "Full auth — cookie **and** bearer (web + mobile) in one run: users + rate_limits + mobile_tokens tables, login/logout/me + login_token/logout_token/me_token handlers, all 6 routes self-registered. Run scaffold_registration after for a registration endpoint."
2. **Golden Recipe / Option C** (and anywhere prose pairs `scaffold_auth` with a follow-up `scaffold_mobile_auth`): drop the `scaffold_mobile_auth` follow-up — `scaffold_auth` alone now yields web + mobile auth. If a line reads like "`scaffold_auth()` → `scaffold_registration()`" plus a separate mobile step, keep the registration step and remove the mobile one.

Verify: `grep -n "scaffold_mobile_auth" CLAUDE.md` → no output.

- [ ] **Step 2: Sweep the gova-ios command docs**

In `../gova-ios/.claude/commands/build.md`: the plan step `1. `scaffold_mobile_auth` via MCP (if auth required and MCP is wired) — idempotent, safe to run first` is removed (renumber the remaining plan steps). Add a short note that bearer auth ships with the web app's `scaffold_auth`, so `/build` scaffolds nothing auth-related; if the manifest shows no bearer endpoints (`bearer_auth=no`), the developer runs `scaffold_auth` in the gova-monolith project.

In `../gova-ios/.claude/commands/prep.md`: replace the two `scaffold_mobile_auth` references (the `bearer_auth=no` guidance around line 120-122 and the report line around line 136) with `scaffold_auth`. The "run scaffold_mobile_auth first" phrasing becomes "run scaffold_auth (it now includes bearer/mobile auth)". Also the line ~26 "scaffold_mobile_auth will be unavailable" → "scaffold_auth will be unavailable".

In `../gova-ios/.claude/commands/export-mobile.md`: the follow-up "run `scaffold_mobile_auth` (after `scaffold_auth`)" (around line 76) becomes "run `scaffold_auth`".

- [ ] **Step 3: Sweep the gova-ios CLAUDE.md**

In `../gova-ios/CLAUDE.md`, replace the `scaffold_mobile_auth` references:
- The first-time-setup note (~line 40) mentioning the `gova-builder` tools (`inspect_app`, `scaffold_mobile_auth`) → `scaffold_auth`.
- Translation-guide **Step 2** (~line 67) "Use the gova-builder MCP tool `scaffold_mobile_auth` to add token-based auth endpoints" → "Bearer token endpoints are already present if the web app was built with `scaffold_auth` (which now emits both cookie and bearer auth). If the manifest lacks them, run `scaffold_auth` in the gova-monolith project."
- The MCP-tools cheat-sheet row (~line 251) `| scaffold_mobile_auth | … |` → `| scaffold_auth | Full auth (cookie + bearer) endpoints on the Go API (idempotent) |`.

- [ ] **Step 4: Verify no stale references remain**

```bash
grep -rn "scaffold_mobile_auth" CLAUDE.md
grep -rn "scaffold_mobile_auth" ../gova-ios/CLAUDE.md ../gova-ios/.claude/commands/
```
Expected: no output from either.

- [ ] **Step 5: Commit both repos**

```bash
git add CLAUDE.md
git commit -m "docs: scaffold_auth now covers mobile bearer auth; drop scaffold_mobile_auth"
cd ../gova-ios && git add CLAUDE.md .claude/commands/build.md .claude/commands/prep.md .claude/commands/export-mobile.md && \
  git commit -m "docs: scaffold_auth replaces scaffold_mobile_auth (unified auth)" && cd -
```

---

## Task 3: End-to-end verification via MCP

**Files:** none modified — run and observe, then revert the scratch scaffold.

**Interfaces:** consumes everything above.

**Context:** the `mcp` image embeds templates + the tool set at build time — **rebuild it first** so `scaffold_mobile_auth` is actually gone from the running server and `scaffold_auth` emits the new set. Drive the server over stdio via `docker exec -i gove-test-mcp-1 /usr/local/bin/mcp-server` (initialize → notifications/initialized → tools/call), as in prior builds. The scratch DB is a git-ignored bind mount.

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

- [ ] **Step 3: Confirm the tool surface — scaffold_mobile_auth gone, scaffold_auth present**

Call `tools/list` over the MCP stdio handshake and print the tool names.
Expected: `scaffold_auth`, `scaffold_registration`, `scaffold_resource`, `scaffold_list`, etc. are present; **`scaffold_mobile_auth` is absent**.

- [ ] **Step 4: Run ONLY scaffold_auth**

Call `scaffold_auth` (no args) via MCP.
Expected: reports creating `users, rate_limits, mobile_tokens` and files including `mobile_auth.go`; reports registering cookie + bearer routes.

- [ ] **Step 5: Assert all six endpoints and both handler files**

```bash
python3 -c "import json;d=json.load(open('src/app/api.json'));print(sorted((e['method'],e['path'],e['kind'],e['auth']) for e in d['endpoints']))"
grep -nE "LoginPOST|LogoutPOST|MeGET|MobileLoginPOST|MobileLogoutDELETE|MobileMeGET" src/app/handlers/routes_gen.go | wc -l
ls src/app/handlers/auth.go src/app/handlers/mobile_auth.go
git status --short src/app/main.go
```
Expected: six endpoints (three cookie, three token; only `/me` auth:true, the token trio auth:false); `routes_gen.go` mounts all six; both `auth.go` and `mobile_auth.go` present; `main.go` unmodified.

- [ ] **Step 6: Generated tests compile and pass**

```bash
docker compose restart app
sleep 5
docker compose exec app go test ./...
```
Expected: `ok` including the generated `handlers/auth_test.go` and `handlers/mobile_auth_test.go`.

- [ ] **Step 7: Live proof — one tool, full web + mobile auth**

Register a user, then exercise both cookie and bearer login (mutations need the CSRF token, as established in prior builds):

```bash
until curl -sf localhost:8080/api/v1/_version >/dev/null 2>&1; do sleep 2; done
CSRF=$(curl -s -i localhost:8080/ | grep -i "set-cookie: csrf_token" | sed 's/.*csrf_token=//;s/;.*//' | tr -d '\r')
# scaffold_registration is a separate tool — run it via MCP first, then restart app, to get POST /api/v1/auth/register.
# (If registration is not scaffolded, insert a user via execute_sql instead.)
echo "== register =="; curl -s -X POST localhost:8080/api/v1/auth/register -H "Content-Type: application/json" -H "X-CSRF-Token: $CSRF" --cookie "csrf_token=$CSRF" -d '{"name":"Ada","email":"ada@example.com","password":"correct-horse-battery-staple"}'
echo; echo "== cookie login =="; curl -s -i -X POST localhost:8080/api/v1/auth/login -H "Content-Type: application/json" -H "X-CSRF-Token: $CSRF" --cookie "csrf_token=$CSRF" -d '{"email":"ada@example.com","password":"correct-horse-battery-staple"}' | grep -iE "HTTP/|gova_session" | head
echo "== bearer login_token =="; TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/login_token -H "Content-Type: application/json" -d '{"email":"ada@example.com","password":"correct-horse-battery-staple"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['token'])")
echo "token: ${TOKEN:0:12}..."
echo "== me_token with bearer (expect 200 + user) =="; curl -s localhost:8080/api/v1/auth/me_token -H "Authorization: Bearer $TOKEN"
```
Expected: register → `{"ok":true,...}`; cookie login → `200` with a `Set-Cookie: gova_session=…`; `login_token` → a token; `me_token` with the bearer → `200` with Ada's `id`/`name`/`email`. Both auth mechanisms work from the single `scaffold_auth` run. (`login_token` is CSRF-exempt by path, so it needs no token; it issues one.)

- [ ] **Step 8: Revert the scratch scaffold**

```bash
git checkout -- src/app/api.json src/app/handlers/routes_gen.go
rm -f src/app/models/User.go src/app/handlers/auth.go src/app/handlers/auth_test.go \
      src/app/handlers/logout.go src/app/handlers/mobile_auth.go src/app/handlers/mobile_auth_test.go \
      src/app/static/pages/login.html src/app/static/js/login.js
# If scaffold_registration was run in Step 7, also remove its files:
rm -f src/app/handlers/register.go src/app/handlers/register_test.go \
      src/app/static/pages/register.html src/app/static/js/register.js
git status --short
```
Expected: clean tree (committed empty `api.json`/`routes_gen.go` restored, unmodified).

- [ ] **Step 9: Reset DB and final suites**

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
Expected: both suites pass on the clean tree; tree clean; the log shows the Build 3c commits.

---

## Verification Summary

| Concern | Where proven |
|---|---|
| `authEndpoints()` returns the six with correct handlers/kinds/auth | Task 1 |
| Token endpoints auth:false, cookie `me` auth:true | Task 1, Task 3 Step 5 |
| `scaffold_mobile_auth` removed (tool + handler), templates kept | Task 1 Step 9, Task 3 Step 3 |
| One `scaffold_auth` run yields all tables + files + 6 routes | Task 3 Steps 4-5 |
| Generated auth + mobile_auth tests pass | Task 3 Step 6 |
| Cookie AND bearer login both work from one run | Task 3 Step 7 |
| No stale `scaffold_mobile_auth` docs in either repo | Task 2 Step 4 |
