package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// fallbackRouter mirrors how main.go wires the two fallbacks, with one
// registered API route to bounce a wrong method off. Going through a real chi
// mux is the point: calling the handlers directly would prove they format an
// envelope, not that chi ever reaches them.
func fallbackRouter() *chi.Mux {
	r := chi.NewRouter()
	r.NotFound(NotFoundHandler())
	r.MethodNotAllowed(MethodNotAllowedHandler())
	r.Get("/api/v1/things", func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, []string{})
	})
	r.Get("/things", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return r
}

func TestFallbacks(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantJSON   bool
		wantCode   string
	}{
		// An unknown path under /api/ is the typo case: a client decoding the
		// response must get the envelope it was promised, not text/plain.
		{"unknown API path", http.MethodGet, "/api/v1/nope", http.StatusNotFound, true, CodeNotFound},

		// A wrong method on a real API path. This is the scaffold_list case: a
		// creation form posting to a read-only resource used to get plain text
		// that api.js could not decode.
		{"wrong method on API path", http.MethodPost, "/api/v1/things", http.StatusMethodNotAllowed, true, CodeMethodNotAllowed},
		{"wrong method, unsafe verb", http.MethodDelete, "/api/v1/things", http.StatusMethodNotAllowed, true, CodeMethodNotAllowed},

		// Human-facing URLs keep the ordinary response a browser renders.
		// Answering a mistyped page navigation with a JSON body is the same
		// mistake as guarding a page with RequireAuth instead of
		// RequirePageAuth.
		{"unknown page path", http.MethodGet, "/nope", http.StatusNotFound, false, ""},
		{"wrong method on page path", http.MethodPost, "/things", http.StatusMethodNotAllowed, false, ""},

		// A path that merely starts with /api but is not the API namespace is
		// still a page as far as this split is concerned.
		{"apiary is not the API", http.MethodGet, "/apiary", http.StatusNotFound, false, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			fallbackRouter().ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))

			if rec.Code != c.wantStatus {
				t.Fatalf("%s %s: got %d, want %d", c.method, c.path, rec.Code, c.wantStatus)
			}
			ct := rec.Header().Get("Content-Type")
			if !c.wantJSON {
				if strings.Contains(ct, "application/json") {
					t.Errorf("%s %s: got JSON for a human-facing URL (%s)", c.method, c.path, ct)
				}
				return
			}
			if !strings.Contains(ct, "application/json") {
				t.Fatalf("%s %s: Content-Type is %q, want application/json", c.method, c.path, ct)
			}
			var env struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("%s %s: body is not JSON (%v): %s", c.method, c.path, err, rec.Body.String())
			}
			if env.OK {
				t.Errorf("%s %s: ok must be false", c.method, c.path)
			}
			if env.Code != c.wantCode {
				t.Errorf("%s %s: code = %q, want %q", c.method, c.path, env.Code, c.wantCode)
			}
			if env.Error == "" {
				t.Errorf("%s %s: error must be a non-empty string", c.method, c.path)
			}
		})
	}
}

// The two codes these fallbacks produce were unreachable through routing before
// they existed. Assert they are wired to the statuses codeForStatus maps them
// from, so the constant, the status and the fallback cannot drift apart.
func TestFallbackCodesMatchStatusMapping(t *testing.T) {
	if got := codeForStatus(http.StatusNotFound); got != CodeNotFound {
		t.Errorf("codeForStatus(404) = %q, want %q", got, CodeNotFound)
	}
	if got := codeForStatus(http.StatusMethodNotAllowed); got != CodeMethodNotAllowed {
		t.Errorf("codeForStatus(405) = %q, want %q", got, CodeMethodNotAllowed)
	}
}
