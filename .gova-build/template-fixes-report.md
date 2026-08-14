# Template defect fixes — report

**Branch:** `fix/template-defects` (pushed to `origin`, 5 commits on top of `9188b1c`)

| Commit | Defect |
|---|---|
| `0eac012` | A1 — `scaffold_auth` emitted raw SQL in a handler; expiry layout unpinned |
| `4a1eacc` | A2 — rate limiter never decayed |
| `8923808` | A3 — `TestRenderRoutes_EmptyMatchesCommittedFile` permanently red in generated projects |
| `e62c225` | A4 — `create_page` could not register a page route |
| `5305e80` | A5 — scaffolds generated pages they never registered (+ `CLAUDE.md`) |

No Go toolchain exists on this host, so every compile and test run below happened
inside a `golang:1.25` container with the repo bind-mounted — the same image the
project's own `Dockerfile` uses. Docker was never used to run the app as a
service; the serve-side proof is `httptest` (see A4/A5).

---

## A1 — `scaffold_auth` emitted raw SQL in a handler

**What changed**

- New template `src/builder/templates/mobile_token_model.go.tmpl` →
  `models/MobileToken.go`, exposing `Issue` / `Revoke` / `UserIDAt` / `UserID`
  (plus `DeleteExpired` for a future prune job).
- `mobile_auth_handler.go.tmpl` rewritten: no SQL, no `*sql.DB` calls at all —
  it constructs `models.NewMobileTokenModel(readDB, writeDB)` and calls methods.
- Added to `scaffold_auth`'s file spec list in `src/builder/main.go`.
- `MobileLogoutDELETE` now takes both db handles (`deps: ["read","write"]`)
  because it reaches the table through the model constructor rather than
  touching `writeDB` itself. `TestAuthEndpoints_SixWithKinds` updated.

**Expiry correct by construction (FINDINGS §B1)**

`expires_at` is TEXT and SQLite compares lexicographically. RFC3339 writes `T`
(0x54) where SQLite writes a space (0x20), so an RFC3339 expiry sorts *above*
the current native timestamp for any instant on the same calendar date — with a
30-day TTL, an expired token would be accepted for the whole day it expired on.
The old code was sound only by accident (it bound a Go `time.Time` and relied on
driver encoding). The model now formats **both** the stored value and the
comparison instant with `sqliteDatetimeLayout = "2006-01-02 15:04:05"` in UTC,
and binds the comparison value rather than using `CURRENT_TIMESTAMP`.

**Guards added**

- `TestMobileAuthHandlerTemplate_NoRawSQL` — fails on any of `INSERT INTO`,
  `DELETE FROM`, `SELECT `, `UPDATE `, `ExecContext(`, `QueryContext(`,
  `QueryRowContext(`, `.Exec(`, `.Query(`, `.QueryRow(` in the handler template,
  and requires `models.NewMobileTokenModel(`.
- `TestMobileTokenModelTemplate_PinsSQLiteDatetimeLayout` — pins the layout,
  rejects `time.RFC3339`, requires the bound-`?` comparison, rejects a
  `> CURRENT_TIMESTAMP` comparison.
- Generated `TestMobileMeGET_RejectsExpiredToken` — uses an expired instant on
  the **current UTC calendar date** (the only fixture that can catch the trap),
  asserts non-vacuity (really past, really same-date), and waits out the
  near-midnight case rather than skipping.
- Generated `TestMobileLogoutDELETE_RevokesToken`.

**Proof the expiry test is non-vacuous** — same generated tree, `Issue` switched
to store RFC3339 and nothing else changed:

```
MobileToken.Issue now stores RFC3339 (the B1 trap)

########## A1: expiry test against RFC3339-stored expiry ##########
=== RUN   TestMobileMeGET_ValidatesBearerToken
--- PASS: TestMobileMeGET_ValidatesBearerToken (0.10s)
=== RUN   TestMobileMeGET_RejectsMissingBearerToken
--- PASS: TestMobileMeGET_RejectsMissingBearerToken (0.00s)
=== RUN   TestMobileMeGET_RejectsExpiredToken
    mobile_auth_test.go:155: expired token was accepted: got 200, want 401, body: {"ok":true,"data":{"email":"ada@example.com","id":1,"name":"Ada"}}
--- FAIL: TestMobileMeGET_RejectsExpiredToken (0.05s)
FAIL
FAIL	gova/app/handlers	0.198s
FAIL
```

