package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// ═════════════════════════════════════════════════════════════════════════════
// CREDENTIAL HANDLING.
//
// Five defects lived here at once, and they shared one root cause: "is this
// column a secret?" was answered independently, and differently, in five
// places. These tests pin the single answer (isSecret / isCredentialColumn /
// publicFields) and each of its consequences.
//
// The fixture below is the shape at issue everywhere: a credential column that
// is NOT literally named `password`, between two ordinary columns. A fixture
// named `password` would have passed against every one of the bugs.
// ═════════════════════════════════════════════════════════════════════════════

func credentialFields() []Field {
	return []Field{
		{Name: "label", Type: "string"},
		{Name: "password_hash", Type: "password"},
		{Name: "note", Type: "string", Nullable: true},
	}
}

func renderModel(t *testing.T, data TemplateData) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "Model.go")
	if err := renderToFile("model.go.tmpl", out, data); err != nil {
		t.Fatalf("render model.go.tmpl: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read rendered model: %v", err)
	}
	return string(b)
}

// ─── the predicates ──────────────────────────────────────────────────────────

func TestCredentialNamedColumn(t *testing.T) {
	credential := []string{
		// segment matches
		"password", "passwd", "client_secret", "pw_secret", "passphrase",
		"token", "session_token", "refresh_token", "access_token", "salt", "otp",
		"otp_secret", "apikey",
		// _hash suffix
		"password_hash", "api_key_hash", "session_token_hash", "content_hash",
		// whole-name matches, where no single segment is a credential word
		"api_key", "private_key", "secret_key", "access_key", "recovery_code",
		"backup_code", "pass_word",
	}
	for _, n := range credential {
		if !credentialNamedColumn(n) {
			t.Errorf("credentialNamedColumn(%q) = false, want true", n)
		}
	}
	// Segment matching, not substring matching — these must NOT trip.
	// `key` is deliberately NOT a credential segment: these are ordinary.
	ordinary := []string{
		"secretary_id", "passwordless_at", "hash_algorithm", "name", "email",
		"hashtag", "id", "sort_key", "foreign_key", "primary_key", "key",
		"salted_caramel_id", "code", "encoding", "tokenizer",
	}
	for _, n := range ordinary {
		if credentialNamedColumn(n) {
			t.Errorf("credentialNamedColumn(%q) = true, want false", n)
		}
	}
}

// isSecret must stay narrow: it drives CODE GENERATION (bcrypt), and widening
// it to the name heuristic would make Create bcrypt an already-hashed digest a
// second time and silently break every lookup against it.
func TestIsSecretIsTypeOnly(t *testing.T) {
	if isSecret(Field{Name: "password_hash", Type: "string"}) {
		t.Error("isSecret matched on the NAME; it must be the declared type only")
	}
	if !isSecret(Field{Name: "pw", Type: "password"}) {
		t.Error("isSecret missed a declared password whose name says nothing")
	}
	// ...while the EXPOSURE predicate catches both.
	if !isCredentialColumn(Field{Name: "password_hash", Type: "string"}) {
		t.Error("isCredentialColumn missed a misdeclared credential")
	}
	if !isCredentialColumn(Field{Name: "pw", Type: "password"}) {
		t.Error("isCredentialColumn missed a declared password")
	}
}

func TestMisdeclaredCredentialNames(t *testing.T) {
	got := misdeclaredCredentialNames([]Field{
		{Name: "label", Type: "string"},
		{Name: "password_hash", Type: "string"}, // named, not declared
		{Name: "pw", Type: "password"},          // declared, not named
	})
	if !reflect.DeepEqual(got, []string{"password_hash"}) {
		t.Errorf("got %v, want [password_hash]", got)
	}
}

// ─── item 1: the struct tag ──────────────────────────────────────────────────

func TestModelTagsCredentialsJSONDash(t *testing.T) {
	code := renderModel(t, newData("member", credentialFields()))
	if !strings.Contains(code, "`json:\"-\"`") {
		t.Errorf("the credential column is not tagged json:\"-\":\n%s", code)
	}
	if strings.Contains(code, "`json:\"password_hash\"`") {
		t.Errorf("the bcrypt hash is serialized under its column name:\n%s", code)
	}
	// Ordinary columns keep their tags.
	for _, want := range []string{"`json:\"label\"`", "`json:\"note\"`"} {
		if !strings.Contains(code, want) {
			t.Errorf("missing tag %s", want)
		}
	}
}

