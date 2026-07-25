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
