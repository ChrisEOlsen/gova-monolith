package main

// onDiskFiles is a snapshot of the generated-file names found in each of the
// four directories the builder scaffolds into.
type onDiskFiles struct {
	Models   []string `json:"models"`
	Handlers []string `json:"handlers"`
	Pages    []string `json:"pages"`
	JS       []string `json:"js"`
}

// inspection is the structured result returned by the inspect_app tool.
type inspection struct {
	Manifest   Manifest    `json:"manifest"`
	OnDisk     onDiskFiles `json:"on_disk"`
	Builder    Template    `json:"builder"`
	Divergence []string    `json:"divergence"`
}

// buildInspection cross-checks the manifest against the files actually on disk.
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
		if !present(onDisk.Models, toPascal(model.Name)+".go") {
			div = append(div, "api.json lists model '"+model.Name+"' but src/app/models/"+toPascal(model.Name)+".go is missing")
		}
	}
	// A registered page whose shell is gone is a route that 404s at runtime —
	// exactly the class of unreachable-page defect the page table exists to
	// close, so it is worth surfacing before a build depends on it.
	for _, page := range m.Pages {
		if !present(onDisk.Pages, page.File+".html") {
			div = append(div, "api.json registers page '"+page.Path+"' but src/app/static/pages/"+page.File+".html is missing")
		}
	}
	// WHICH BUILDER WROTE THIS, AND WHICH ONE IS RUNNING NOW?
	//
	// These disagree in one specific and easy-to-hit way: src/builder is
	// updated on disk but the mcp image is not rebuilt, so the running tools
	// are the OLD binary — it embeds its templates at image build time, and a
	// plain `docker compose restart` reruns the stale one. CLAUDE.md warns
	// about that in prose; this is the check that notices.
	//
	// It reads as a divergence rather than an error because the manifest is
	// not wrong — it faithfully records the builder that wrote it. What is
	// wrong is that the next tool call will write something different.
	running := Template{Version: templateVersion(), Fingerprint: templateFingerprint()}
	switch {
	case m.Template.Version == "" && (len(m.Models) > 0 || len(m.Endpoints) > 0 || len(m.Pages) > 0):
		div = append(div, "api.json carries no template stamp — it was written by a builder older than "+
			running.Version+"; re-run any scaffold tool to record one")
	case m.Template.Version != "" && m.Template.Version != running.Version:
		div = append(div, "api.json was written by template "+m.Template.Version+
			" but the running builder is "+running.Version+
			" — if src/builder was just synced, rebuild the mcp image: docker compose up -d --build mcp")
	case m.Template.Fingerprint != "" && m.Template.Fingerprint != running.Fingerprint:
		div = append(div, "template version "+running.Version+
			" matches but the templates themselves differ from the ones that wrote api.json"+
			" — templates were edited without bumping src/builder/VERSION")
	}

	return inspection{Manifest: m, OnDisk: onDisk, Builder: running, Divergence: div}
}
