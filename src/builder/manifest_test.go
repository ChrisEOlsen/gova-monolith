package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedTime() time.Time { return time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC) }

func sampleModel() Model {
	return Model{Name: "project", Table: "projects", Fields: []ModelField{
		{Name: "id", Type: "int", Nullable: false},
		{Name: "notes", Type: "string", Nullable: true},
	}}
}

func sampleEndpoint() Endpoint {
	return Endpoint{Method: "GET", Path: "/api/v1/projects", Handler: "ProjectListGET",
		Deps: []string{"read", "write", "cache"}, Auth: false, Model: "project", Kind: "list"}
}

func TestReadManifest_MissingFileIsEmpty(t *testing.T) {
	m, err := readManifestAt(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("readManifestAt(missing): %v", err)
	}
	if m.APIVersion != "1.0.0" || len(m.Models) != 0 || len(m.Endpoints) != 0 {
		t.Errorf("missing file should be empty manifest, got %+v", m)
	}
}

func TestUpsertModel_AddsThenReplaces(t *testing.T) {
	var m Manifest
	m.UpsertModel(sampleModel())
	if len(m.Models) != 1 {
		t.Fatalf("after add: got %d models, want 1", len(m.Models))
	}
	updated := sampleModel()
	updated.Fields = append(updated.Fields, ModelField{Name: "status", Type: "string"})
	m.UpsertModel(updated)
	if len(m.Models) != 1 {
		t.Fatalf("after replace: got %d models, want 1 (no duplicate)", len(m.Models))
	}
	if len(m.Models[0].Fields) != 3 {
		t.Errorf("replace did not take: got %d fields, want 3", len(m.Models[0].Fields))
	}
}

func TestUpsertEndpoint_AddsThenReplaces(t *testing.T) {
	var m Manifest
	if err := m.UpsertEndpoint(sampleEndpoint()); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := m.UpsertEndpoint(sampleEndpoint()); err != nil {
		t.Fatalf("re-add same handler (idempotent) should not error: %v", err)
	}
	if len(m.Endpoints) != 1 {
		t.Fatalf("re-adding same endpoint duplicated it: got %d", len(m.Endpoints))
	}
}

func TestUpsertEndpoint_ConflictErrors(t *testing.T) {
	var m Manifest
	if err := m.UpsertEndpoint(sampleEndpoint()); err != nil {
		t.Fatalf("add: %v", err)
	}
	conflict := sampleEndpoint()
	conflict.Handler = "SomethingElseGET"
	err := m.UpsertEndpoint(conflict)
	if err == nil {
		t.Fatal("expected conflict error for same (method,path) different handler")
	}
	if !strings.Contains(err.Error(), "/api/v1/projects") {
		t.Errorf("conflict error should name the path: %v", err)
	}
	if len(m.Endpoints) != 1 || m.Endpoints[0].Handler != "ProjectListGET" {
		t.Errorf("conflict must not mutate the manifest, got %+v", m.Endpoints)
	}
}

func TestHash_StableAndSensitive(t *testing.T) {
	var a Manifest
	a.UpsertModel(sampleModel())
	_ = a.UpsertEndpoint(sampleEndpoint())
	a.canonicalize()
	h1 := a.Hash

	// Re-canonicalizing identical content yields the same hash.
	a.canonicalize()
	if a.Hash != h1 {
		t.Errorf("hash not stable across no-op canonicalize: %s vs %s", h1, a.Hash)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("hash missing sha256: prefix: %s", h1)
	}

	// Adding an endpoint changes the hash.
	var b Manifest
	b.UpsertModel(sampleModel())
	_ = b.UpsertEndpoint(sampleEndpoint())
	e2 := sampleEndpoint()
	e2.Method, e2.Handler, e2.Kind = "POST", "ProjectCreatePOST", "create"
	_ = b.UpsertEndpoint(e2)
	b.canonicalize()
	if b.Hash == h1 {
		t.Error("hash did not change after adding an endpoint")
	}
}

func TestHash_OrderIndependent(t *testing.T) {
	e2 := sampleEndpoint()
	e2.Method, e2.Path, e2.Handler = "POST", "/api/v1/projects", "ProjectCreatePOST"

	var a Manifest
	_ = a.UpsertEndpoint(sampleEndpoint())
	_ = a.UpsertEndpoint(e2)
	a.canonicalize()

	var b Manifest
	_ = b.UpsertEndpoint(e2)
	_ = b.UpsertEndpoint(sampleEndpoint())
	b.canonicalize()

	if a.Hash != b.Hash {
		t.Errorf("hash depends on insertion order: %s vs %s", a.Hash, b.Hash)
	}
}

func TestWriteThenRead_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.json")
	var m Manifest
	m.APIVersion = "1.0.0"
	m.UpsertModel(sampleModel())
	_ = m.UpsertEndpoint(sampleEndpoint())
	if err := writeManifestAt(path, &m, fixedTime()); err != nil {
		t.Fatalf("write: %v", err)
	}
	back, err := readManifestAt(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if back.Hash != m.Hash || len(back.Models) != 1 || len(back.Endpoints) != 1 {
		t.Errorf("round-trip mismatch: %+v", back)
	}
	if back.GeneratedAt != "2026-07-23T00:00:00Z" {
		t.Errorf("generated_at: got %q", back.GeneratedAt)
	}
}
