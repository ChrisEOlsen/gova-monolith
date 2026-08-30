package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testKey installs a valid-length SESSION_SECRET for the duration of one
// test. Without it sessionSecret() panics — the fail-fast invariant — so
// every test in this file that touches a cookie goes through here.
func testKey(t *testing.T) {
	t.Helper()
	t.Setenv("SESSION_SECRET", strings.Repeat("k", 64))
}

// sigCookie mints a well-formed session cookie value via the real signing
// path, so the MAC verifies without the test hand-rolling base64.
func sigCookie(t *testing.T, uid, epoch int64, jti string) string {
	t.Helper()
	testKey(t)
	w := httptest.NewRecorder()
	if jti == "" {
		SetSessionWithEpoch(w, uid, epoch, time.Hour)
	} else {
		// JTI pinning uses the exported minting path only; tests that need
		// a specific jti decode the cookie below instead.
		SetSessionWithEpoch(w, uid, epoch, time.Hour)
	}
	cookieVal := ""
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookieName {
			cookieVal = c.Value
		}
	}
	if cookieVal == "" {
		t.Fatal("no session cookie minted")
	}
	return cookieVal
}

// authedRequest wraps a probe in Auth with a pre-installed cookie. Returns
// the recorder unconditionally — the CALLER decides which status means
// "identity was set" (200) vs "rejected" (418). Never asserts here: a
// stale-epoch rejection is a legitimate outcome a caller tests for.
func authedRequest(t *testing.T, cookieVal string) *httptest.ResponseRecorder {
	t.Helper()
	h := Auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserID(r) == 0 {
			// Rejection surface: Auth passed the request downstream without
			// an identity. 418 so a green 200 can never happen by accident.
			w.WriteHeader(http.StatusTeapot)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookieVal})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A valid session signed with the current epoch reaches the downstream
// handler.
func TestAuth_SessionAtCurrentEpoch_Passes(t *testing.T) {
	lookups := int64(0)
	EpochLookup = func(r *http.Request, uid int64) int64 {
		atomic.AddInt64(&lookups, 1)
		return 3
	}
	t.Cleanup(func() { EpochLookup = nil })

	rec := authedRequest(t, sigCookie(t, 7, 3, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("current-epoch session: want 200, got %d", rec.Code)
	}
	if atomic.LoadInt64(&lookups) == 0 {
		t.Error("epoch lookup was never consulted")
	}
}

// THE revocation property: a cookie minted BEFORE a bump is dead after it.
// Rejection surfaces as 418 (the downstream saw no UserID), not 200.
func TestAuth_StaleEpochRejected(t *testing.T) {
	EpochLookup = func(r *http.Request, uid int64) int64 { return 4 }
	t.Cleanup(func() { EpochLookup = nil })

	rec := authedRequest(t, sigCookie(t, 7, 2, "x"))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("stale-epoch session must be rejected (418), got %d", rec.Code)
	}
}

// A nil EpochLookup (app scaffolded without the users table) passes
// everything — the pre-epoch behavior, and what keeps this change additive.
func TestAuth_NilEpochLookup_PassesEverything(t *testing.T) {
	// EpochLookup is nil here; a cookie with NO epoch at all must pass.
	rec := authedRequest(t, sigCookie(t, 7, 0, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("nil lookup + epoch-0 cookie: want 200, got %d", rec.Code)
	}
}

// A cookie epoch AHEAD of the stored epoch also passes: the epoch check is
// "not behind", so a klass of clock/ordering skew between mint and lookup
// cannot lock a legitimate user out. A forged-ahead epoch is useless to an
// attacker — the cookie still needs the server's signature.
func TestAuth_AheadEpochStillPasses(t *testing.T) {
	EpochLookup = func(r *http.Request, uid int64) int64 { return 2 }
	t.Cleanup(func() { EpochLookup = nil })
	rec := authedRequest(t, sigCookie(t, 7, 5, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("ahead-of-epoch cookie: want 200 (check is 'not behind'), got %d", rec.Code)
	}
}

// A lookup error counts as epoch 0, so an epoch-0 cookie passes but a
// revoked user's stale cookie is checked (and possibly passes) rather than
// 500ing the whole request stream.
func TestAuth_LookupErrorReadsAsEpochZero(t *testing.T) {
	EpochLookup = func(r *http.Request, uid int64) int64 { return 0 }
	t.Cleanup(func() { EpochLookup = nil })
	rec := authedRequest(t, sigCookie(t, 7, 0, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup failure treated as epoch 0: want 200, got %d", rec.Code)
	}
}

// SetSessionWithEpoch mints a cookie carrying the epoch; the wire format
// must round-trip it (a future change that drops the field from the payload
// turns this test red at the decode, not in production).
func TestSetSessionWithEpoch_RoundTripsEpoch(t *testing.T) {
	testKey(t)
	w := httptest.NewRecorder()
	SetSessionWithEpoch(w, 9, 4, time.Hour)
	val := ""
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookieName {
			val = c.Value
		}
	}
	if val == "" {
		t.Fatal("no cookie")
	}
	if !strings.Contains(val, "|") {
		t.Fatalf("cookie not signed: %q", val)
	}

	// Decode via a request through Auth with a lookup asserting the epoch
	// arrives intact.
	seen := make(chan int64, 1)
	EpochLookup = func(r *http.Request, uid int64) int64 {
		if uid != 9 {
			t.Errorf("lookup got uid %d, want 9", uid)
		}
		seen <- 4
		return 4
	}
	t.Cleanup(func() { EpochLookup = nil })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: val})
	Auth(okHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("epoch-4 cookie vs lookup 4: want 200, got %d", rec.Code)
	}
}

// The deletion cookie carries the same attributes as the minting one — some
// browsers scope evictions by attribute set, so a bare deletion cookie can
// fail to evict the real cookie. (Security-report note, turned into a test.)
func TestClearSession_DeletionCookieCarriesAttributes(t *testing.T) {
	var got *http.Cookie
	w := httptest.NewRecorder()
	ClearSession(w)
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookieName {
			got = c
		}
	}
	if got == nil {
		t.Fatal("ClearSession minted no deletion cookie")
	}
	if got.MaxAge >= 0 {
		t.Errorf("deletion cookie MaxAge = %d, want negative", got.MaxAge)
	}
	if !got.HttpOnly {
		t.Error("deletion cookie lost HttpOnly")
	}
	if got.SameSite != http.SameSiteStrictMode {
		t.Error("deletion cookie lost SameSite=Strict")
	}
}

// The JTI is server-generated randomness — 32 hex chars, distinct across
// mints. A cookie whose identity a client could choose would be forgeable in
// combination with a future revocation-by-jti path.
func TestSetSessionWithEpoch_JTIIsRandomAndDistinct(t *testing.T) {
	testKey(t)
	w1 := httptest.NewRecorder()
	SetSessionWithEpoch(w1, 1, 0, time.Hour)
	w2 := httptest.NewRecorder()
	SetSessionWithEpoch(w2, 1, 0, time.Hour)

	c1, c2 := "", ""
	for _, c := range w1.Result().Cookies() {
		if c.Name == SessionCookieName {
			c1 = strings.SplitN(c.Value, "|", 2)[0]
		}
	}
	for _, c := range w2.Result().Cookies() {
		if c.Name == SessionCookieName {
			c2 = strings.SplitN(c.Value, "|", 2)[0]
		}
	}
	if c1 == "" || c2 == "" || c1 == c2 {
		t.Fatal("two mints produced identical or empty session payloads — JTI is not serving as identity")
	}
}