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
	Template    Template   `json:"template"`
	Models      []Model    `json:"models"`
	Endpoints   []Endpoint `json:"endpoints"`
	Pages       []Page     `json:"pages"`
}

// Template records which build of the generator wrote this manifest.
//
// It is PROVENANCE, not surface, so it is deliberately outside manifestHash:
// bumping the template must not look like an API change to a client watching
// that hash. See version.go for why an app needs to be able to answer this at
// all.
type Template struct {
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

// Page is a human-facing HTML route: a URL a person types or clicks, serving a
// static shell out of static/pages. Pages are kept in their own table rather
// than mixed into Endpoints because they are not part of the API surface — they
// have no method beyond GET, no request or response body, and no deps, and a
// native client reading this manifest wants nothing to do with them.
//
// File is the shell's base name under static/pages (no extension) and is the
// only thing that reaches the filesystem — never a value from a request.
type Page struct {
	Path  string `json:"path"`
	File  string `json:"file"`
	Title string `json:"title,omitempty"`
	// Auth records that this page's JS module calls requireAuth() on load. It
	// is declarative metadata only: the page shell holds no data, so it is not
	// wrapped server-side. Answering a browser navigation with a JSON 401 body
	// would be worse than letting the module redirect, and the data behind the
	// page is protected on its own /api/v1/ endpoints.
	Auth bool `json:"auth"`
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
	Format     string `json:"format,omitempty"`     // semantic hint: datetime-local, date, time, json, email
	References string `json:"references,omitempty"` // FK target model name
}

// BodySchema is the closed shape describing an endpoint's request or response
// body. Either Model (fields inherited from that model) or Fields (inline) is
// set, never both for a given schema.
type BodySchema struct {
	Shape  string       `json:"shape"` // "object" | "list" | "empty"
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

// readManifestAt loads a manifest. A missing file is not an error — it is the
// empty manifest, which is the correct state for an app that has scaffolded
// nothing yet.
func readManifestAt(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Manifest{APIVersion: "1.0.0", Models: []Model{}, Endpoints: []Endpoint{}, Pages: []Page{}}, nil
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

// UpsertPage mirrors UpsertEndpoint for the page table, keyed on path. Same
// path + same file refreshes the row in place; same path + a different file is
// two scaffolds claiming one URL, which errors and leaves the manifest
// untouched so nothing is written.
func (m *Manifest) UpsertPage(p Page) error {
	for i := range m.Pages {
		if m.Pages[i].Path == p.Path {
			if m.Pages[i].File != p.File {
				return fmt.Errorf("page conflict: %s is already served by static/pages/%s.html, cannot reassign to %s.html",
					p.Path, m.Pages[i].File, p.File)
			}
			m.Pages[i] = p
			return nil
		}
	}
	m.Pages = append(m.Pages, p)
	return nil
}

func (m *Manifest) canonicalize() {
	// Normalize nil to empty so a manifest written before a table existed
	// hashes identically to one written after — null and [] must not differ.
	if m.Models == nil {
		m.Models = []Model{}
	}
	if m.Endpoints == nil {
		m.Endpoints = []Endpoint{}
	}
	if m.Pages == nil {
		m.Pages = []Page{}
	}
	sort.Slice(m.Models, func(i, j int) bool { return m.Models[i].Name < m.Models[j].Name })
	sort.Slice(m.Endpoints, func(i, j int) bool {
		if m.Endpoints[i].Path != m.Endpoints[j].Path {
			return m.Endpoints[i].Path < m.Endpoints[j].Path
		}
		return m.Endpoints[i].Method < m.Endpoints[j].Method
	})
	sort.Slice(m.Pages, func(i, j int) bool { return m.Pages[i].Path < m.Pages[j].Path })
	m.Hash = manifestHash(*m)
}

// manifestHash is sha256 over the models, endpoints and pages (sorted by
// canonicalize before this is called), excluding generated_at so an
// otherwise-identical manifest always hashes the same. Pages are in the payload
// so that adding or moving a page shows up as a surface change in
// GET /api/v1/_version's manifest_hash.
func manifestHash(m Manifest) string {
	payload := struct {
		Models    []Model    `json:"models"`
		Endpoints []Endpoint `json:"endpoints"`
		Pages     []Page     `json:"pages"`
	}{m.Models, m.Endpoints, m.Pages}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeManifestAt(path string, m *Manifest, now time.Time) error {
	if m.APIVersion == "" {
		m.APIVersion = "1.0.0"
	}
	m.canonicalize()
	// Stamped on every write, so the record follows the surface it describes
	// rather than being set once at scaffold time and drifting.
	m.Template = Template{Version: templateVersion(), Fingerprint: templateFingerprint()}
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
	out, err := renderNamedToString("routes_gen.go.tmpl", data)
	if err != nil {
		return "", err
	}
	return formatGo("routes_gen.go", out), nil
}

func regenerateRoutesAt(handlersDir string, m Manifest) error {
	out, err := renderRoutes(m)
	if err != nil {
		return err
	}
	return writeGoFile(filepath.Join(handlersDir, "routes_gen.go"), out)
}

// pagesTemplateData is what both page templates render from: the mount lines
// for pages_gen.go and the raw rows for the generated test's table.
type pagesTemplateData struct {
	Lines []string
	Pages []Page
	// AnyAuth drives the conditional middleware import in pages_gen.go.tmpl —
	// Go will not compile an unused import, so a project with no guarded page
	// must not have one.
	AnyAuth bool
}

func pagesData(m Manifest) pagesTemplateData {
	m.canonicalize()
	lines := make([]string, 0, len(m.Pages))
	for _, p := range m.Pages {
		// auth:true wraps the page in a REDIRECT guard, not the JSON
		// RequireAuth: this is a human-facing URL and a browser must not be
		// handed an error envelope. See middleware.RequirePageAuth for what the
		// guard is and is not worth. The flag used to render nothing at all.
		if p.Auth {
			lines = append(lines, fmt.Sprintf(
				`r.With(middleware.RequirePageAuth).Get(%q, pageFile(%q))`, p.Path, p.File))
			continue
		}
		lines = append(lines, fmt.Sprintf(`r.Get(%q, pageFile(%q))`, p.Path, p.File))
	}
	anyAuth := false
	for _, p := range m.Pages {
		if p.Auth {
			anyAuth = true
			break
		}
	}
	return pagesTemplateData{Lines: lines, Pages: m.Pages, AnyAuth: anyAuth}
}

// renderPages emits RegisterPages from the manifest's page table. Each line
// passes a literal file base name — taken from the manifest, never from a
// request — to the pageFile helper, which is where the second guard
// (filepath.Base) lives.
func renderPages(m Manifest) (string, error) {
	out, err := renderNamedToString("pages_gen.go.tmpl", pagesData(m))
	if err != nil {
		return "", err
	}
	return formatGo("pages_gen.go", out), nil
}

// renderPagesTest emits the companion test that mounts RegisterPages on a real
// chi router and asserts every registered page actually serves its shell. It is
// generated rather than hand-written because the assertions are per-page: a
// hand-written test cannot know which pages a project scaffolded, and a page
// that is registered but unreachable is exactly the defect this closes.
func renderPagesTest(m Manifest) (string, error) {
	out, err := renderNamedToString("pages_gen_test.go.tmpl", pagesData(m))
	if err != nil {
		return "", err
	}
	return formatGo("pages_gen_test.go", out), nil
}

func regeneratePagesAt(handlersDir string, m Manifest) error {
	out, err := renderPages(m)
	if err != nil {
		return err
	}
	if err := writeGoFile(filepath.Join(handlersDir, "pages_gen.go"), out); err != nil {
		return err
	}
	testOut, err := renderPagesTest(m)
	if err != nil {
		return err
	}
	return writeGoFile(filepath.Join(handlersDir, "pages_gen_test.go"), testOut)
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
		out = append(out, ModelField{
			Name: f.Name, Type: typ, Nullable: f.Nullable,
			Format: f.Format, References: f.Ref,
		})
	}
	out = append(out, ModelField{Name: "created_at", Type: "timestamp", Nullable: false})
	return Model{Name: name, Table: table, Fields: out}
}

// parseBodySchemaArg parses a create_handler schema argument. Empty input means
// "no schema declared" (nil, nil). A non-empty value must be valid JSON with a
// recognized shape.
func parseBodySchemaArg(raw string) (*BodySchema, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var bs BodySchema
	if err := json.Unmarshal([]byte(raw), &bs); err != nil {
		return nil, fmt.Errorf("invalid schema JSON: %w", err)
	}
	switch bs.Shape {
	case "object", "list", "empty":
	default:
		return nil, fmt.Errorf("schema shape must be object|list|empty, got %q", bs.Shape)
	}
	return &bs, nil
}

// validateRefsAt fails if any field references a model not yet in the manifest.
// A dangling reference is a stated-fact violation — the parent must be
// scaffolded before its child.
func validateRefsAt(apiPath string, fields []Field) error {
	need := false
	for _, f := range fields {
		if f.Ref != "" {
			need = true
		}
	}
	if !need {
		return nil
	}
	m, err := readManifestAt(apiPath)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(m.Models))
	for _, mm := range m.Models {
		known[mm.Name] = true
	}
	for _, f := range fields {
		if f.Ref != "" && !known[f.Ref] {
			return fmt.Errorf("field %q references model %q which is not scaffolded yet — scaffold the parent resource first", f.Name, f.Ref)
		}
	}
	return nil
}

// validateRefs is the production entry point (against the live manifest path).
func validateRefs(fields []Field) error { return validateRefsAt(manifestFilePath, fields) }

// writableFields is a model's fields minus the auto columns id and created_at —
// the body a client sends on create/update.
func writableFields(m Model) []ModelField {
	out := make([]ModelField, 0, len(m.Fields))
	for _, f := range m.Fields {
		if f.Name == "id" || f.Name == "created_at" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func resourceRequest(m Model, kind string) *BodySchema {
	switch kind {
	case "create", "update":
		return &BodySchema{Shape: "object", Fields: writableFields(m)}
	default:
		return nil
	}
}

func resourceResponse(m Model, kind string) *BodySchema {
	switch kind {
	case "list":
		return &BodySchema{Shape: "list", Model: m.Name}
	case "detail", "create", "update":
		return &BodySchema{Shape: "object", Model: m.Name}
	case "delete":
		return &BodySchema{Shape: "object", Fields: []ModelField{{Name: "ok", Type: "boolean"}}}
	default:
		return nil
	}
}

// updateManifestAt is the transactional core: read, upsert all, and only if
// every upsert succeeded, write api.json and regenerate routes_gen.go and
// pages_gen.go. A conflict — on an endpoint or a page — returns before any file
// is touched.
func updateManifestAt(apiPath, handlersDir string, now time.Time, models []Model, endpoints []Endpoint, pages []Page) error {
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
	for _, p := range pages {
		if err := m.UpsertPage(p); err != nil {
			return err // conflict — nothing written yet
		}
	}
	if err := writeManifestAt(apiPath, &m, now); err != nil {
		return err
	}
	if err := regenerateRoutesAt(handlersDir, m); err != nil {
		return err
	}
	return regeneratePagesAt(handlersDir, m)
}
