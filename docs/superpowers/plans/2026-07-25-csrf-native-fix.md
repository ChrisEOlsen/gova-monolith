# CSRF Native-Client Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use gova-build-execution to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make CSRF middleware enforce the double-submit check only for browser clients that carry the `csrf_token` cookie, so no-auth native clients stop getting 403 on every mutation.

**Architecture:** Read the incoming `csrf_token` cookie once. Enforce the `X-CSRF-Token` header match only when that cookie is present (a browser participating in double-submit). A request that brings no cookie is a native/non-browser client with no ambient credential to forge — pass it through. Stop generating-and-setting a cookie inside the mutating branch (the bug). Keep issuing the cookie on safe (GET) methods.

**Tech Stack:** GOVA Monolith — Go/chi, SQLite, vanilla JS, Tailwind

## Global Constraints

- **Infrastructure file, hand-written.** `middleware/csrf.go` is app-wide plumbing — no MCP scaffold tool applies (CLAUDE.md infrastructure exception). Edit directly.
- **App-side only.** No `src/builder/` change → no mcp image rebuild. `docker compose restart app` picks up the change.
- **Security boundary — do not weaken the browser path.** A browser forged cross-site POST still carries the cookie (auto-attached) and must still 403 without a matching header. The narrowing applies only to requests that bring no `csrf_token` cookie.
- **Wire contract on the 403:** unchanged — `{"ok":false,"error":"invalid CSRF token","code":"forbidden"}`, status 403.

---

### Task 1: CSRF enforces only for cookie-bearing browser clients

**Files:**
- Modify: `src/app/middleware/csrf.go` — read incoming cookie once; gate header enforcement on cookie presence; remove cookie synthesis in the mutating branch.
- Create: `src/app/middleware/csrf_test.go` — net-new coverage that mutation-proves the fix (template currently ships no CSRF test).

**Interfaces:**
- Consumes: nothing new. Uses stdlib `net/http`, `crypto/hmac`, existing `secureCookies`, `csrfKey`, `generateToken()`, `CSRFToken()`.
- Produces: no new exported symbols. `CSRF(next http.Handler) http.Handler` keeps its signature; `CSRFToken(r)` keeps working.

- [ ] **Step 1: Rewrite the `CSRF` handler body**

Replace the body of `CSRF` in `src/app/middleware/csrf.go` (keep the package, imports, `csrfKey`, `generateToken`, `CSRFToken`, and the `Bearer`/`login_token` fast-path exemption comment + check unchanged). New body:

```go
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bearer-token requests (mobile clients) carry no cookies for this
		// origin, so a forged cross-site request can't replay them the way
		// it can a session cookie — CSRF doesn't apply to them.
		//
		// login_token is exempted by path for the same reason even though
		// it can't carry a Bearer header yet — it's the request that issues
		// the token, so there's nothing to attach. CSRF's threat model is a
		// browser auto-attaching credentials to a forged cross-site request;
		// a native app calling this endpoint directly was never reachable
		// that way in the first place.
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.URL.Path == "/api/v1/auth/login_token" {
			next.ServeHTTP(w, r)
			return
		}

		// Read the INCOMING cookie once, before we might set one. Its
		// presence is the signal that this client is a browser participating
		// in the double-submit scheme (an earlier safe-method load set it).
		var token string
		hasCookie := false
		if cookie, err := r.Cookie("csrf_token"); err == nil {
			token = cookie.Value
			hasCookie = true
		} else {
			// No cookie yet. Mint one so browser JS can read it, but only
			// issue it on safe methods — a mutating request that brought no
			// cookie is a native/non-browser client we must NOT force into
			// the scheme (doing so is the bug that 403'd every mobile write).
			token = generateToken()
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				http.SetCookie(w, &http.Cookie{
					Name:     "csrf_token",
					Value:    token,
					Path:     "/",
					HttpOnly: false,
					Secure:   secureCookies,
					SameSite: http.SameSiteStrictMode,
				})
			}
		}

		ctx := context.WithValue(r.Context(), csrfKey, token)

		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			// A client that brought no csrf_token cookie is not a browser
			// riding an ambient credential — there is nothing for CSRF to
			// protect, so let it through. A browser forged cross-site POST
			// DOES carry the cookie (auto-attached) but cannot carry the
			// matching header, so it still fails below.
			if !hasCookie {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			headerToken := r.Header.Get("X-CSRF-Token")
			if !hmac.Equal([]byte(token), []byte(headerToken)) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"ok":false,"error":"invalid CSRF token","code":"forbidden"}`))
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

