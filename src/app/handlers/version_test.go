package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/_version", nil)
	rec := httptest.NewRecorder()

	VersionGET()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			APIVersion       string `json:"api_version"`
			MinClientVersion string `json:"min_client_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if !body.OK {
		t.Error("ok: got false, want true")
	}
	if body.Data.APIVersion != APIVersion {
		t.Errorf("api_version: got %q, want %q", body.Data.APIVersion, APIVersion)
	}
	if body.Data.MinClientVersion != MinClientVersion {
		t.Errorf("min_client_version: got %q, want %q", body.Data.MinClientVersion, MinClientVersion)
	}
}

func TestVersionGET_IncludesManifestHash(t *testing.T) {
	// VersionGET reads "./api.json" relative to CWD via loadManifest. At
	// runtime the app's CWD is the app module root (/src/app); Chdir there
	// so the test matches.
	t.Chdir("..")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/_version", nil)
	rec := httptest.NewRecorder()
	VersionGET()(rec, req)

	var body struct {
		Data struct {
			ManifestHash string `json:"manifest_hash"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The committed api.json has a non-empty hash; VersionGET reads it.
	if body.Data.ManifestHash == "" {
		t.Error("version response missing manifest_hash")
	}
}
