package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeTempManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "api.json")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatalf("write temp manifest: %v", err)
	}
	return p
}

func TestLoadManifest_Present(t *testing.T) {
	p := writeTempManifest(t, `{"api_version":"1.0.0","hash":"sha256:abc",
		"models":[{"name":"project","table":"projects","fields":[
			{"name":"notes","type":"string","nullable":true}]}],
		"endpoints":[{"method":"GET","path":"/api/v1/projects","handler":"ProjectListGET",
			"deps":["read","write","cache"],"auth":false,"model":"project","kind":"list"}]}`)
	m := loadManifest(p)
	if m.Hash != "sha256:abc" || len(m.Models) != 1 || len(m.Endpoints) != 1 {
		t.Fatalf("load mismatch: %+v", m)
	}
	if !m.Models[0].Fields[0].Nullable {
		t.Error("nullable field lost in decode")
	}
}

func TestLoadManifest_MissingIsEmpty(t *testing.T) {
	m := loadManifest(filepath.Join(t.TempDir(), "absent.json"))
	if m.APIVersion != "1.0.0" || len(m.Models) != 0 || len(m.Endpoints) != 0 {
		t.Errorf("absent manifest should be empty, got %+v", m)
	}
}

func TestManifestGET_ServesEnvelope(t *testing.T) {
	// ManifestGET reads "./api.json" relative to CWD; the committed repo file
	// exists at the app module root, which is the test's working dir.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/_manifest", nil)
	rec := httptest.NewRecorder()
	ManifestGET()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			APIVersion string `json:"api_version"`
			Models     []any  `json:"models"`
			Endpoints  []any  `json:"endpoints"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if !body.OK || body.Data.APIVersion == "" {
		t.Errorf("unexpected manifest envelope: %s", rec.Body.String())
	}
}
