package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// THE TEMPLATE COMPILE GATE.
//
// Everything else in this package asserts what the templates RENDER; this one
// asserts that what they render IS GO THAT COMPILES AND PASSES ITS OWN TESTS.
// A generator can parse-check nothing: templates are text, and a missing
// brace, an undefined name, or an import a template no longer emits shows up
// only when the output meets a compiler. That failure used to be discovered
// by the next app that ran scaffold_auth — weeks later, in someone else's
// project. Now it is discovered here, by this test, at template-change time.
//
// The fixture is assembled from the repo itself: /src/app supplies the fixed
// runtime (middleware, db, cache, main.go, json helpers, static assets), the
// auth+registration templates are rendered over that copy exactly as
// scaffold_auth + scaffold_registration would land them, and then the whole
// tree is built and tested with the real toolchain.
//
// Skipped when /src/app does not exist (unit tests run outside the container
// where the app tree is absent) — the gate's value is in the dev loop and
// CI, both of which have the tree.
// ═════════════════════════════════════════════════════════════════════════════

func TestAuthTemplateCompileGate(t *testing.T) {
	srcApp := "/src/app"
	if _, err := os.Stat(srcApp); err != nil {
		t.Skipf("compile gate: %s not available (outside the app container?): %v", srcApp, err)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("compile gate: go toolchain not on PATH")
	}

	root := t.TempDir()
	scratchApp(t, srcApp, root)
	renderAuthFixtures(t, root)
	renderCredentialFixtures(t, root)
	assertResourceTemplatesRefuseCredentials(t, root)

	// The gate must actually GATE. A gate that passes on broken code is a
	// rubber stamp, so before trusting a pass, plant code that cannot
	// compile and require the build to go red. Then remove it and require
	// the build to go green — which is the real assertion.
	plantBrokenFile(t, filepath.Join(root, "handlers"))

	build := execGo(t, root, "go", "build", "./...")
	if build.err == nil {
		t.Fatal("gate: build PASSED on deliberately broken Go — the gate cannot detect template defects")
	}

	// Clean it up and require the real pass.
	undoBrokenFile(t, filepath.Join(root, "handlers"))
	build = execGo(t, root, "go", "build", "./...")
	if build.err != nil {
		t.Fatalf("templates do not compile as an app:\n%s\n%s", build.out, build.err)
	}

	test := execGo(t, root, "go", "test", "-count=1", "-timeout", "180s", "./...")
	if test.err != nil {
		t.Fatalf("templates compile but the generated tests FAIL:\n%s\n%s", test.out, test.err)
	}
}

// scratchApp copies /src/app into root — the fixed runtime a generated app
// starts from. Generated _gen.go files come along: they match the committed
// api.json and the copied main.go mounts them.
func scratchApp(t *testing.T, srcApp, root string) {
	t.Helper()
	err := filepath.Walk(srcApp, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(srcApp, path)
		if rel == "." {
			return nil
		}
		// Skip nothing except caches: generated tests (pages_gen_test.go,
		// routes_gen_test.go) are part of what must pass.
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(root, rel), 0755)
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(filepath.Join(root, rel), data, 0644)
	})
	if err != nil {
		t.Fatalf("copying %s to fixture: %v", srcApp, err)
	}
	// The fixture nests inside the builder's module tree? No: t.TempDir lives
	// outside it, but strip any accidental nesting by writing a go.mod sure
	// to be present (copied above). Belt: verify it survived the walk.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("fixture missing go.mod: %v", err)
	}
}

