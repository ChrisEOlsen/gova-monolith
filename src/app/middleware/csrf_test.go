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

// PATCH is verified exactly like POST.
//
// This is the test the old denylist (`POST || PUT || DELETE`) could not have:
// it passed against the bug because PATCH was never checked at all. Reverting
// isSafeMethod to that denylist turns this one red and leaves the four above
// green, which is the whole point of writing it separately.
func TestCSRF_MutatingPATCHCookieWrongHeader_Forbidden(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/things/1", nil)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
	req.Header.Set("X-CSRF-Token", "wrong")
	CSRF(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("PATCH cookie+wrong-header: want 403, got %d", rec.Code)
	}
}

// Every unsafe method the standard library names is verified — including ones
// nobody has written a handler for yet.
//
// The allowlist's value is that it covers methods this file does not enumerate,
// so enumerating them here would defeat the point of the test. What it CAN
// assert is that the set of methods let through unverified is exactly the three
// safe ones, which is a property a future edit cannot widen by accident.
func TestCSRF_OnlySafeMethodsSkipVerification(t *testing.T) {
	safe := map[string]bool{http.MethodGet: true, http.MethodHead: true, http.MethodOptions: true}
	all := []string{
		http.MethodGet, http.MethodHead, http.MethodOptions,
		http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodConnect, http.MethodTrace,
		"PURGE", "LOCK", // methods no RFC in this app defines — must still fail closed
	}
	for _, m := range all {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(m, "/api/v1/things", nil)
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc123"})
		req.Header.Set("X-CSRF-Token", "wrong")
		CSRF(okHandler()).ServeHTTP(rec, req)

		if safe[m] && rec.Code != http.StatusOK {
			t.Errorf("%s is safe: want 200, got %d", m, rec.Code)
		}
		if !safe[m] && rec.Code != http.StatusForbidden {
			t.Errorf("%s is unsafe: want 403 (verified), got %d", m, rec.Code)
		}
	}
}

// A request carrying a SESSION cookie and no csrf_token cookie is verified,
// not waved through.
//
// The no-cookie escape exists for native clients, which carry no session
// either. A browser holding a session but not a csrf_token — the state every
// browser restart used to produce, back when csrf_token had no MaxAge — is a
// browser riding an ambient credential and must fail closed. It cannot pass:
// `token` is minted fresh inside this request and the client has never seen it.
func TestCSRF_SessionCookieWithoutCSRFCookie_Forbidden(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "whatever|sig"})
	CSRF(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("session cookie, no csrf cookie: want 403, got %d", rec.Code)
	}

	// And it cannot be satisfied by echoing back the cookie the response set,
	// because a mutating request is not one the cookie is minted on.
	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrf_token" {
			t.Fatalf("a mutating request must not mint a csrf_token cookie")
		}
	}
}

// A native client — no session cookie, no csrf_token cookie — still passes.
// This is the case the whole no-cookie escape exists for, and the hasSession
// change must not have narrowed it.
func TestCSRF_NoCookiesAtAll_StillPassesThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/things/1", nil)
	CSRF(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("native PATCH with no cookies: want 200, got %d", rec.Code)
	}
}

// The csrf_token cookie outlives the browser session, so it cannot diverge from
// the session cookie on a restart and strand a legitimate browser in the
// fail-closed branch above.
func TestCSRF_MintedCookieIsPersistent(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	CSRF(okHandler()).ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrf_token" {
			if c.MaxAge <= 0 {
				t.Fatalf("csrf_token must be persistent: MaxAge = %d", c.MaxAge)
			}
			return
		}
	}
	t.Fatal("GET did not mint a csrf_token cookie")
}

// The bearer exemption is Pinned so a future edit cannot silently drop the
// mobile path into CSRF scope and 403 every native write: login_token (the
// issuing call, by path) and any Authorization: Bearer request pass even
// carrying cookies-session headers a browser would have attached. If you
// change this, reason through it in isBearerRequest's comment — the whole
// point of the named helper is that there is exactly one place to do that.
func TestCSRF_BearerExemptionSurivesAmbientCookies(t *testing.T) {
	// A request with a full ambient-cookie set, but a Bearer header: exempt.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/.anything", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "v|s"})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "abc"})
	req.Header.Set("X-CSRF-Token", "wrong") // would 403 without the exemption
	req.Header.Set("Authorization", "Bearer tok")
	CSRF(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer request with ambient cookies: want 200 (exempt), got %d", rec.Code)
	}

	// login_token by PATH, with the same cookies and no header at all.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login_token", nil)
	req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "v|s"})
	CSRF(okHandler()).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("login_token with session cookie: want 200 (path-exempt), got %d", rec2.Code)
	}

	// ...and the same path WITHOUT the exemption's conditions — a wrong-path
	// cookie bearing request — still verifies. The exemption must not leak
	// beyond its two named cases.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req3.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "v|s"})
	CSRF(okHandler()).ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusForbidden {
		t.Fatalf("login (not exempt) with session cookie and no token: want 403, got %d", rec3.Code)
	}
}
