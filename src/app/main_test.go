package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// noDirIndex closes http.FileServer's directory listing. Everything under
// static/ is public by definition, so a listing is not a disclosure — it is
// free reconnaissance, and turning it off costs nothing.
func TestNoDirIndex(t *testing.T) {
	served := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for path, want := range map[string]int{
		"/js/":         http.StatusNotFound,
		"/":            http.StatusNotFound,
		"/js/api.js":   http.StatusOK,
		"/css/app.css": http.StatusOK,
	} {
		rec := httptest.NewRecorder()
		noDirIndex(served).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("%s: got %d, want %d", path, rec.Code, want)
		}
	}
}
