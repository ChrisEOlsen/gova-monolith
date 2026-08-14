package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// RegisterPages must be safe to call whatever the page set is, and must never
// install a catch-all — the app has to boot before anything is scaffolded, and
// an unregistered URL has to keep 404ing after it is.
func TestRegisterPages_AddsNoCatchAll(t *testing.T) {
	r := chi.NewRouter()
	RegisterPages(r)

	req := httptest.NewRequest(http.MethodGet, "/definitely-not-a-page", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unregistered page, got %d", rec.Code)
	}
}

// TestPageFile_ServesOnlyFromStaticPages is the traversal guard.
//
// pageFile's argument is always a literal from the generated table, never
// request input, so this is defence in depth rather than the primary control.
// It asserts the second guard actually holds: a name carrying separators or
// ".." collapses to its last element and cannot reach a file outside
// static/pages.
func TestPageFile_ServesOnlyFromStaticPages(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "static", "pages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "static", "pages", "widgets.html"),
		[]byte("<h1>widgets</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	// A file that exists OUTSIDE static/pages, named so that a traversing
	// argument would resolve onto it if filepath.Base were absent.
	if err := os.WriteFile(filepath.Join(dir, "secrets.html"), []byte("TOP SECRET"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	// Happy path: a registered page serves its shell.
	rec := httptest.NewRecorder()
	pageFile("widgets")(rec, httptest.NewRequest(http.MethodGet, "/widgets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("registered page: got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<h1>widgets</h1>") {
		t.Errorf("registered page served the wrong file: %q", rec.Body.String())
	}

	// Traversal: every one of these must collapse inside static/pages.
	for _, name := range []string{
		"../secrets",
		"../../secrets",
		"foo/../../secrets",
		"/etc/passwd",
	} {
		rec := httptest.NewRecorder()
		pageFile(name)(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
		if strings.Contains(rec.Body.String(), "TOP SECRET") {
			t.Errorf("pageFile(%q) escaped static/pages and served %q", name, rec.Body.String())
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("pageFile(%q): got %d, want 404 (nothing by that base name in static/pages)", name, rec.Code)
		}
	}

	// The 404s above would also happen if pageFile were simply broken, so pin
	// that filepath.Base is what produced them: a traversing name whose LAST
	// element does exist inside static/pages must collapse onto it and serve.
	// That can only happen if the guard actually ran.
	rec = httptest.NewRecorder()
	pageFile("../../../widgets")(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<h1>widgets</h1>") {
		t.Errorf("filepath.Base guard was not reached: pageFile(%q) gave %d / %q, want 200 serving static/pages/widgets.html",
			"../../../widgets", rec.Code, rec.Body.String())
	}
}

// TestPageRoute_RequestPathCannotTraverse is the URL side of the same question,
// through a real chi router.
//
// chi routes on the literal request path without cleaning it, so a traversing
// URL matches no page route and pageFile is never invoked — there is no path
// from request input to the filesystem at all. This asserts that holds for raw
// and percent-encoded forms alike.
func TestPageRoute_RequestPathCannotTraverse(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "static", "pages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "static", "pages", "widgets.html"),
		[]byte("<h1>widgets</h1>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.html"), []byte("TOP SECRET"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	r := chi.NewRouter()
	r.Get("/widgets", pageFile("widgets"))

	// Sanity: the route itself works, so a 404 below means "no match", not
	// "nothing is mounted".
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/widgets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("fixture route is broken: GET /widgets gave %d", rec.Code)
	}

	for _, target := range []string{
		"/../secrets.html",
		"/widgets/../../secrets.html",
		"/%2e%2e%2fsecrets.html",
		"/..%2fsecrets.html",
		"/widgets/..%2f..%2fsecrets.html",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if strings.Contains(rec.Body.String(), "TOP SECRET") {
			t.Errorf("GET %s escaped the pages directory", target)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: got %d, want 404 — a traversing path must match no page route", target, rec.Code)
		}
	}
}
