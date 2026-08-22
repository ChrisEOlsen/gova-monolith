package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildInspection_ReportsDivergence(t *testing.T) {
	m := Manifest{
		Models:    []Model{{Name: "task", Table: "tasks"}},
		Endpoints: []Endpoint{{Method: "GET", Path: "/api/v1/tasks", Handler: "TaskListGET"}},
	}
	onDisk := onDiskFiles{
		Models:   []string{}, // Task.go missing
		Handlers: []string{"task_list.go", "routes_gen.go"},
	}
	rep := buildInspection(m, onDisk)

	if len(rep.Divergence) == 0 {
		t.Fatal("expected divergence for missing Task.go")
	}
	joined := strings.Join(rep.Divergence, " ")
	if !strings.Contains(joined, "task") {
		t.Errorf("divergence should name the missing model: %v", rep.Divergence)
	}

	// It must serialize to JSON with the three top-level keys.
	data, _ := json.Marshal(rep)
	for _, key := range []string{`"manifest"`, `"on_disk"`, `"divergence"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("inspection JSON missing %s: %s", key, data)
		}
	}
}

func TestBuildInspection_CleanWhenConsistent(t *testing.T) {
	m := Manifest{
		// Stamped by the builder that is running this test, which is what a
		// manifest written by the current tools looks like.
		Template:  Template{Version: templateVersion(), Fingerprint: templateFingerprint()},
		Models:    []Model{{Name: "project", Table: "projects"}},
		Endpoints: []Endpoint{},
	}
	onDisk := onDiskFiles{Models: []string{"Project.go"}, Handlers: []string{"routes_gen.go"}}
	rep := buildInspection(m, onDisk)
	if len(rep.Divergence) != 0 {
		t.Errorf("expected no divergence, got %v", rep.Divergence)
	}
}

// The stamp exists so an app can answer "which template am I built from".
// These are the three ways that answer can be wrong, and each must be visible
// rather than silent — silence is exactly what let one app carry three
// already-fixed defects for weeks.
func TestBuildInspection_TemplateStampDivergence(t *testing.T) {
	populated := func(tmpl Template) Manifest {
		return Manifest{
			Template:  tmpl,
			Models:    []Model{{Name: "project", Table: "projects"}},
			Endpoints: []Endpoint{},
		}
	}
	onDisk := onDiskFiles{Models: []string{"Project.go"}, Handlers: []string{"routes_gen.go"}}

	t.Run("no stamp at all", func(t *testing.T) {
		// A manifest written before stamping existed: the app predates it and
		// cannot say what built it.
		rep := buildInspection(populated(Template{}), onDisk)
		if !containsSub(rep.Divergence, "carries no template stamp") {
			t.Errorf("an unstamped manifest must be flagged, got %v", rep.Divergence)
		}
	})

	t.Run("empty project is not nagged", func(t *testing.T) {
		// A fresh checkout has an empty manifest and no stamp yet. Flagging
		// that would make the very first inspect_app of every new project cry
		// wolf, which is how a warning stops being read.
		rep := buildInspection(Manifest{}, onDiskFiles{})
		if containsSub(rep.Divergence, "template stamp") {
			t.Errorf("an empty project must not be flagged, got %v", rep.Divergence)
		}
	})

	t.Run("stale running binary", func(t *testing.T) {
		// src/builder synced on disk, mcp image never rebuilt: the manifest was
		// written by a version the running tools are not.
		rep := buildInspection(populated(Template{Version: "1999-01-01.1", Fingerprint: "sha256:old"}), onDisk)
		if !containsSub(rep.Divergence, "docker compose up -d --build mcp") {
			t.Errorf("a version mismatch must name the rebuild, got %v", rep.Divergence)
		}
	})

	t.Run("templates edited without bumping VERSION", func(t *testing.T) {
		// The version is a promise a human makes; the fingerprint is what the
		// binary carries. This is them coming apart.
		rep := buildInspection(populated(Template{Version: templateVersion(), Fingerprint: "sha256:different"}), onDisk)
		if !containsSub(rep.Divergence, "without bumping src/builder/VERSION") {
			t.Errorf("an edited template must be flagged, got %v", rep.Divergence)
		}
	})

	t.Run("current builder is clean", func(t *testing.T) {
		rep := buildInspection(populated(Template{Version: templateVersion(), Fingerprint: templateFingerprint()}), onDisk)
		if len(rep.Divergence) != 0 {
			t.Errorf("a manifest written by this builder must be clean, got %v", rep.Divergence)
		}
		if rep.Builder.Version != templateVersion() {
			t.Errorf("inspection must report the running builder, got %q", rep.Builder.Version)
		}
	})
}

func containsSub(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