// renderAuthFixtures writes every auth/registration file the two scaffold
// tools emit, using the same render path (renderToFile) and the same
// TemplateData shape as handleScaffoldAuth and handleScaffoldRegistration.
func renderAuthFixtures(t *testing.T, root string) {
	t.Helper()
	data := newData("user", nil)
	type spec struct{ tmpl, out string }
	specs := []spec{
		{"user_model.go.tmpl", filepath.Join(root, "models", "User.go")},
		{"mobile_token_model.go.tmpl", filepath.Join(root, "models", "MobileToken.go")},
		{"auth_handler.go.tmpl", filepath.Join(root, "handlers", "auth.go")},
		{"auth_test.go.tmpl", filepath.Join(root, "handlers", "auth_test.go")},
		{"clientip.go.tmpl", filepath.Join(root, "handlers", "clientip.go")},
		{"clientip_test.go.tmpl", filepath.Join(root, "handlers", "clientip_test.go")},
		{"auth_buckets.go.tmpl", filepath.Join(root, "handlers", "auth_buckets.go")},
		{"auth_buckets_test.go.tmpl", filepath.Join(root, "handlers", "auth_buckets_test.go")},
		{"logout_handler.go.tmpl", filepath.Join(root, "handlers", "logout.go")},
		{"mobile_auth_handler.go.tmpl", filepath.Join(root, "handlers", "mobile_auth.go")},
		{"mobile_auth_test.go.tmpl", filepath.Join(root, "handlers", "mobile_auth_test.go")},
		{"register_handler.go.tmpl", filepath.Join(root, "handlers", "register.go")},
		{"register_test.go.tmpl", filepath.Join(root, "handlers", "register_test.go")},
	}
	for _, s := range specs {
		if err := renderToFile(s.tmpl, s.out, data); err != nil {
			t.Fatalf("render %s: %v", s.tmpl, err)
		}
	}
}

// renderCredentialFixtures renders the credential-bearing models into the same
// package as scaffold_auth's User, and the gate then BUILDS and TESTS them.
//
// Two of the password defects create_model shipped were compile failures, not
// wrong output: the bcrypt call named a parameter createParams never declared,
// and any package-level ErrPasswordTooLong / maxPasswordBytes here collides
// with the ones user_model.go.tmpl declares in models/User.go. Neither is
// visible to a string assertion, and neither was caught until an app hit it by
// hand.
func renderCredentialFixtures(t *testing.T, root string) {
	t.Helper()

	// The column is deliberately NOT named `password` — that name is the one
	// case the old hard-coded bcrypt call happened to get right, so a fixture
	// using it would have passed against the bug.
	//
	// CRUD so Update's hashing path compiles too, and so the generated test
	// runs Create/Update/GetPage against a real SQLite table.
	credential := newData("member", []Field{
		{Name: "label", Type: "string"},
		{Name: "password_hash", Type: "password"},
		{Name: "note", Type: "string", Nullable: true},
	})
	credential.CRUD = true
	credential.Title = "Members"

	// A model whose ONLY field is a credential: its public column list is
	// EMPTY, which is where a naive "SELECT id, %s, created_at" splice renders
	// "SELECT id, , created_at" and a Scan with a hole in its argument list.
	vault := newData("vault", []Field{
		{Name: "secret_hash", Type: "password"},
	})
	vault.Title = "Vaults"

	type spec struct {
		tmpl string
		out  string
		data TemplateData
	}
	for _, s := range []spec{
		{"model.go.tmpl", filepath.Join(root, "models", "Member.go"), credential},
		{"model_test.go.tmpl", filepath.Join(root, "models", "Member_test.go"), credential},
		{"model.go.tmpl", filepath.Join(root, "models", "Vault.go"), vault},
		{"model_test.go.tmpl", filepath.Join(root, "models", "Vault_test.go"), vault},
	} {
		if err := renderToFile(s.tmpl, s.out, s.data); err != nil {
			t.Fatalf("render %s: %v", s.tmpl, err)
		}
	}
}

