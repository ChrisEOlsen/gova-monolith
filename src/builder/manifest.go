package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	manifestFilePath = "/src/app/api.json"
	handlersDirPath  = "/src/app/handlers"
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

// callExpr builds the handler constructor call from the endpoint's deps, in
// argument order. read->database.Read, write->database.Write, cache->appCache.
func callExpr(e Endpoint) string {
	args := make([]string, 0, len(e.Deps))
	for _, d := range e.Deps {
		switch d {
		case "read":
			args = append(args, "database.Read")
		case "write":
			args = append(args, "database.Write")
		case "cache":
			args = append(args, "appCache")
		}
	}
	return e.Handler + "(" + strings.Join(args, ", ") + ")"
}

// chiMethod maps an HTTP method to the chi router method name (Get, Post, ...).
func chiMethod(method string) string {
	m := strings.ToLower(method)
	return strings.ToUpper(m[:1]) + m[1:]
}

func renderRoutes(m Manifest) (string, error) {
	m.canonicalize()
	usesAuth := false
	lines := make([]string, 0, len(m.Endpoints))
	for _, e := range m.Endpoints {
		call := callExpr(e)
		var line string
		if e.Auth {
			usesAuth = true
			line = fmt.Sprintf(`r.With(middleware.RequireAuth).%s(%q, %s)`, chiMethod(e.Method), e.Path, call)
		} else {
			line = fmt.Sprintf(`r.%s(%q, %s)`, chiMethod(e.Method), e.Path, call)
		}
		lines = append(lines, line)
	}
	data := struct {
		UsesAuth bool
		Lines    []string
	}{usesAuth, lines}
	return renderNamedToString("routes_gen.go.tmpl", data)
}

func regenerateRoutesAt(handlersDir string, m Manifest) error {
	out, err := renderRoutes(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(handlersDir, "routes_gen.go"), []byte(out), 0644)
}

// fieldsToModel converts Build 1 Field records (carrying schema-derived
// Nullable) into a manifest Model, adding the implicit id (first) and
// created_at (last) columns every generated table has.
func fieldsToModel(name, table string, fields []Field) Model {
	out := make([]ModelField, 0, len(fields)+2)
	out = append(out, ModelField{Name: "id", Type: "int", Nullable: false})
	for _, f := range fields {
		typ := f.Type
		if typ == "password" {
			typ = "string"
		}
		out = append(out, ModelField{Name: f.Name, Type: typ, Nullable: f.Nullable})
	}
	out = append(out, ModelField{Name: "created_at", Type: "timestamp", Nullable: false})
	return Model{Name: name, Table: table, Fields: out}
}

// updateManifestAt is the transactional core: read, upsert all, and only if
// every upsert succeeded, write api.json and regenerate routes_gen.go. A
// conflict returns before any file is touched.
func updateManifestAt(apiPath, handlersDir string, now time.Time, models []Model, endpoints []Endpoint) error {
	m, err := readManifestAt(apiPath)
	if err != nil {
		return err
	}
	for _, model := range models {
		m.UpsertModel(model)
	}
	for _, e := range endpoints {
		if err := m.UpsertEndpoint(e); err != nil {
			return err // conflict — nothing written yet
		}
	}
	if err := writeManifestAt(apiPath, &m, now); err != nil {
		return err
	}
	return regenerateRoutesAt(handlersDir, m)
}