// ─── item 2: bcrypt derives its parameter from one source ────────────────────

// The old template hashed []byte(password) while createParams named the
// parameter after the COLUMN, so any password column not literally called
// `password` produced a file referencing an undeclared identifier. The compile
// gate proves the output builds; this proves the two names agree.
func TestCreateParamAndBcryptCallAgree(t *testing.T) {
	// The fixture's credential column is `secret_hash`, NOT `password_hash`:
	// password_hash trims to the parameter `password`, which is exactly the
	// name the old hard-coded bcrypt call used — so a password_hash fixture
	// would have passed against the bug it is meant to catch.
	data := newData("vault", []Field{
		{Name: "label", Type: "string"},
		{Name: "secret_hash", Type: "password"},
	})
	data.CRUD = true
	code := renderModel(t, data)

	if strings.Contains(code, "[]byte(password)") {
		t.Errorf("bcrypt still hard-codes the parameter name `password` for a `secret_hash` column:\n%s", code)
	}
	// secret_hash → secret, and the hash local is derived from that same name.
	if !strings.Contains(code, "func (m *VaultModel) Create(label string, secret string)") {
		t.Errorf("Create's parameter is not derived from createParamNames:\n%s", code)
	}
	if !strings.Contains(code, "bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)") {
		t.Errorf("bcrypt call not derived from the same name as the parameter:\n%s", code)
	}
	if !strings.Contains(code, "string(secretHashed)") {
		t.Errorf("insert arg not derived from the same source as the bcrypt local:\n%s", code)
	}
	if strings.Contains(code, "string(hashed)") {
		t.Errorf("insert arg still uses the old fixed `hashed` local:\n%s", code)
	}
	// And the guard uses the model-scoped names.
	if !strings.Contains(code, "if len(secret) > vaultMaxPasswordBytes") {
		t.Errorf("length guard not derived from the parameter name:\n%s", code)
	}
}

// Two credential columns in one model must declare two distinct locals rather
// than redeclaring one.
func TestTwoCredentialColumnsDoNotCollide(t *testing.T) {
	names := createParamNames([]Field{
		{Name: "password", Type: "password"},
		{Name: "password_hash", Type: "password"},
	})
	if names[0] == names[1] {
		t.Fatalf("two credential columns produced one parameter name: %v", names)
	}
	if hashedVar(names[0]) == hashedVar(names[1]) {
		t.Fatalf("two credential columns produced one hash local: %v", names)
	}
}

// ─── item 3: the 72-byte cap ─────────────────────────────────────────────────

// bcrypt REJECTS input over 72 bytes rather than truncating, so without a
// guard the user who chose the longest passphrase got a 500 that retrying never
// fixed. The names are model-scoped because user_model.go.tmpl declares
// package-level ErrPasswordTooLong / maxPasswordBytes in the same package.
func TestModelEmitsThe72ByteCap(t *testing.T) {
	data := newData("member", credentialFields())
	data.CRUD = true
	code := renderModel(t, data)

	for _, want := range []string{
		"const memberMaxPasswordBytes = 72",
		"var ErrMemberPasswordTooLong = bcrypt.ErrPasswordTooLong",
		"if len(password) > memberMaxPasswordBytes",
		"return 0, ErrMemberPasswordTooLong", // Create
		"return ErrMemberPasswordTooLong",    // Update
	} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
	// The unscoped names belong to user_model.go.tmpl and would collide.
	for _, forbidden := range []string{"maxPasswordBytes = 72", "var ErrPasswordTooLong"} {
		if regexp.MustCompile(`(^|[^a-zA-Z])` + regexp.QuoteMeta(forbidden)).MatchString(code) {
			t.Errorf("emitted the unscoped name %q, which collides with models/User.go", forbidden)
		}
	}
}

// A model with no credential must not import bcrypt or emit the cap.
func TestOrdinaryModelIsUnchanged(t *testing.T) {
	code := renderModel(t, newData("project", []Field{
		{Name: "name", Type: "string"},
		{Name: "status", Type: "string"},
	}))
	for _, forbidden := range []string{"bcrypt", "MaxPasswordBytes", "PasswordTooLong", "json:\"-\""} {
		if strings.Contains(code, forbidden) {
			t.Errorf("a credential-free model emitted %q:\n%s", forbidden, code)
		}
	}
	if !strings.Contains(code, `projectAllowedColumns = []string{"id", "name", "status", "created_at"}`) {
		t.Errorf("ordinary model's whitelist changed:\n%s", code)
	}
}