- [ ] **Step 2: Verify the edit compiles cleanly**

Confirm `src/app/middleware/csrf.go` still imports exactly `context`, `crypto/hmac`, `crypto/rand`, `encoding/hex`, `net/http`, `strings` (no new imports needed — `MethodHead`/`MethodOptions` are in `net/http`).

- [ ] **Step 3: Write the test** (`src/app/middleware/csrf_test.go`)

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// next is a trivial 200 handler used as the CSRF wrapper's downstream.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// A mutating request with NO csrf_token cookie is a native client: pass through.
// (Pre-fix, this 403'd — mutation-proves the fix.)
func TestCSRF_MutatingNoCookie_PassesThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	CSRF(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-cookie POST: want 200, got %d", rec.Code)
	}
}

// A browser (cookie present) with a wrong header is blocked.
func TestCSRF_MutatingCookieWrongHeader_Forbidden(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
	req.Header.Set("X-CSRF-Token", "wrong")
	CSRF(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cookie+wrong-header POST: want 403, got %d", rec.Code)
	}
}

// A browser (cookie present) with the matching header passes.
func TestCSRF_MutatingCookieMatchingHeader_Passes(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
	req.Header.Set("X-CSRF-Token", "abc123")
	CSRF(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie+matching-header POST: want 200, got %d", rec.Code)
	}
}

// A safe GET with no cookie sets the csrf_token cookie so browser JS can read it.
func TestCSRF_GetSetsCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	CSRF(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rec.Code)
	}
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrf_token" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("GET did not set a non-empty csrf_token cookie")
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `docker compose exec app go test ./middleware/...`
Expect: all four CSRF tests pass. Then `docker compose exec app go test ./...` green.

- [ ] **Step 5: Restart and live-verify**

Run: `docker compose restart app`
Check: `docker compose logs app` shows no errors. Then, from the host, prove the fix end-to-end — a mutating request with no `csrf_token` cookie succeeds (was 403). Example against any registered mutating route (adjust path to one that exists in this app; if the app has no mutating route yet, the unit test in Step 4 is the proof):

```bash
# no cookie, no header -> should NOT be 403 anymore (may 404/422 on payload, that's fine — just not 403)
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8080/api/v1/_probe_nonexistent
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "fix: CSRF enforces only for cookie-bearing browser clients

No-auth native clients bring no csrf_token cookie and cannot satisfy the
double-submit header check, so every mutation 403'd. Gate enforcement on
cookie presence; a browser forged cross-site POST still carries the cookie
and still fails without a matching header. Adds net-new csrf_test.go.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**1. Spec coverage:** Spec's single change (gate on cookie presence, stop synthesizing in the mutating branch, keep issuing on safe methods, keep Bearer/login_token fast-path) → Task 1 Step 1. Spec's 4 test cases → Task 1 Step 3 (all four present). ✅

**2. Placeholder scan:** No TBD/TODO. All code shown in full. The only conditional is the live-curl caveat in Step 5, which explicitly falls back to the Step 4 unit test as proof. ✅

**3. Naming consistency:** `CSRF`, `csrf_token`, `X-CSRF-Token`, `csrfKey`, `generateToken`, `secureCookies` all match `csrf.go` as read. ✅

**4. CRUD completeness:** N/A — no CRUD feature; infrastructure fix. ✅
