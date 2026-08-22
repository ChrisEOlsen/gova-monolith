# Unified Auth — Design

**Date:** 2026-07-23
**Status:** Approved
**Scope:** Build 3c of 3 (the "3" build split into 3a/3b/3c) in the "API-first for native clients" effort. Monolith-side, plus a gova-ios documentation sweep.

---

## Problem

Auth is split across two MCP tools that must be run in sequence:

- `scaffold_auth` creates the `users` and `rate_limits` tables and the cookie/session
  handlers (`LoginPOST`, `LogoutPOST`, `MeGET`) — web auth.
- `scaffold_mobile_auth` (which *requires* `scaffold_auth` to have run first, for the
  `users` table) creates the `mobile_tokens` table and the bearer-token handlers
  (`MobileLoginPOST`, `MobileLogoutDELETE`, `MobileMeGET`) — mobile auth.

This is a split-brain. A developer who runs only `scaffold_auth` gets a web app with
no mobile auth, and nothing signals the gap until an iOS build fails to authenticate.
The workaround has been procedural: the gova-ios `/build` workflow carries a plan step
reminding the developer to run `scaffold_mobile_auth`, and `prep.md`/`export-mobile.md`
carry the same reminder. That is a documentation band-aid over a tooling seam.

This build removes the seam: `scaffold_auth` emits **both** cookie and bearer auth
from one run — all six endpoints, all tables, all handlers — and `scaffold_mobile_auth`
is removed. "Set up auth" becomes one command that produces a web- and mobile-ready
auth system.

---

## Decisions

| Area | Decision |
|---|---|
| Login shape | One tool, **two** login endpoints — `scaffold_auth` emits the cookie set (`login`/`logout`/`me`) and the bearer set (`login_token`/`logout_token`/`me_token`). NOT a single merged login (keeps bearer tokens out of web responses). |
| `scaffold_mobile_auth` | **Removed** (tool + handler). The two mobile templates stay; `scaffold_auth` uses them. |
| Mobile endpoint auth | `auth:false` (self-enforcing bearer) — unchanged from Build 2's Critical fix. |
| Existing apps | Regenerate; no migration tooling (idempotent DDL, identical files). |
| `scaffold_registration` | Unaffected — stays a separate tool. |

---

## §1 `scaffold_auth` absorbs the mobile pieces

`handleScaffoldAuth` is extended so one run produces the complete auth system.

### DDL (one exec, ordered)

The existing `users` + `rate_limits` DDL gains `mobile_tokens`:

```sql
CREATE TABLE IF NOT EXISTS users ( … );
CREATE TABLE IF NOT EXISTS rate_limits ( … );
CREATE TABLE IF NOT EXISTS mobile_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	expires_at DATETIME NOT NULL
);
```

`mobile_tokens.user_id` references `users(id)`; because `users` is created first in the
same statement, the FK resolves. This inherent ordering is exactly the constraint the
old `scaffold_mobile_auth` enforced procedurally ("run `scaffold_auth` first").

### Files generated

The existing cookie files, **plus** the two mobile files (from the retained
`mobile_auth_handler.go.tmpl` / `mobile_auth_test.go.tmpl`):

| File | Source template |
|---|---|
| `models/User.go` | `user_model.go.tmpl` |
| `handlers/auth.go` | `auth_handler.go.tmpl` |
| `handlers/auth_test.go` | `auth_test.go.tmpl` |
| `handlers/logout.go` | `logout_handler.go.tmpl` |
| `static/pages/login.html` | `login_page.html.tmpl` |
| `static/js/login.js` | `login.js.tmpl` |
| **`handlers/mobile_auth.go`** | `mobile_auth_handler.go.tmpl` |
| **`handlers/mobile_auth_test.go`** | `mobile_auth_test.go.tmpl` |

All rendered unconditionally (consistent with how `scaffold_auth` already renders
`auth.go`); re-running overwrites with identical content. (The old
`scaffold_mobile_auth` had a skip-if-exists guard because it was a separately-invoked
tool; unified, that guard is unnecessary.)

### Manifest — all six endpoints in one `updateManifest`

```
POST   /api/v1/auth/login        LoginPOST          rwc  kind auth_login
POST   /api/v1/auth/logout       LogoutPOST         []   kind auth_logout
GET    /api/v1/auth/me           MeGET              rwc  auth:true  kind auth_me
POST   /api/v1/auth/login_token  MobileLoginPOST    rwc  kind mobile_login
DELETE /api/v1/auth/logout_token MobileLogoutDELETE w    auth:false kind mobile_logout
GET    /api/v1/auth/me_token     MobileMeGET        rwc  auth:false kind mobile_me
```

Plus the `user` model (`id`, `name`, `email`, `created_at` — **no** `password` field,
unchanged). The mobile endpoints keep `auth:false` — they self-enforce the bearer token
in the handler; a session-cookie `RequireAuth` wrap would 401 them (Build 2's Critical
fix). `MobileLoginPOST` issues the token, so it too is `auth:false`.

No handler logic changes: `auth.go` (cookie) and `mobile_auth.go` (bearer) already
coexist correctly against the shared `users` table. This is a tool merge.

---

## §2 Remove `scaffold_mobile_auth`

- Delete its `s.AddTool(mcp.NewTool("scaffold_mobile_auth", …))` registration in `main()`.
- Delete `handleScaffoldMobileAuth`.
- **Keep** `mobile_auth_handler.go.tmpl` and `mobile_auth_test.go.tmpl` — `scaffold_auth`
  now renders them.