// ─── items 4 and 5: the whitelist and the list query ─────────────────────────

func TestCredentialIsNotSortableFilterableOrSelected(t *testing.T) {
	data := newData("member", credentialFields())
	data.CRUD = true
	code := renderModel(t, data)

	// ?sort=password_hash was an ordering oracle over stored hashes, and
	// ?filter=password_hash:<guess> a confirmation oracle — with the guess
	// interpolated into the cache key on the way past.
	if !strings.Contains(code, `memberAllowedColumns = []string{"id", "label", "note", "created_at"}`) {
		t.Errorf("the credential is still sort/filterable:\n%s", code)
	}
	// GetPage specifically: Find() still selects every column, deliberately.
	if !strings.Contains(code, `query := "SELECT id, label, note, created_at FROM members"`) {
		t.Errorf("GetPage still selects the credential into the page cache:\n%s", code)
	}
	// ...and Find does still load it, or authentication could not work.
	if !strings.Contains(code, `"SELECT id, label, password_hash, note, created_at FROM members WHERE id = ?"`) {
		t.Errorf("Find no longer selects the credential; single-row reads still need it:\n%s", code)
	}
}

// The empty-public-column-list edge: a model whose ONLY field is a credential
// would render "SELECT id, , created_at" from a naive splice.
func TestCredentialOnlyModelRendersAValidQuery(t *testing.T) {
	code := renderModel(t, newData("vault", []Field{{Name: "secret_hash", Type: "password"}}))
	if strings.Contains(code, ", ,") {
		t.Errorf("empty public column list produced a malformed SELECT:\n%s", code)
	}
	if !strings.Contains(code, `query := "SELECT id, created_at FROM vaults"`) {
		t.Errorf("credential-only GetPage query is wrong:\n%s", code)
	}
	if !strings.Contains(code, `vaultAllowedColumns = []string{"id", "created_at"}`) {
		t.Errorf("credential-only whitelist is wrong:\n%s", code)
	}
	if !strings.Contains(code, "rows.Scan(&item.ID, &item.CreatedAt)") {
		t.Errorf("credential-only scan has a hole in its argument list:\n%s", code)
	}
}

// ─── item 6: the manifest ────────────────────────────────────────────────────

func TestFieldsToModelOmitsCredentialColumns(t *testing.T) {
	m := fieldsToModel("member", "members", credentialFields())
	var names []string
	for _, f := range m.Fields {
		names = append(names, f.Name)
	}
	want := []string{"id", "label", "note", "created_at"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("manifest fields = %v, want %v", names, want)
	}
}

// A credential-only model still keeps its implicit columns.
func TestFieldsToModelCredentialOnlyKeepsImplicitColumns(t *testing.T) {
	m := fieldsToModel("vault", "vaults", []Field{{Name: "secret_hash", Type: "password"}})
	var names []string
	for _, f := range m.Fields {
		names = append(names, f.Name)
	}
	if !reflect.DeepEqual(names, []string{"id", "created_at"}) {
		t.Errorf("manifest fields = %v, want [id created_at]", names)
	}
}

// THE ACTUAL INVARIANT: the manifest must describe the same field list the
// generated struct serializes. A field advertised in api.json but tagged
// json:"-" makes a generated typed client emit a REQUIRED field that appears in
// no response — a hard failure for a strict decoder, and invisible to
// inspect_app, which never compares struct tags to the manifest.
func TestManifestMatchesTheGeneratedStructOnTheWire(t *testing.T) {
	fields := credentialFields()
	code := renderModel(t, newData("member", fields))

	// Only the model struct itself — the <name>Page cache payload declared
	// below it carries json tags of its own ("items", "total") that are not
	// part of any model's wire surface.
	structBody := code[strings.Index(code, "type Member struct {"):]
	structBody = structBody[:strings.Index(structBody, "\n}")]

	serialized := map[string]bool{}
	for _, m := range regexp.MustCompile("`json:\"([^\"]+)\"`").FindAllStringSubmatch(structBody, -1) {
		if m[1] != "-" {
			serialized[m[1]] = true
		}
	}
	for _, f := range fieldsToModel("member", "members", fields).Fields {
		if !serialized[f.Name] {
			t.Errorf("api.json advertises %q but the struct never serializes it", f.Name)
		}
		delete(serialized, f.Name)
	}
	if len(serialized) != 0 {
		t.Errorf("the struct serializes fields the manifest omits: %v", serialized)
	}
}

