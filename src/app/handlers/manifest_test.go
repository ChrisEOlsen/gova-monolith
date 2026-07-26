package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// writeTempManifestDir writes body to a temp dir's api.json and Chdir's the
// test into that dir, so ManifestGET's hardcoded "./api.json" read picks it
// up. Returns the temp dir.
func writeTempManifestDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "api.json")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatalf("write temp manifest: %v", err)
	}
	t.Chdir(dir)
	return dir
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
	// ManifestGET reads "./api.json" relative to CWD. At runtime the app's
	// CWD is the app module root (/src/app); Chdir there so the test matches
	// where the committed repo file actually lives.
	t.Chdir("..")
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

func TestManifestGET_ServesEnrichedContract(t *testing.T) {
	writeTempManifestDir(t, `{"api_version":"1.0.0","hash":"sha256:x","models":[
	  {"name":"reminder","table":"reminders","fields":[
	    {"name":"remind_at","type":"string","nullable":false,"format":"datetime-local"},
	    {"name":"category_id","type":"int","nullable":false,"references":"log_category"}]}],
	  "endpoints":[
	    {"method":"POST","path":"/api/v1/reminders","handler":"ReminderCreatePOST","deps":["read"],"auth":false,"kind":"create",
	     "request":{"shape":"object","fields":[{"name":"remind_at","type":"string","nullable":false,"format":"datetime-local"}]},
	     "response":{"shape":"object","model":"reminder"}}]}`)
	rec := httptest.NewRecorder()
	ManifestGET().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/_manifest", nil))
	body := rec.Body.String()
	for _, want := range []string{`"format":"datetime-local"`, `"references":"log_category"`, `"request"`, `"response"`} {
		if !strings.Contains(body, want) {
			t.Errorf("served manifest missing %s\nbody: %s", want, body)
		}
	}
}