Green with the shipped template (from the replayed tree):

```
=== RUN   TestMobileLoginPOST_IssuesToken
--- PASS: TestMobileLoginPOST_IssuesToken (0.09s)
=== RUN   TestMobileMeGET_ValidatesBearerToken
--- PASS: TestMobileMeGET_ValidatesBearerToken (0.09s)
=== RUN   TestMobileMeGET_RejectsMissingBearerToken
--- PASS: TestMobileMeGET_RejectsMissingBearerToken (0.00s)
=== RUN   TestMobileMeGET_RejectsExpiredToken
--- PASS: TestMobileMeGET_RejectsExpiredToken (0.05s)
=== RUN   TestMobileLogoutDELETE_RevokesToken
--- PASS: TestMobileLogoutDELETE_RevokesToken (0.09s)
=== RUN   TestMobileLoginPOST_RateLimited
--- PASS: TestMobileLoginPOST_RateLimited (0.05s)
```

---

## A2 — the rate limiter never decayed

**What changed** — `user_model.go.tmpl`'s `RecordFailedAttempt` `ON CONFLICT`
branch:

```sql
attempts = CASE WHEN updated_at < datetime('now', '-15 minutes')
    THEN 1 ELSE attempts + 1 END,
locked_until = CASE
    WHEN updated_at < datetime('now', '-15 minutes') THEN NULL
    WHEN attempts + 1 >= 5 THEN datetime('now', '+15 minutes')
    ELSE locked_until END,
updated_at = CURRENT_TIMESTAMP
```

Bare column names on the right of `DO UPDATE SET` read the pre-update row, and
`updated_at` / `datetime('now', ...)` share SQLite's own text layout, so the
window comparison is like-for-like. A decayed bucket also clears its stale
`locked_until`.

**Guards** — generated `TestRecordFailedAttempt_BucketDecays` drives 5 failures,
asserts locked, ages the row past the window, asserts the lock lapsed, records
one more failure and asserts it does **not** re-lock, then asserts 4 failures in
the fresh window stay open and the 5th locks again. Plus
`TestUserModelTemplate_RateLimitBucketDecays` pinning the clause.

**Proof against the old template** — generated tree with only the `ON CONFLICT`
branch reverted to the increment-only form:

```
reverted RecordFailedAttempt to the pre-fix increment-only form

########## A2: generated decay test against the OLD (increment-only) model ##########
=== RUN   TestLoginPOST_RateLimited
--- PASS: TestLoginPOST_RateLimited (0.05s)
=== RUN   TestRecordFailedAttempt_BucketDecays
    auth_test.go:152: bucket re-locked on the first failure after the window — attempts never decay, so this IP is capped at one attempt per 15 minutes forever
--- FAIL: TestRecordFailedAttempt_BucketDecays (0.00s)
=== RUN   TestMobileLoginPOST_RateLimited
--- PASS: TestMobileLoginPOST_RateLimited (0.05s)
FAIL
FAIL	gova/app/handlers	0.146s
FAIL
```

Note the two pre-existing rate-limit tests **pass against the bug** — exactly
the trap FINDINGS §A2 warned about.

---

## A3 — permanently-red sync test

**What changed** — `TestRenderRoutes_EmptyMatchesCommittedFile` deleted. Replaced
by `TestRenderRoutes_MatchesCommittedManifest`, which renders from the committed
`api.json` and asserts the committed `routes_gen.go` matches. A companion
`TestRenderPages_MatchesCommittedManifest` (landed with A4) covers
`pages_gen.go` and `pages_gen_test.go`.

**Proof it fails when it should** — desync `routes_gen.go` from `api.json`
(`-count=1`; Go's test cache does not track the cross-module file read, so
caching masks the change without it):

```
########## A3: baseline (routes_gen.go in sync with api.json) ##########
ok  	gova/builder	0.003s

########## A3: desync routes_gen.go from api.json ##########
--- FAIL: TestRenderRoutes_MatchesCommittedManifest (0.00s)
    render_test.go:532: handlers/routes_gen.go has drifted from api.json.
        Re-run any scaffold tool (or regenerate) to bring them back in sync.
        ---committed---
        // Code generated from api.json by gova-builder. DO NOT EDIT.
        package handlers
        ...
        func RegisterGenerated(r chi.Router, database *db.DB, appCache *cache.Cache) {
        	r.Get("/api/v1/ghost", GhostGET(database.Read))
        }

########## A3: restored ##########
ok  	gova/builder	0.003s
git diff (should be empty):
```

