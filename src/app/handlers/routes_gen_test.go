package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"gova/app/cache"
	"gova/app/db"
)

// RegisterGenerated must be safe to call with the committed empty route set —
// the app has to boot before anything is scaffolded.
func TestRegisterGenerated_EmptyIsSafe(t *testing.T) {
	r := chi.NewRouter()
	database := &db.DB{}
	RegisterGenerated(r, database, cache.New())

	// An unregistered path 404s through chi — proves the router is functional
	// and RegisterGenerated added no catch-all.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nothing", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unregistered route, got %d", rec.Code)
	}
}
