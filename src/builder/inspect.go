package main

import (
	"os"
	"path/filepath"
)

// onDiskFiles is a snapshot of the generated-file names found in each of the
// four directories the builder scaffolds into.
type onDiskFiles struct {
	Models   []string `json:"models"`
	Handlers []string `json:"handlers"`
	Pages    []string `json:"pages"`
	JS       []string `json:"js"`
}

// inspection is the structured result of the inspect_app tool.
type inspection struct {
	Manifest       Manifest    `json:"manifest"`
	OnDisk         onDiskFiles `json:"on_disk"`
	BuilderVersion string      `json:"builder_version"`
	Divergence     []string    `json:"divergence"`
}

// buildInspection cross-checks the manifest against the files on disk.
func buildInspection(m Manifest, onDisk onDiskFiles) inspection {
	present := func(list []string, name string) bool {
		for _, f := range list {
			if f == name {
				return true
			}
		}
		return false
	}
	div := []string{}
	for _, model := range m.Models {
		// Auth's models ship as hand-written files under their own names.
		if reservedModelNames[model.Name] {
			continue
		}
		if !present(onDisk.Models, toPascal(model.Name)+".go") {
			div = append(div, "api.json lists model '"+model.Name+"' but src/app/models/"+toPascal(model.Name)+".go is missing")
		}
	}
	// A registered page whose shell is gone is a route that 404s at runtime.
	for _, page := range m.Pages {
		if !present(onDisk.Pages, page.File+".html") {
			div = append(div, "api.json registers page '"+page.Path+"' but src/app/static/pages/"+page.File+".html is missing")
		}
	}
	// The manifest records the builder that wrote it. A mismatch against the
	// running one almost always means src/builder was updated on disk without
	// rebuilding the mcp image — which embeds its templates at image build
	// time, so a plain restart reruns the old binary.
	if m.BuilderVersion != "" && m.BuilderVersion != builderVersion {
		div = append(div, "api.json was written by builder "+m.BuilderVersion+
			" but the running builder is "+builderVersion+
			" — if src/builder was just synced, rebuild the image and re-stamp the manifest: "+
			"docker compose up -d --build builder && ./gova regen")
	}

	return inspection{Manifest: m, OnDisk: onDisk, BuilderVersion: builderVersion, Divergence: div}
}

// generatedDivergence reports generated files that no longer match what
// api.json would produce.
//
// This is the check that catches a hand edit which has not been applied. The
// documented way to protect an existing route is to set auth:true in api.json,
// but the *_gen.go files are only rewritten as a side effect of a scaffold — so
// until something else ran, the manifest claimed a guard the router had never
// mounted, and nothing said so. Re-rendering from the manifest and diffing is
// the only honest way to know: the files are deterministic output of exactly
// this input.
func generatedDivergence(handlersDir string, m Manifest) []string {
	div := []string{}
	for _, f := range []struct {
		name   string
		render func(Manifest) (string, error)
	}{
		{"routes_gen.go", renderRoutes},
		{"pages_gen.go", renderPages},
		{"pages_gen_test.go", renderPagesTest},
	} {
		want, err := f.render(m)
		if err != nil {
			div = append(div, "could not re-render "+f.name+" from api.json: "+err.Error())
			continue
		}
		got, err := os.ReadFile(filepath.Join(handlersDir, f.name))
		if err != nil {
			div = append(div, "api.json is present but handlers/"+f.name+" is missing — run `gova regen`")
			continue
		}
		if string(got) != want {
			div = append(div, "handlers/"+f.name+" does not match api.json — an edit to the manifest has "+
				"not been applied and the running router does not reflect it; run `gova regen`")
		}
	}
	return div
}
