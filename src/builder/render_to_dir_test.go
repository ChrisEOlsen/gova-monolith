package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRenderResourceToDir renders a full resource into $SCRATCH_APP_DIR so the
// generated code can be COMPILED and its own tests RUN, not merely parsed.
//
// Every other render test stops at parser.ParseFile, which proves the output is
// syntactically Go and nothing more. That ceiling hides output which parses but
// does not compile — the `timestamp` type alone touches seven helpers plus
// models.NullTime, and any of them can produce a file that parses and fails to
// build. The fixture carries a NOT NULL and a nullable timestamp for that reason.
//
// Skipped unless the variable is set, because it writes outside the repo and
// needs a tree with the rest of the app in it. From the repo root:
//
//	docker run --rm -v "$PWD":/w -w /w golang:1.25 sh -c '
//	  rm -rf /scratch && cp -r /w/src/app /scratch
//	  cd /w/src/builder && SCRATCH_APP_DIR=/scratch go test -run TestRenderResourceToDir -count=1 .
//	  cd /scratch && go build ./... && go test ./handlers/ ./models/'
func TestRenderResourceToDir(t *testing.T) {
	root := os.Getenv("SCRATCH_APP_DIR")
	if root == "" {
		t.Skip("SCRATCH_APP_DIR unset — see the doc comment for the full invocation")
	}
	data := newData("widget", []Field{
		{Name: "title", Type: "string"},
		{Name: "notes", Type: "string", Nullable: true},
		{Name: "quantity", Type: "int"},
		{Name: "updated_at", Type: "timestamp"},
		{Name: "archived_at", Type: "timestamp", Nullable: true},
	})
	data.CRUD = true
	data.Title = "Widgets"

	for _, s := range [][2]string{
		{"model.go.tmpl", "models/Widget.go"},
		{"model_test.go.tmpl", "models/Widget_test.go"},
		{"resource_handlers.go.tmpl", "handlers/widget_resource.go"},
		{"resource_handlers_test.go.tmpl", "handlers/widget_resource_test.go"},
		{"list_page.html.tmpl", "static/pages/widgets.html"},
		{"list_page.js.tmpl", "static/js/widgets.js"},
	} {
		if err := renderToFile(s[0], filepath.Join(root, s[1]), data); err != nil {
			t.Fatalf("%s: %v", s[0], err)
		}
	}
}
