package handlers

import (
	"encoding/json"
	"net/http"
	"os"
)

const manifestPath = "./api.json"

// Manifest mirrors the builder's api.json shape. The app only reads it.
type Manifest struct {
	APIVersion  string     `json:"api_version"`
	Hash        string     `json:"hash"`
	GeneratedAt string     `json:"generated_at"`
	Models      []Model    `json:"models"`
	Endpoints   []Endpoint `json:"endpoints"`
}

type Model struct {
	Name   string       `json:"name"`
	Table  string       `json:"table"`
	Fields []ModelField `json:"fields"`
}

type ModelField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type Endpoint struct {
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Handler string   `json:"handler"`
	Deps    []string `json:"deps"`
	Auth    bool     `json:"auth"`
	Model   string   `json:"model,omitempty"`
	Kind    string   `json:"kind"`
}

// loadManifest reads and decodes the manifest. A missing or unreadable file
// yields the empty manifest — a fresh app has an empty but valid contract, not
// an error.
func loadManifest(path string) Manifest {
	empty := Manifest{APIVersion: "1.0.0", Models: []Model{}, Endpoints: []Endpoint{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return empty
	}
	if m.Models == nil {
		m.Models = []Model{}
	}
	if m.Endpoints == nil {
		m.Endpoints = []Endpoint{}
	}
	return m
}

// ManifestGET handles GET /api/v1/_manifest
func ManifestGET() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, loadManifest(manifestPath))
	}
}
