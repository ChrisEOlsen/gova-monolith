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
