package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// epochStub is an EpochStore returning a fixed epoch.
type epochStub struct {
	epoch   int64
	lookups int
}

func (e *epochStub) SessionEpoch(int64) int64 {
	e.lookups++
	return e.epoch
}

func testKey(t *testing.T) {
	t.Helper()
	t.Setenv("SESSION_SECRET", strings.Repeat("k", 64))
}

// mintCookie returns a well-formed session cookie value via the real signing
// path, so the MAC verifies without the test hand-rolling base64.
func mintCookie(t *testing.T, uid, epoch int64) string {
	t.Helper()
	testKey(t)
	w := httptest.NewRecorder()
	SetSession(w, uid, epoch)
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookieName {
			return c.Value
		}
	}
	t.Fatal("no session cookie minted")
	return ""
}

// authResult runs a probe behind Auth. 200 means an identity was set, 418
// means Auth passed the request through anonymously.
func authResult(t *testing.T, store EpochStore, cookieVal string) int {
	t.Helper()
	h := Auth(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserID(r) == 0 {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	if cookieVal != "" {
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookieVal})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestAuth_CurrentEpochPasses(t *testing.T) {
	store := &epochStub{epoch: 3}
	if code := authResult(t, store, mintCookie(t, 7, 3)); code != http.StatusOK {
		t.Fatalf("current-epoch session: want 200, got %d", code)
	}
	if store.lookups == 0 {
		t.Error("epoch store was never consulted")
	}
}

// The revocation property: a cookie minted before a bump is dead after it.
func TestAuth_StaleEpochRejected(t *testing.T) {
	if code := authResult(t, &epochStub{epoch: 4}, mintCookie(t, 7, 2)); code != http.StatusTeapot {
		t.Fatalf("stale-epoch session must be rejected, got %d", code)
	}
}

// The check is "not behind", so clock or ordering skew between mint and lookup
// cannot lock a legitimate user out. An attacker gains nothing by forging an
// epoch ahead — the cookie still needs our signature.
func TestAuth_AheadEpochPasses(t *testing.T) {
	if code := authResult(t, &epochStub{epoch: 2}, mintCookie(t, 7, 5)); code != http.StatusOK {
		t.Fatalf("ahead-of-epoch cookie: want 200, got %d", code)
	}
}

func TestAuth_NoCookieIsAnonymous(t *testing.T) {
	testKey(t)
	if code := authResult(t, &epochStub{}, ""); code != http.StatusTeapot {
		t.Fatalf("no cookie: want anonymous, got %d", code)
	}
}

func TestAuth_TamperedSignatureRejected(t *testing.T) {
	val := mintCookie(t, 7, 0)
	payload, _, _ := strings.Cut(val, "|")
	if code := authResult(t, &epochStub{}, payload+"|deadbeef"); code != http.StatusTeapot {
		t.Fatalf("forged signature must be rejected, got %d", code)
	}
}

// A payload edited to name another user invalidates the MAC.
func TestAuth_TamperedPayloadRejected(t *testing.T) {
	val := mintCookie(t, 7, 0)
	_, sig, _ := strings.Cut(val, "|")
	other := mintCookie(t, 99, 0)
	otherPayload, _, _ := strings.Cut(other, "|")
	if code := authResult(t, &epochStub{}, otherPayload+"|"+sig); code != http.StatusTeapot {
		t.Fatalf("swapped payload must be rejected, got %d", code)
	}
}

func TestAuth_MalformedCookieRejected(t *testing.T) {
	testKey(t)
	for _, val := range []string{"", "nopipe", "|", "not-base64|sig"} {
		if code := authResult(t, &epochStub{}, val); code != http.StatusTeapot {
			t.Errorf("malformed cookie %q: want anonymous, got %d", val, code)
		}
	}
}

// A deletion cookie must carry the same attributes as the one it replaces —
// some browsers scope evictions by attribute set.
func TestClearSession_CarriesMintingAttributes(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSession(w)
	var got *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookieName {
			got = c
		}
	}
	if got == nil {
		t.Fatal("ClearSession minted no deletion cookie")
	}
	if got.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative", got.MaxAge)
	}
	if !got.HttpOnly {
		t.Error("lost HttpOnly")
	}
	if got.SameSite != http.SameSiteStrictMode {
		t.Error("lost SameSite=Strict")
	}
}

func TestSessionSecret_PanicsWhenTooShort(t *testing.T) {
	t.Setenv("SESSION_SECRET", "short")
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for a short SESSION_SECRET")
		}
	}()
	sessionSecret()
}

func TestRequireAuth_UnauthenticatedGets401JSON(t *testing.T) {
	rec := httptest.NewRecorder()
	RequireAuth(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("want JSON content type, got %q", ct)
	}
}

func TestRequirePageAuth_UnauthenticatedRedirects(t *testing.T) {
	rec := httptest.NewRecorder()
	RequirePageAuth(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("want redirect to /login, got %q", loc)
	}
}
