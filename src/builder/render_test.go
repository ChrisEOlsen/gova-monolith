package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// renderAndParse renders tmplName with data and verifies the output is
// syntactically valid Go. It does not type-check or resolve imports — full
// compilation is checked once, end-to-end, in Task 10.
func renderAndParse(t *testing.T, tmplName string, data TemplateData) string {
	t.Helper()
	out, err := renderToString(tmplName, data)
	if err != nil {
		t.Fatalf("render %s: %v", tmplName, err)
	}
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, tmplName, out, parser.AllErrors); err != nil {
		t.Fatalf("render %s: output is not valid Go: %v\n---\n%s", tmplName, err, out)
	}
	return out
}

func sampleFields() []Field {
	return []Field{
		{Name: "title", Type: "string"},
		{Name: "count", Type: "int"},
		{Name: "active", Type: "boolean"},
	}
}

func TestRenderAndParse_ExistingTemplateIsValidGo(t *testing.T) {
	data := newData("widget", sampleFields())
	renderAndParse(t, "model.go.tmpl", data)
}

func TestModelTestTemplate_IsValidGo(t *testing.T) {
	data := newData("widget", sampleFields())
	renderAndParse(t, "model_test.go.tmpl", data)
}

func TestListHandlerTestTemplate_IsValidGo(t *testing.T) {
	data := newData("widget", sampleFields())
	renderAndParse(t, "list_handler_test.go.tmpl", data)
}

func TestAuthTestTemplate_IsValidGo(t *testing.T) {
	data := newData("user", nil)
	renderAndParse(t, "auth_test.go.tmpl", data)
}

func TestRegisterTestTemplate_IsValidGo(t *testing.T) {
	data := newData("user", nil)
	renderAndParse(t, "register_test.go.tmpl", data)
}

func TestMobileAuthTestTemplate_IsValidGo(t *testing.T) {
	renderAndParse(t, "mobile_auth_test.go.tmpl", TemplateData{})
}

func sampleFieldsWithNullable() []Field {
	return []Field{
		{Name: "title", Type: "string", Nullable: false},
		{Name: "notes", Type: "string", Nullable: true},
		{Name: "count", Type: "int", Nullable: false},
		{Name: "score", Type: "int", Nullable: true},
	}
}

func TestModelTemplate_NullableFieldIsPointer(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", data)

	if !strings.Contains(out, "Notes *string `json:\"notes\"`") {
		t.Errorf("nullable field is not a pointer:\n%s", out)
	}
	if !strings.Contains(out, "Title string `json:\"title\"`") {
		t.Errorf("non-nullable field should not be a pointer:\n%s", out)
	}
	if !strings.Contains(out, "var notesNull sql.NullString") {
		t.Errorf("missing sql.NullString temporary:\n%s", out)
	}
	if !strings.Contains(out, "item.Notes = &notesNull.String") {
		t.Errorf("missing nullable assignment:\n%s", out)
	}

	// A nullable non-string field must route through its own sql.Null*
	// wrapper and accessor — a typo in nullTypeFor/nullFieldFor's int
	// branch would otherwise ship uncaught, since the only other nullable
	// sample field is a string.
	if !strings.Contains(out, "Score *int64 `json:\"score\"`") {
		t.Errorf("nullable int field is not a *int64 pointer:\n%s", out)
	}
	if !strings.Contains(out, "var scoreNull sql.NullInt64") {
		t.Errorf("missing sql.NullInt64 temporary:\n%s", out)
	}
	if !strings.Contains(out, "item.Score = &scoreNull.Int64") {
		t.Errorf("missing nullable int assignment:\n%s", out)
	}
}

func TestModelTemplate_UsesGovaTime(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", data)

	if !strings.Contains(out, "CreatedAt Time `json:\"created_at\"`") {
		t.Errorf("CreatedAt should use models.Time:\n%s", out)
	}
}

func TestModelTemplate_GetPageReplacesGetAll(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", data)

	if !strings.Contains(out, "func (m *WidgetModel) GetPage(limit, offset int) ([]Widget, int, error)") {
		t.Errorf("missing GetPage signature:\n%s", out)
	}
	if strings.Contains(out, "func (m *WidgetModel) GetAll(") {
		t.Errorf("GetAll should be gone:\n%s", out)
	}
	if !strings.Contains(out, "items := []Widget{}") {
		t.Errorf("slice must be initialized non-nil:\n%s", out)
	}
	if !strings.Contains(out, "SELECT COUNT(*) FROM widgets") {
		t.Errorf("missing total count query:\n%s", out)
	}
}

func TestModelTemplate_CreateTakesPointerForNullable(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	out := renderAndParse(t, "model.go.tmpl", data)

	if !strings.Contains(out, "Create(title string, notes *string, count int64, score *int64)") {
		t.Errorf("Create should take a pointer for the nullable field:\n%s", out)
	}
}

func TestModelTestTemplate_NullableIsValidGo(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	renderAndParse(t, "model_test.go.tmpl", data)
}