Removing the tool is a breaking change for anyone scripting `scaffold_mobile_auth`
directly. Acceptable in a template repo where apps are regenerated; the capability is
not lost, only relocated into `scaffold_auth`.

---

## §3 Documentation

### Monolith `CLAUDE.md`
- **Tool Cheat Sheet:** remove the `scaffold_mobile_auth` row; update the `scaffold_auth`
  row to "Full auth — cookie **and** bearer (web + mobile) in one run: users +
  rate_limits + mobile_tokens tables, login/logout/me + login_token/logout_token/me_token
  handlers, all 6 routes self-registered."
- **Golden Recipe Option C** and any prose that pairs `scaffold_auth` with a follow-up
  `scaffold_mobile_auth` step: drop the follow-up — `scaffold_auth` is now sufficient.

### gova-ios (cross-repo sweep — separate repo, its own commit)
Replace every `scaffold_mobile_auth` reference (grep found them in `.claude/commands/build.md`,
`.claude/commands/prep.md`, `.claude/commands/export-mobile.md`, and `CLAUDE.md`):
- **`build.md`:** the plan step "1. `scaffold_mobile_auth` via MCP (if auth required…)"
  is removed — bearer auth now ships with the web app's `scaffold_auth`, so `/build`
  scaffolds nothing auth-related. If the manifest shows no bearer endpoints, the
  developer is told to run `scaffold_auth` in the monolith.
- **`prep.md`:** the `bearer_auth=no` guidance and the report line change
  `scaffold_mobile_auth` → `scaffold_auth`.
- **`export-mobile.md`:** the "run `scaffold_mobile_auth` (after `scaffold_auth`)"
  follow-up becomes "run `scaffold_auth`".
- **`CLAUDE.md`:** the setup note, the translation-guide Step 2, and the MCP-tools cheat
  sheet — `scaffold_mobile_auth` → `scaffold_auth` (and note it now includes bearer auth).

3a's `/export:mobile` already reports `bearer_auth=yes` once `scaffold_auth` runs (it
keys off the `mobile_login` endpoint kind), so **no export-script change** is needed.

---

## §4 Testing

**Builder suite (host):**
- A test asserting `scaffold_auth`'s registration set is exactly the six endpoints
  above — correct methods, paths, handlers, kinds, and auth flags (`me` auth:true;
  the three token endpoints auth:false). Factor the endpoint list into a testable
  helper (e.g. `authEndpoints()`), mirroring `resourceEndpoints` from Build 3b.
- A test/assertion that `scaffold_mobile_auth` is no longer registered (the tool list
  in `main()` does not contain it) and `handleScaffoldMobileAuth` is gone.
- The existing `render_test.go` template checks for `mobile_auth_handler.go.tmpl` /
  `mobile_auth_test.go.tmpl` still pass (templates retained).

**End-to-end (rebuilt MCP image — templates are `go:embed`-ed at build time):**
- Run **only** `scaffold_auth` → assert `api.json` has all six auth endpoints,
  `routes_gen.go` mounts all six, `handlers/mobile_auth.go` and `handlers/auth.go` both
  generated, `mobile_tokens` table exists, and `scaffold_mobile_auth` is not a listed
  tool (a `tools/list` call omits it).
- The generated `auth_test.go` and `mobile_auth_test.go` compile and pass.
- **Live proof** — the flow that required two tool runs in Build 2 now needs one: run
  `scaffold_auth`, then register a user (`scaffold_registration` + a create), cookie-login
  (with CSRF), and `login_token` → bearer → `GET /me_token` returns **200** with the user.

---

## §5 Scope boundary

Out of scope, deferred / unchanged:
- **A single merged login endpoint** issuing both cookie and token — deliberately not
  done (keeps bearer tokens out of web responses); two login endpoints from one tool.
- **`scaffold_registration`** — unchanged, separate tool.
- **The web login/mobile handler logic** — unchanged; only the tool that emits them.
- **Migrating existing apps** — regenerate; idempotent DDL and identical files.
- **The password-in-JSON model-template follow-up** noted at the end of Build 3b — a
  separate future security pass, not part of auth unification.

---

## Files touched

**`gova-monolith`**

| File | Change |
|---|---|
| `src/builder/main.go` | `handleScaffoldAuth`: add `mobile_tokens` DDL, render the two mobile files, register all 6 endpoints (factor `authEndpoints()`); **remove** `scaffold_mobile_auth` tool registration + `handleScaffoldMobileAuth` |
| `src/builder/manifest_test.go` (or a builder test) | `authEndpoints()` returns the six with correct fields; `scaffold_mobile_auth` absent |
| `CLAUDE.md` | Cheat-sheet + Golden Recipe: `scaffold_auth` now covers mobile; drop `scaffold_mobile_auth` |

**`gova-ios`**

| File | Change |
|---|---|
| `.claude/commands/build.md` | Remove the `scaffold_mobile_auth` plan step |
| `.claude/commands/prep.md` | `scaffold_mobile_auth` → `scaffold_auth` in the bearer-auth guidance/report |
| `.claude/commands/export-mobile.md` | `scaffold_mobile_auth` → `scaffold_auth` follow-up |
| `CLAUDE.md` | Setup note, translation-guide Step 2, MCP-tools cheat sheet → `scaffold_auth` |
