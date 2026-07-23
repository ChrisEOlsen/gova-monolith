package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

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

// readManifestAt loads a manifest. A missing file is not an error — it is the
// empty manifest, which is the correct state for an app that has scaffolded
// nothing yet.
func readManifestAt(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Manifest{APIVersion: "1.0.0", Models: []Model{}, Endpoints: []Endpoint{}}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("api.json is corrupt: %w", err)
	}
	if m.APIVersion == "" {
		m.APIVersion = "1.0.0"
	}
	return m, nil
}

func (m *Manifest) UpsertModel(model Model) {
	for i := range m.Models {
		if m.Models[i].Name == model.Name {
			m.Models[i] = model
			return
		}
	}
	m.Models = append(m.Models, model)
}

// UpsertEndpoint replaces a same-key endpoint or appends. A same (method,path)
// naming a different handler is a conflict: two scaffolds claiming one route.
// It errors and leaves the manifest untouched.
func (m *Manifest) UpsertEndpoint(e Endpoint) error {
	for i := range m.Endpoints {
		if m.Endpoints[i].Method == e.Method && m.Endpoints[i].Path == e.Path {
			if m.Endpoints[i].Handler != e.Handler {
				return fmt.Errorf("route conflict: %s %s is already registered by handler %q, cannot reassign to %q",
					e.Method, e.Path, m.Endpoints[i].Handler, e.Handler)
			}
			m.Endpoints[i] = e
			return nil
		}
	}
	m.Endpoints = append(m.Endpoints, e)
	return nil
}

func (m *Manifest) canonicalize() {
	sort.Slice(m.Models, func(i, j int) bool { return m.Models[i].Name < m.Models[j].Name })
	sort.Slice(m.Endpoints, func(i, j int) bool {
		if m.Endpoints[i].Path != m.Endpoints[j].Path {
			return m.Endpoints[i].Path < m.Endpoints[j].Path
		}
		return m.Endpoints[i].Method < m.Endpoints[j].Method
	})
	m.Hash = manifestHash(*m)
}

// manifestHash is sha256 over just the models and endpoints (sorted by
// canonicalize before this is called), excluding generated_at so an
// otherwise-identical manifest always hashes the same.
func manifestHash(m Manifest) string {
	payload := struct {
		Models    []Model    `json:"models"`
		Endpoints []Endpoint `json:"endpoints"`
	}{m.Models, m.Endpoints}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeManifestAt(path string, m *Manifest, now time.Time) error {
	if m.APIVersion == "" {
		m.APIVersion = "1.0.0"
	}
	m.canonicalize()
	m.GeneratedAt = now.UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
