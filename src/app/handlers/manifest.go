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
	Template    Template   `json:"template"`
	Models      []Model    `json:"models"`
	Endpoints   []Endpoint `json:"endpoints"`
	Pages       []Page     `json:"pages"`
}

// Template mirrors the builder's provenance stamp: which build of the generator
// wrote this manifest.
//
// This struct is decoded from api.json, so ANY field missing here is silently
// dropped from what /_manifest serves — the file on disk can be complete while
// the endpoint answers with holes, and nothing reports a mismatch. That already
// happened once: `pages` was added to api.json and this struct was not updated,
// so the served manifest omitted every page while the file listed them all.
// Keep this in step with the builder's Manifest.
type Template struct {
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

// Page mirrors the builder's page record — a human-facing HTML route. The app
// only reads it; handlers/pages_gen.go is what actually mounts these.
type Page struct {
	Path  string `json:"path"`
	File  string `json:"file"`
	Title string `json:"title,omitempty"`
	Auth  bool   `json:"auth"`
}

type Model struct {
	Name   string       `json:"name"`
	Table  string       `json:"table"`
	Fields []ModelField `json:"fields"`
}

type ModelField struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	Format     string `json:"format,omitempty"`
	References string `json:"references,omitempty"`
}

// BodySchema mirrors the builder's request/response schema shape. Read-only.
type BodySchema struct {
	Shape  string       `json:"shape"`
	Model  string       `json:"model,omitempty"`
	Fields []ModelField `json:"fields,omitempty"`
}

type Endpoint struct {
	Method   string      `json:"method"`
	Path     string      `json:"path"`
	Handler  string      `json:"handler"`
	Deps     []string    `json:"deps"`
	Auth     bool        `json:"auth"`
	Model    string      `json:"model,omitempty"`
	Kind     string      `json:"kind"`
	Summary  string      `json:"summary,omitempty"`
	Request  *BodySchema `json:"request,omitempty"`
	Response *BodySchema `json:"response,omitempty"`
}

// loadManifest reads and decodes the manifest. A missing or unreadable file
// yields the empty manifest — a fresh app has an empty but valid contract, not
// an error.
func loadManifest(path string) Manifest {
	empty := Manifest{APIVersion: "1.0.0", Models: []Model{}, Endpoints: []Endpoint{}, Pages: []Page{}}
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
	if m.Pages == nil {
		m.Pages = []Page{}
	}
	return m
}

// ManifestGET handles GET /api/v1/_manifest
func ManifestGET() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, loadManifest(manifestPath))
	}
}
