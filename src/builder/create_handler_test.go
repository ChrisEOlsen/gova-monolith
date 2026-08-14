package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCustomEndpointSchemaRoundtrip(t *testing.T) {
	dir := t.TempDir()
	api := filepath.Join(dir, "api.json")
	handlers := filepath.Join(dir, "handlers")
	os.MkdirAll(handlers, 0755)

	reqS, err := parseBodySchemaArg(`{"shape":"object","fields":[{"name":"note","type":"string"}]}`)
	if err != nil {
		t.Fatalf("parse request: %v", err)
	}
	respS, _ := parseBodySchemaArg(`{"shape":"object","fields":[{"name":"ok","type":"boolean"}]}`)
	ep := Endpoint{Method: "POST", Path: "/api/v1/todos/{id}/archive", Handler: "TodoArchivePOST",
		Deps: []string{"read", "write", "cache"}, Kind: "custom", Summary: "Archive a todo",
		Request: reqS, Response: respS}

	if err := updateManifestAt(api, handlers, time.Unix(0, 0).UTC(), nil, []Endpoint{ep}, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	m, _ := readManifestAt(api)
	if m.Endpoints[0].Summary != "Archive a todo" || m.Endpoints[0].Request == nil || m.Endpoints[0].Response.Fields[0].Name != "ok" {
		t.Errorf("custom endpoint lost schema/summary: %+v", m.Endpoints[0])
	}
}

func TestParseBodySchemaArg_Errors(t *testing.T) {
	if _, err := parseBodySchemaArg(""); err != nil {
		t.Errorf("empty should be nil,nil: %v", err)
	}
	if _, err := parseBodySchemaArg(`{bad json`); err == nil {
		t.Error("malformed JSON should error")
	}
	if _, err := parseBodySchemaArg(`{"shape":"weird"}`); err == nil {
		t.Error("unknown shape should error")
	}
}
