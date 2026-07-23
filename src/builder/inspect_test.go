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
		Models:    []Model{{Name: "project", Table: "projects"}},
		Endpoints: []Endpoint{},
	}
	onDisk := onDiskFiles{Models: []string{"Project.go"}, Handlers: []string{"routes_gen.go"}}
	rep := buildInspection(m, onDisk)
	if len(rep.Divergence) != 0 {
		t.Errorf("expected no divergence, got %v", rep.Divergence)
	}
}
