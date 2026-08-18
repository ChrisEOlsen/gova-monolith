package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRenderScaffoldAuthToDir renders the whole scaffold_auth file set into
// $SCRATCH_APP_DIR, so the generated code can be COMPILED AND ITS TESTS RUN
// rather than only parsed.
//
// Every other test in this file's neighbourhood stops at parser.ParseFile: it
// proves the output is syntactically Go and nothing more. That ceiling is real,
// and it hides a specific class of defect — output that parses, and does not
// compile or does not agree with its own sibling templates. Changing how the
// login handlers key the rate limiter broke two GENERATED tests that arm the
// bucket through the model; both templates still parsed perfectly.
//
// Skipped unless the variable is set, because it writes outside the repo and
// needs a caller to supply a tree with the rest of the app in it. The full
// invocation, from the repo root:
//
//	docker run --rm -v "$PWD":/w -w /w golang:1.25 sh -c '
//	  rm -rf /scratch && cp -r /w/src/app /scratch
//	  cd /w/src/builder && SCRATCH_APP_DIR=/scratch go test -run TestRenderScaffoldAuthToDir -count=1 .
//	  cd /scratch && go build ./... && go test ./handlers/ ./models/'
//
// It is also the harness for mutation-checking a template change: render, edit
// one line in /scratch, re-run the generated tests, and see which of them
// notices.
func TestRenderScaffoldAuthToDir(t *testing.T) {
	root := os.Getenv("SCRATCH_APP_DIR")
	if root == "" {
		t.Skip("SCRATCH_APP_DIR unset — see the doc comment for the full invocation")
	}
	data := newData("user", nil)

	// Mirrors handleScaffoldAuth's and handleScaffoldRegistration's own file
	// specs. Registration is included because its handler shares the package
	// and would not compile on its own.
	specs := [][2]string{
		{"user_model.go.tmpl", "models/User.go"},
		{"mobile_token_model.go.tmpl", "models/MobileToken.go"},
		{"auth_handler.go.tmpl", "handlers/auth.go"},
		{"auth_test.go.tmpl", "handlers/auth_test.go"},
		{"logout_handler.go.tmpl", "handlers/logout.go"},
		{"clientip.go.tmpl", "handlers/clientip.go"},
		{"clientip_test.go.tmpl", "handlers/clientip_test.go"},
		{"auth_buckets.go.tmpl", "handlers/auth_buckets.go"},
		{"auth_buckets_test.go.tmpl", "handlers/auth_buckets_test.go"},
		{"mobile_auth_handler.go.tmpl", "handlers/mobile_auth.go"},
		{"mobile_auth_test.go.tmpl", "handlers/mobile_auth_test.go"},
		{"register_handler.go.tmpl", "handlers/register.go"},
		{"register_test.go.tmpl", "handlers/register_test.go"},
	}
	for _, s := range specs {
		if err := renderToFile(s[0], filepath.Join(root, s[1]), data); err != nil {
			t.Fatalf("%s: %v", s[0], err)
		}
	}
}

// TestRenderTimestampModelToDir renders a model carrying both a NOT NULL and a
// nullable timestamp field into $SCRATCH_APP_DIR, for the same reason as the
// test above: the `timestamp` field type touches seven helpers (goTypeFor,
// nullTypeFor, nullFieldFor, testLiteralFor, sqlType, hasTimestamp,
// testFieldMismatch) plus models.NullTime, and every one of them can produce
// output that parses and does not compile.
//
// Same invocation, with -run TestRenderTimestampModelToDir.
func TestRenderTimestampModelToDir(t *testing.T) {
	root := os.Getenv("SCRATCH_APP_DIR")
	if root == "" {
		t.Skip("SCRATCH_APP_DIR unset — see TestRenderScaffoldAuthToDir for the full invocation")
	}
	data := newData("widget", []Field{
		{Name: "title", Type: "string", Nullable: false},
		{Name: "updated_at", Type: "timestamp", Nullable: false},
		{Name: "archived_at", Type: "timestamp", Nullable: true},
	})
	data.CRUD = true
	for _, s := range [][2]string{
		{"model.go.tmpl", "models/Widget.go"},
		{"model_test.go.tmpl", "models/Widget_test.go"},
	} {
		if err := renderToFile(s[0], filepath.Join(root, s[1]), data); err != nil {
			t.Fatalf("%s: %v", s[0], err)
		}
	}
}