**Proof it fixes the actual complaint** — in a *scaffolded* tree (13 endpoints,
5 pages, 3 models), the new test is green and the old assertion is red:

```
scaffolded project's api.json has:
   13 endpoints, 5 pages, 3 models

########## NEW test, in the scaffolded tree ##########
ok  	gova/builder	0.003s

########## OLD test (empty manifest vs committed file), same scaffolded tree ##########
--- FAIL: TestOldA3_RenderRoutesEmptyMatchesCommittedFile (0.00s)
    zz_old_a3_test.go:20: committed routes_gen.go is not byte-identical to renderRoutes(empty)
FAIL
FAIL	gova/builder	0.002s
FAIL
```

---

## A4 — `create_page` could not register a page route

### Design decision and why

**A separate `pages` array, not a `kind: "page"` endpoint.** A page has no method
beyond GET, no request or response body, and no deps. Putting it in `endpoints`
would place non-API rows in front of every manifest consumer (the iOS client
reads this file) and force each one to filter. Keeping the tables apart also
makes the namespaces **provably** disjoint rather than conventionally so:
`create_handler` requires `/api/v1/`, `create_page` refuses `/api/` — the exact
inverse — so neither tool can register a path the other could shadow. That is a
structural guarantee, checked by `TestValidatePagePath`.

**`Page{Path, File, Title, Auth}`.** `File` is the shell's base name under
`static/pages`; it is the only value that reaches the filesystem, and it comes
from the manifest, never from a request.

**`pages_gen.go` mirrors `routes_gen.go`.** `RegisterPages(r chi.Router)`, one
call from `main.go`. Serving is `http.ServeFile("./static/pages/" +
filepath.Base(name) + ".html")`. Two layers: the argument is a compile-time
literal from the generated table (so request input has no path to the
filesystem at all), and `filepath.Base` is a second guard against a hostile
generated name.

**`create_page` no longer emits a Go handler stub.** With the page served by
`pageFile`, a stub returning `jsonOK(w, nil)` is dead code — and it is precisely
the confusion that produced this defect. This is a contract change and is
documented in `CLAUDE.md`.

**A page's `auth` is declarative only — pages are NOT wrapped in
`middleware.RequireAuth`.** `RequireAuth` answers with a JSON 401 body, which is
the wrong response to a browser navigation; the template's own documented pattern
is a client-side `requireAuth()` redirect in the JS module. The shell contains no
data, and the data behind it is protected on its own `/api/v1/` endpoints, where
`auth: true` genuinely enforces something. The flag is still recorded in
`api.json` so a future middleware (or a doc generator) can use it.
`TestRenderPages_MountsEachPage` asserts no page line is wrapped and that
`pages_gen.go` does not import `middleware`.

**Pages are in the manifest hash.** Adding or moving a page is a change to what
the server serves, so it must show up in `_version`'s `manifest_hash`.
`canonicalize` normalizes `nil` → `[]` for all three tables so `null` and `[]`
cannot hash differently.

**`UpsertPage` mirrors `UpsertEndpoint`.** Same path + same file refreshes in
place; same path + different file returns an error naming the incumbent and
leaves the manifest untouched. `updateManifestAt` applies page upserts inside the
same all-or-nothing block as endpoints, so a page conflict aborts the endpoint
writes too.

**`inspect_app`** now flags a registered page whose `.html` shell is missing.

### Other files touched

`src/app/main.go` (one `handlers.RegisterPages(r)` call),
`src/app/handlers/manifest.go` (mirror the `pages` field so `_manifest` reports
it), `src/app/api.json` (regenerated: `pages: []` + new hash),
`src/app/handlers/pages_gen.go` + `pages_gen_test.go` (committed empty renders),
`src/app/handlers/pages_test.go` (hand-written guards).

---

## A5 — scaffolds register the pages they generate