// assertResourceTemplatesRefuseCredentials is the credential half of the gate,
// and it is an assertion the compiler CANNOT make.
//
// Everything else here proves that what the templates render compiles. This one
// proves that one particular pairing NEVER RENDERS AT ALL — because if it did,
// it would compile perfectly and be wrong.
//
// The pairing: a credential-bearing model rendered through
// resource_handlers.go.tmpl. The generated request struct tags every field by
// column name and the PUT hands them all to Update, so a request that simply
// OMITS the password field decodes it to "" and the model stores bcrypt("") — a
// known-empty password anyone can then authenticate against, on an endpoint
// scaffold_resource documents as PUBLIC by default. Every type in that call
// chain is `string`, so `go build` is perfectly happy. Until create_model's
// password handling was fixed, the combination happened not to compile for any
// column not named exactly `password`; that accident is gone, and this is the
// deliberate replacement.
//
// renderCredentialFixtures renders the SAME credential model through
// model.go.tmpl and model_test.go.tmpl and BUILDS it — that pairing is fine and
// must keep working. It is only the generic-CRUD handler pairing that is
// refused, in two independent places (handleScaffoldResource's field check and
// the templates' own refuseCredentials guard). Remove either and this fails.
//
// It also asserts NOTHING WAS WRITTEN: renderToFile renders before it writes,
// so a refusal must not leave a half-file in handlers/ for the build to trip
// over.
func assertResourceTemplatesRefuseCredentials(t *testing.T, root string) {
	t.Helper()
	data := newData("member", []Field{
		{Name: "label", Type: "string"},
		{Name: "password_hash", Type: "password"},
		{Name: "note", Type: "string", Nullable: true},
	})
	data.CRUD = true
	data.Title = "Members"

	for _, tmpl := range []string{"resource_handlers.go.tmpl", "resource_handlers_test.go.tmpl"} {
		out := filepath.Join(root, "handlers", "zz_credential_"+tmpl+".go")
		err := renderToFile(tmpl, out, data)
		if err == nil {
			t.Fatalf("gate: %s RENDERED a credential-bearing model. It compiles and it is wrong: "+
				"a PUT that omits %q blanks the stored credential with bcrypt(\"\"). "+
				"Both refusals (handleScaffoldResource's field check and the template's "+
				"refuseCredentials guard) are gone.", tmpl, "password_hash")
		}
		if !strings.Contains(err.Error(), "password_hash") {
			t.Errorf("gate: %s refused, but without naming the column: %v", tmpl, err)
		}
		if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
			t.Errorf("gate: %s left a file behind after refusing to render (%v) — "+
				"a partial write would break the build below", tmpl, statErr)
		}
	}
}

// plantBrokenFile writes Go that parses in isolation but references a symbol
// that exists nowhere — the exact class of defect (a renamed template symbol,
// a lost import) that a formatting-only check cannot see.
var brokenContent = []byte("package handlers\n\nfunc brokenGateProbe() { return undefinedSymbolNowhere }")

func plantBrokenFile(t *testing.T, handlersDir string) string {
	t.Helper()
	path := filepath.Join(handlersDir, "zz_gate_broken.go")
	if err := os.WriteFile(path, brokenContent, 0644); err != nil {
		t.Fatalf("planting broken file: %v", err)
	}
	return path
}

func undoBrokenFile(t *testing.T, handlersDir string) {
	t.Helper()
	if err := os.Remove(filepath.Join(handlersDir, "zz_gate_broken.go")); err != nil {
		t.Fatalf("removing broken file: %v", err)
	}
}

// execGo runs a go command in dir and returns its combined output.
func execGo(t *testing.T, dir, name string, args ...string) *execResult {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// GOFLAGS=-mod=mod so the fixture's go.mod self-heals if a template
	// template added a dependency the committed one lacks — the gate is
	// about compile success, not lockfile purity. GOPROXY stays at the
	// default: the download cache under /go/pkg/mod/cache is warm from the
	// image build, so first extraction is local, and a genuinely new
	// dependency must resolve honestly rather than faking a cache miss as
	// a template failure.
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return &execResult{out: out.String(), err: err}
}

type execResult struct {
	out string
	err error
}