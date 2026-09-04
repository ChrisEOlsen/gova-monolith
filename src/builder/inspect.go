package main

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
			" — if src/builder was just synced, rebuild the image: docker compose up -d --build mcp")
	}

	return inspection{Manifest: m, OnDisk: onDisk, BuilderVersion: builderVersion, Divergence: div}
}