// ─── item 7: scaffold_resource refuses a credential ──────────────────────────

func TestResourceTemplatesRefuseACredentialField(t *testing.T) {
	data := newData("member", credentialFields())
	data.CRUD = true
	data.Title = "Members"

	for _, tmpl := range []string{"resource_handlers.go.tmpl", "resource_handlers_test.go.tmpl"} {
		out := filepath.Join(t.TempDir(), "out.go")
		err := renderToFile(tmpl, out, data)
		if err == nil {
			t.Fatalf("%s rendered a credential-bearing CRUD handler — a PUT that omits "+
				"password_hash would blank the stored credential with bcrypt(\"\")", tmpl)
		}
		if !strings.Contains(err.Error(), "password_hash") {
			t.Errorf("%s: refusal does not name the column: %v", tmpl, err)
		}
		if !strings.Contains(err.Error(), "scaffold_auth") {
			t.Errorf("%s: refusal does not point at the remedy: %v", tmpl, err)
		}
		if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
			t.Errorf("%s left a partial file behind after refusing", tmpl)
		}
	}
}

// A MISDECLARED credential is refused too, with its own message — and this is
// the worse case: the generator would treat it as ordinary data and write a
// caller-supplied value into it verbatim, so an attacker stores the bcrypt hash
// of a password they already know and logs in with it.
func TestResourceTemplatesRefuseAMisdeclaredCredential(t *testing.T) {
	data := newData("member", []Field{
		{Name: "label", Type: "string"},
		{Name: "password_hash", Type: "string"},
	})
	data.CRUD = true
	err := renderToFile("resource_handlers.go.tmpl", filepath.Join(t.TempDir(), "out.go"), data)
	if err == nil {
		t.Fatal("a misdeclared credential column was accepted into a public CRUD write path")
	}
	if !strings.Contains(err.Error(), "COLUMN NAME says credential material") {
		t.Errorf("wrong refusal for a misdeclared column: %v", err)
	}
}

// The refusal is scoped: an ordinary resource must still scaffold.
func TestResourceTemplatesStillRenderWithoutACredential(t *testing.T) {
	data := newData("project", []Field{
		{Name: "name", Type: "string"},
		{Name: "status", Type: "string"},
	})
	data.CRUD = true
	data.Title = "Projects"
	for _, tmpl := range []string{"resource_handlers.go.tmpl", "resource_handlers_test.go.tmpl"} {
		if err := renderToFile(tmpl, filepath.Join(t.TempDir(), "out.go"), data); err != nil {
			t.Fatalf("%s refused an ordinary resource: %v", tmpl, err)
		}
	}
}

// scaffold_list and create_model must NOT be refused for a credential-bearing
// table: neither emits a write path a caller can reach, and refusing would make
// a legitimate digest column unscaffoldable.
func TestListTemplatesAreNotRefusedForACredentialTable(t *testing.T) {
	data := newData("member", credentialFields())
	data.Title = "Members"
	for _, tmpl := range []string{"list_handler.go.tmpl", "model.go.tmpl"} {
		if err := renderToFile(tmpl, filepath.Join(t.TempDir(), "out.go"), data); err != nil {
			t.Fatalf("%s refused a credential-bearing table; only the CRUD write path should: %v", tmpl, err)
		}
	}
}

// add_js_form: a real `password` field is legitimate (a sign-up form posting to
// a hand-written handler), a column named after a stored hash is not.
func TestAddJSFormRefusesAMisdeclaredCredentialButNotAPassword(t *testing.T) {
	bad := newData("signup", []Field{{Name: "password_hash", Type: "string"}})
	bad.APIEndpoint = "/api/v1/signup"
	bad.FormName = "signupForm"
	if err := renderToFile("js_form.js.tmpl", filepath.Join(t.TempDir(), "out.js"), bad); err == nil {
		t.Error("add_js_form put a stored-credential column in front of a user")
	}

	ok := newData("signup", []Field{
		{Name: "email", Type: "string"},
		{Name: "password", Type: "password"},
	})
	ok.APIEndpoint = "/api/v1/auth/register"
	ok.FormName = "signupForm"
	if err := renderToFile("js_form.js.tmpl", filepath.Join(t.TempDir(), "out.js"), ok); err != nil {
		t.Errorf("add_js_form refused a legitimate password field: %v", err)
	}
}

