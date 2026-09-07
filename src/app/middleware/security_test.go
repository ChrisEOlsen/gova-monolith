package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	Security(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"X-Frame-Options":        "SAMEORIGIN",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s: got %q, want %q", header, got, want)
		}
	}

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy — the XSS rules in CLAUDE.md have no backstop")
	}
	// The directives that actually stop an injected script from running. A
	// future edit that widens script-src to 'unsafe-inline', or drops the two
	// bypass-closers, should turn this red rather than pass quietly.
	for _, required := range []string{
		"script-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"default-src 'self'",
	} {
		if !strings.Contains(csp, required) {
			t.Errorf("CSP is missing %q: %s", required, csp)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP allows unsafe inline/eval, which defeats the point: %s", csp)
	}
}

// HSTS is sent on every response, plain HTTP included: browsers ignore it
// there, so there is nothing to gate on APP_ENV and no dev/prod branch that can
// be wrong.
func TestSecurity_SetsHSTS(t *testing.T) {
	rec := httptest.NewRecorder()
	Security(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	got := rec.Header().Get("Strict-Transport-Security")
	if got == "" {
		t.Fatal("no Strict-Transport-Security header")
	}
	if !strings.Contains(got, "max-age=") || !strings.Contains(got, "includeSubDomains") {
		t.Errorf("HSTS = %q, want a max-age with includeSubDomains", got)
	}
	if strings.Contains(got, "preload") {
		t.Error("HSTS must not claim preload — that is a submission a template cannot make for a downstream app")
	}
}