**Path scheme** (evaluated, then adopted as FINDINGS §A5 recorded it):
`scaffold_auth` → `/login`, `scaffold_registration` → `/register`,
`scaffold_list` and `scaffold_resource` → `/<plural>`. `toPlural` never returns
its input unchanged, so a resource named `login` registers `/logins` and the
auth/resource namespaces cannot collide for any resource name — no reserved-word
list needed. `auth` off for both auth pages. Asserted by
`TestListPage_PluralNamespaceCannotCollideWithAuthPages`.

**One correction to the recorded scheme:** the page titles are set to each
shell's own `<title>` (`Sign In`, `Create Account`) rather than to the tool name,
so `api.json` cannot describe a page differently from how it renders. The
generated test asserts that agreement and would catch drift in either direction.

### End-to-end proof

Replayed all four scaffolds plus `create_page` against a copy of the app tree
using the real MCP tool handler bodies, from the **committed** HEAD.

Resulting `api.json` `pages`:

```json
[
  { "path": "/dashboard", "file": "dashboard", "title": "Dashboard",      "auth": false },
  { "path": "/gadgets",   "file": "gadgets",   "title": "Gadgets",        "auth": false },
  { "path": "/login",     "file": "login",     "title": "Sign In",        "auth": false },
  { "path": "/register",  "file": "register",  "title": "Create Account", "auth": false },
  { "path": "/widgets",   "file": "widgets",   "title": "Widgets",        "auth": false }
]
```

Generated `pages_gen.go` body:

```go
func RegisterPages(r chi.Router) {
	r.Get("/dashboard", pageFile("dashboard"))
	r.Get("/gadgets", pageFile("gadgets"))
	r.Get("/login", pageFile("login"))
	r.Get("/register", pageFile("register"))
	r.Get("/widgets", pageFile("widgets"))
}
```

Refusals, verbatim from the replay:

```
[create_page under /api/v1/ REFUSED as expected]
page path must not be under /api/ — that namespace belongs to create_handler and the scaffold tools; give the page a human-facing URL like /projects

[create_page conflicting path REFUSED as expected]
manifest update failed: page conflict: /dashboard is already served by static/pages/dashboard.html, cannot reassign to dashboard_v2.html
```

The replay also asserts, and it passed: `api.json` and `pages_gen.go` were
byte-unchanged by the conflicting call; re-running `scaffold_auth` and
`scaffold_list` produced exactly one row and one `r.Get` line per path; and
`handlers/dashboard.go` was not created.

**Serve proof — `httptest`, not `curl`.** A generated
`handlers/pages_gen_test.go` is rendered alongside `pages_gen.go` from the same
page table. It `t.Chdir("..")` to `src/app` (so `./static/pages/...` resolves as
it does when the server runs), builds a real `chi` router, calls the real
`RegisterPages(r)`, and for every registered page asserts 200, byte-equality with
the shell on disk, and the expected `<title>`. This is a committed generator
artifact, so it keeps proving the property for every future change rather than
evaporating after one run.

```
=== RUN   TestGeneratedPages_ServeTheirShell
--- PASS: TestGeneratedPages_ServeTheirShell (0.00s)
=== RUN   TestGeneratedPages_UnregisteredPathIs404
--- PASS: TestGeneratedPages_UnregisteredPathIs404 (0.00s)
=== RUN   TestGeneratedPages_RequestPathCannotTraverse
--- PASS: TestGeneratedPages_RequestPathCannotTraverse (0.00s)
=== RUN   TestRegisterPages_AddsNoCatchAll
--- PASS: TestRegisterPages_AddsNoCatchAll (0.00s)
=== RUN   TestPageFile_ServesOnlyFromStaticPages
--- PASS: TestPageFile_ServesOnlyFromStaticPages (0.00s)
=== RUN   TestPageRoute_RequestPathCannotTraverse
--- PASS: TestPageRoute_RequestPathCannotTraverse (0.00s)
PASS
ok  	gova/app/handlers	0.422s
```

Non-vacuity of the serve assertion — remove one `r.Get` line from `pages_gen.go`,
reproducing the old defect exactly (shell on disk, page never registered):

```
--- simulating the old defect: shell emitted, page never registered ---
--- FAIL: TestGeneratedPages_ServeTheirShell (0.00s)
    pages_gen_test.go:65: GET /widgets: got 404, want 200 (page registered but not served)
FAIL
FAIL	gova/app/handlers	0.047s
FAIL
--- restored ---
ok  	gova/app/handlers	0.047s
```

