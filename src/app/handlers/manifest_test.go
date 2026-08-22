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

// THE SERVED MANIFEST MUST NOT DROP FIELDS THAT api.json CARRIES.
//
// Manifest is decoded from the file, so a key the struct does not declare is
// discarded in silence: api.json can be complete while /_manifest answers with
// holes, and nothing anywhere reports the difference. That is not hypothetical
// — `pages` was added to api.json and this struct was not updated, so every
// registered page was missing from the served contract while the file on disk
// listed all of them.
//
// Rather than asserting the fields we happen to remember, this decodes the
// REAL committed api.json generically and requires every top-level key to
// survive the round trip. A field added to the builder's manifest and
// forgotten here turns it red.
func TestManifestGET_DropsNoTopLevelField(t *testing.T) {
	raw, err := os.ReadFile("../api.json")
	if err != nil {
		t.Skipf("no committed api.json to check against: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("committed api.json is not valid JSON: %v", err)
	}

	served, err := json.Marshal(loadManifest("../api.json"))
	if err != nil {
		t.Fatalf("marshal served manifest: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(served, &out); err != nil {
		t.Fatalf("served manifest is not valid JSON: %v", err)
	}

	for key := range onDisk {
		if _, ok := out[key]; !ok {
			t.Errorf("api.json has top-level %q but the served manifest drops it — add it to the Manifest struct", key)
		}
	}
}

// The provenance stamp specifically: it is what tells an app which template
// built it, and it is useless if the endpoint does not carry it.
func TestManifestGET_CarriesTemplateStamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.json")
	body := `{"api_version":"1.0.0","hash":"sha256:abc",
	          "template":{"version":"2026-01-01.1","fingerprint":"sha256:def"},
	          "models":[],"endpoints":[],"pages":[]}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	m := loadManifest(path)
	if m.Template.Version != "2026-01-01.1" {
		t.Errorf("template version: got %q, want %q", m.Template.Version, "2026-01-01.1")
	}
	if m.Template.Fingerprint != "sha256:def" {
		t.Errorf("template fingerprint: got %q, want %q", m.Template.Fingerprint, "sha256:def")
	}
}
