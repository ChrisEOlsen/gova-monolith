package main

import (
	"strings"
	"testing"
)

func TestBuildInspection_ReportsMissingFiles(t *testing.T) {
	m := Manifest{
		Models: []Model{{Name: "widget", Table: "widgets"}, {Name: "gadget", Table: "gadgets"}},
		Pages:  []Page{{Path: "/widgets", File: "widgets"}, {Path: "/gone", File: "gone"}},
	}
	onDisk := onDiskFiles{
		Models: []string{"Widget.go"},
		Pages:  []string{"widgets.html"},
	}

	rep := buildInspection(m, onDisk)
	joined := strings.Join(rep.Divergence, "\n")

	if !strings.Contains(joined, "Gadget.go is missing") {
		t.Errorf("a model with no file should diverge:\n%s", joined)
	}
	if strings.Contains(joined, "Widget.go is missing") {
		t.Errorf("a model with a file should not diverge:\n%s", joined)
	}
	if !strings.Contains(joined, "gone.html is missing") {
		t.Errorf("a page with no shell should diverge — that route 404s:\n%s", joined)
	}
}

// Auth's models ship as hand-written files, so they are exempt from the
// "manifest lists it, no generated file" check.
func TestBuildInspection_SkipsTemplateOwnedModels(t *testing.T) {
	m := Manifest{Models: []Model{{Name: "user", Table: "users"}}}
	rep := buildInspection(m, onDiskFiles{})
	if len(rep.Divergence) != 0 {
		t.Errorf("the template's own user model must not read as divergence: %v", rep.Divergence)
	}
}

// The common mistake: src/builder is updated on disk but the mcp image is not
// rebuilt, so the running tools are the old binary.
func TestBuildInspection_StaleBuilderVersionDiverges(t *testing.T) {
	populated := func(v string) Manifest {
		return Manifest{BuilderVersion: v, Models: []Model{}, Endpoints: []Endpoint{}, Pages: []Page{}}
	}

	rep := buildInspection(populated("1999-01-01.1"), onDiskFiles{})
	if len(rep.Divergence) == 0 || !strings.Contains(strings.Join(rep.Divergence, "\n"), "rebuild the image") {
		t.Errorf("a version mismatch should say to rebuild: %v", rep.Divergence)
	}

	rep = buildInspection(populated(builderVersion), onDiskFiles{})
	if len(rep.Divergence) != 0 {
		t.Errorf("a matching version must not diverge: %v", rep.Divergence)
	}
	if rep.BuilderVersion != builderVersion {
		t.Errorf("BuilderVersion = %q, want %q", rep.BuilderVersion, builderVersion)
	}

	// An unstamped manifest is not an error — a hand-written one is legitimate.
	if rep := buildInspection(populated(""), onDiskFiles{}); len(rep.Divergence) != 0 {
		t.Errorf("an unstamped manifest must not diverge: %v", rep.Divergence)
	}
}