### Traversal — which mechanism does the work

Two distinct properties, tested separately, because conflating them would let a
404 for the wrong reason look like a pass.

1. **URL-level.** `chi` routes on the literal request path and does not clean it,
   so a traversing URL matches no page route and `pageFile` is never invoked.
   `TestPageRoute_RequestPathCannotTraverse` (hand-written, with a sanity
   assertion that the fixture route itself works, so a 404 means "no match" and
   not "nothing mounted") and `TestGeneratedPages_RequestPathCannotTraverse`
   (generated, targeting the real `api.json` one level above `static/pages`)
   cover `/../secrets.html`, `/widgets/../../secrets.html`, `/%2e%2e%2f...`,
   `/..%2f...`, `/widgets/..%2f..%2f...`. Each asserts 404, no secret content,
   and no page shell in the body.
2. **`filepath.Base` guard.** Reached only when the *generated name* is hostile,
   which is the direction the guard exists for.
   `TestPageFile_ServesOnlyFromStaticPages` calls `pageFile` directly with
   `../secrets`, `../../secrets`, `foo/../../secrets`, `/etc/passwd` and asserts
   the secret is never served. **And** — because those 404s would also occur if
   `pageFile` were simply broken — it asserts that
   `pageFile("../../../widgets")` returns **200 serving
   `static/pages/widgets.html`**. That can only happen if `filepath.Base` actually
   ran and collapsed the path, so the guard is proven reached, not assumed.

---

## Verification summary

From the committed branch tip (`5305e80`):

```
########## src/builder ##########
72          # --- PASS count
ok  	gova/builder	0.033s
########## src/app ##########
?   	gova/app	[no test files]
?   	gova/app/cache	[no test files]
ok  	gova/app/db	0.006s
ok  	gova/app/handlers	0.004s
ok  	gova/app/middleware	0.001s
ok  	gova/app/models	0.001s
```

`go vet` clean in both modules.

Fresh replay from `git archive HEAD` (nothing from the working tree):

```
########## replay all scaffolds from committed HEAD ##########
ok  	gova/builder	0.063s
########## generated app: build + full suite ##########
?   	gova/app	[no test files]
?   	gova/app/cache	[no test files]
ok  	gova/app/db	0.007s
ok  	gova/app/handlers	0.846s
ok  	gova/app/middleware	0.001s
ok  	gova/app/models	0.011s
########## generated builder suite stays green in the scaffolded project (A3) ##########
ok  	gova/builder	0.030s
```

`~/Desktop/repos/exersites-2.0` was not modified (`git status` clean, still at
`0717344`).

---

## Not verified

- **The app has not been run as a live HTTP server.** No `curl` against a bound
  port, no `docker compose up`. The serving path is proven by `httptest` against
  the real `chi` router and the real `RegisterPages`/`pageFile`, which covers
  routing, file resolution and traversal; what it does not cover is the process
  actually listening, Traefik/tunnel routing, and Tailwind having produced
  `static/css/style.css`. Those are deployment concerns, unchanged by this work.
- **The `mcp` image was not rebuilt.** Templates are `go:embed`-ed at image build
  time, so whoever picks this branch up must run `docker compose up -d --build`
  before the new templates take effect. `docker compose restart` will silently
  keep generating the old shapes.
- **`inspect_app`'s new page-divergence branch** is covered only by reading; the
  MCP dispatch boundary is not reachable from tests because `manifestFilePath` /
  `handlersDirPath` / `sqliteDSN` are hardcoded (FINDINGS §A6). The replay calls
  the tool handler bodies directly, which is the same limitation.
- **No JS was tested** (Critical Constraint 4 — no Node). The generated `.js`
  modules for the newly-reachable pages are unexercised beyond being served.

## Incidental notes

- `src/builder/main.go` is not `gofmt`-clean at one spot (a missing blank line
  before `resourceEndpoints`). This is **pre-existing at `9188b1c`** and was left
  alone to keep the diffs defect-scoped.
- The replay harness (`zz_replay_test.go`) is deliberately **not** committed — it
  hardcodes `/src/app` container paths and exists to exercise the tool handlers,
  not to ship. The committed permanent equivalents are
  `handlers/pages_gen_test.go` (generated) and `handlers/pages_test.go`
  (hand-written).