// ─── end to end: a misdeclared credential is never exposed ───────────────────

// The declaration is the thing a hurried author gets wrong, so the whole chain
// is asserted for a column that is credential material by NAME ONLY.
func TestMisdeclaredCredentialIsNeverExposed(t *testing.T) {
	fields := []Field{
		{Name: "label", Type: "string"},
		{Name: "password_hash", Type: "string"}, // declared string, named credential
		{Name: "note", Type: "string", Nullable: true},
	}
	code := renderModel(t, newData("member", fields))

	if !strings.Contains(code, "`json:\"-\"`") {
		t.Error("the misdeclared credential is not tagged json:\"-\"")
	}
	if !strings.Contains(code, `memberAllowedColumns = []string{"id", "label", "note", "created_at"}`) {
		t.Errorf("the misdeclared credential is still sort/filterable:\n%s", code)
	}
	if !strings.Contains(code, `query := "SELECT id, label, note, created_at FROM members"`) {
		t.Errorf("GetPage still selects the misdeclared credential into the page cache:\n%s", code)
	}
	// It is NOT bcrypt-hashed — only the declared type does that, because
	// hashing an already-hashed digest would break every lookup against it.
	if strings.Contains(code, "bcrypt") {
		t.Errorf("a misdeclared (string) column was bcrypt-hashed:\n%s", code)
	}
	// And it is off the manifest.
	blob, err := json.Marshal(fieldsToModel("member", "members", fields))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(blob), "password_hash") {
		t.Errorf("api.json advertises the misdeclared credential: %s", blob)
	}
}

// ─── the tool boundary ───────────────────────────────────────────────────────

// TestScaffoldResourceToolRefusesACredentialField exercises the FIRST of the
// two independent refusals — the one in handleScaffoldResource, before the
// templates are ever reached.
//
// The check runs before applySchema, so it returns without touching
// /data/app.db: a credential field is refused on the argument list alone.
func TestScaffoldResourceToolRefusesACredentialField(t *testing.T) {
	for _, c := range []struct {
		name   string
		fields []string
		want   string
	}{
		{"declared credential", []string{"label:string", "password:password"},
			"refuses the credential field(s) password"},
		{"credential not named password", []string{"label:string", "secret_hash:password"},
			"refuses the credential field(s) secret_hash"},
		{"misdeclared credential", []string{"label:string", "password_hash:string"},
			"refuses the field(s) password_hash"},
		{"widened list: api_key", []string{"label:string", "api_key:string"},
			"refuses the field(s) api_key"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res, err := handleScaffoldResource(context.Background(), scaffoldResourceReq("member", c.fields))
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			text := resultText(res)
			if !strings.Contains(text, c.want) {
				t.Fatalf("scaffold_resource did not refuse as expected.\ngot:  %s\nwant: %s", text, c.want)
			}
			if !strings.Contains(text, "scaffold_auth") {
				t.Errorf("refusal does not point at the remedy: %s", text)
			}
		})
	}
}

// An ordinary field list must still pass the credential check and proceed —
// here it reaches applySchema and fails on the missing table, which is proof it
// got PAST the refusal rather than being stopped by it.
func TestScaffoldResourceToolAcceptsOrdinaryFields(t *testing.T) {
	res, err := handleScaffoldResource(context.Background(),
		scaffoldResourceReq("project", []string{"name:string", "status:string"}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := resultText(res)
	if strings.Contains(text, "refuses") {
		t.Fatalf("scaffold_resource refused an ordinary field list: %s", text)
	}
}

func scaffoldResourceReq(name string, fields []string) mcp.CallToolRequest {
	raw := make([]interface{}, len(fields))
	for i, f := range fields {
		raw[i] = f
	}
	var req mcp.CallToolRequest
	req.Params.Name = "scaffold_resource"
	req.Params.Arguments = map[string]interface{}{"name": name, "fields": raw}
	return req
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
