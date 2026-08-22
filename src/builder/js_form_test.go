package main

import (
	"regexp"
	"strings"
	"testing"
)

// jsIdentRE is the shape a JavaScript function name must have. The generated
// module is an ES module, so anything outside this set is not a cosmetic
// problem: the parse error takes down every export in the file.
var jsIdentRE = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func TestFormNameFor(t *testing.T) {
	cases := []struct {
		endpoint string
		page     string
		want     string
	}{
		// The regression: a nested path put a literal "/" in the identifier.
		{"/api/v1/admin/trainers", "admin", "AdminTrainers"},
		// Same class, same fix — a hyphenated resource.
		{"/api/v1/work-orders", "work_orders", "WorkOrders"},
		// Already-valid names must not churn; these are byte-for-byte what
		// toPascal produced before, so committed modules keep their names.
		{"/api/v1/trainers", "trainers", "Trainers"},
		{"/api/v1/client_notes", "clients", "ClientNotes"},
		{"/api/v1/clients", "clients", "Clients"},
		// A leading digit is legal in a path segment, never in an identifier.
		{"/api/v1/2fa_codes", "settings", "N2faCodes"},
		// Unversioned and bare paths still resolve to the resource.
		{"/api/trainers", "admin", "Trainers"},
		{"trainers", "admin", "Trainers"},
		// Nothing usable in the endpoint: fall back to the page name, with no
		// doubled "Form" (the template supplies that suffix itself).
		{"/api/v1/", "clients", "Clients"},
		{"", "clients", "Clients"},
		// Nothing usable anywhere: the handler turns this into an error.
		{"/api/v1/", "", ""},
	}
	for _, c := range cases {
		got := formNameFor(c.endpoint, c.page)
		if got != c.want {
			t.Errorf("formNameFor(%q, %q) = %q, want %q", c.endpoint, c.page, got, c.want)
		}
		if got != "" && !jsIdentRE.MatchString(got) {
			t.Errorf("formNameFor(%q, %q) = %q, which is not a valid JS identifier", c.endpoint, c.page, got)
		}
	}
}

// TestRenderJSForm_EmitsValidIdentifier renders the template the way
// handleAddJSForm does and checks the two places the name lands: the function
// declaration in the template output, and the call injected at the
// // @inject-forms marker. Both were broken by the same bad name.
func TestRenderJSForm_EmitsValidIdentifier(t *testing.T) {
	for _, endpoint := range []string{
		"/api/v1/admin/trainers",
		"/api/v1/work-orders",
		"/api/v1/clients",
	} {
		formName := formNameFor(endpoint, "admin")
		data := newData("admin", parseFields([]string{"name:string", "email:string"}))
		data.APIEndpoint = endpoint
		data.SubmitLabel = "Save"
		data.FormName = formName

		out, err := renderToString("js_form.js.tmpl", data)
		if err != nil {
			t.Fatalf("render js_form.js.tmpl for %q: %v", endpoint, err)
		}

		decl := "function setup" + formName + "Form(container)"
		if !strings.Contains(out, decl) {
			t.Errorf("%s: rendered form does not declare %q\n---\n%s", endpoint, decl, out)
		}
		// This is the assertion that would have caught the original bug: the
		// declared name, read back out of the rendered source.
		m := regexp.MustCompile(`function setup(\S*?)Form\(`).FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("%s: no setup...Form declaration in rendered output", endpoint)
		}
		if !jsIdentRE.MatchString("setup" + m[1] + "Form") {
			t.Errorf("%s: emitted %q, which is not a valid JS identifier", endpoint, "setup"+m[1]+"Form")
		}

		call := "setup" + formName + "Form(document.getElementById('forms-container'));"
		if !jsIdentRE.MatchString("setup" + formName + "Form") {
			t.Errorf("%s: injected call %q is not valid JS", endpoint, call)
		}
	}
}
