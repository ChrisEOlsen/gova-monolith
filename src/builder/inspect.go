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
	return inspection{Manifest: m, OnDisk: onDisk, Divergence: div}
}
