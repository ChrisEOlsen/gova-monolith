package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type decoded struct {
	OK     bool              `json:"ok"`
	Data   json.RawMessage   `json:"data"`
	Meta   *Meta             `json:"meta"`
	Error  string            `json:"error"`
	Code   string            `json:"code"`
	Fields map[string]string `json:"fields"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) decoded {
	t.Helper()
	var d decoded
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return d
}

// The defect this guards: a nil slice inside a non-nil interface is not
// "empty" for encoding/json's omitempty, so it marshals as null. JS shrugs
// via `res.data ?? []`; a Swift decoder expecting [Item] throws.
func TestJSONOK_NilSliceRendersAsEmptyArray(t *testing.T) {
	var items []string
	rec := httptest.NewRecorder()
	jsonOK(rec, items)

	d := decode(t, rec)
	if !d.OK {
		t.Error("ok: got false, want true")
	}
	if string(d.Data) != "[]" {
		t.Errorf("data: got %s, want []", d.Data)
	}
}

func TestJSONOK_NonSliceDataUnaffected(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonOK(rec, map[string]int{"n": 1})

	d := decode(t, rec)
	if string(d.Data) != `{"n":1}` {
		t.Errorf("data: got %s, want {\"n\":1}", d.Data)
	}
}

func TestJSONList_IncludesMeta(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonList(rec, []string{"a"}, Meta{Limit: 50, Offset: 0, Total: 123})

	d := decode(t, rec)
	if d.Meta == nil {
		t.Fatal("meta: got nil, want populated")
	}
	if d.Meta.Limit != 50 || d.Meta.Offset != 0 || d.Meta.Total != 123 {
		t.Errorf("meta: got %+v, want {50 0 123}", *d.Meta)
	}
}

func TestJSONList_EmptyPageRendersAsEmptyArray(t *testing.T) {
	var items []string
	rec := httptest.NewRecorder()
	jsonList(rec, items, Meta{Limit: 50, Offset: 0, Total: 0})

	d := decode(t, rec)
	if string(d.Data) != "[]" {
		t.Errorf("data: got %s, want []", d.Data)
	}
	if d.Meta == nil || d.Meta.Total != 0 {
		t.Errorf("meta: got %+v, want total 0", d.Meta)
	}
}

// jsonError keeps its original three-argument signature — every
// already-generated handler calls it this way.
func TestJSONError_DerivesCodeFromStatus(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "unauthorized"},
		{http.StatusForbidden, "forbidden"},
		{http.StatusNotFound, "not_found"},
		{http.StatusConflict, "conflict"},
		{http.StatusUnprocessableEntity, "validation_failed"},
		{http.StatusTooManyRequests, "rate_limited"},
		{http.StatusInternalServerError, "internal"},
		{http.StatusTeapot, "internal"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		jsonError(rec, "boom", tc.status)

		if rec.Code != tc.status {
			t.Errorf("status %d: got HTTP %d", tc.status, rec.Code)
		}
		d := decode(t, rec)
		if d.OK {
			t.Errorf("status %d: ok got true, want false", tc.status)
		}
		if d.Error != "boom" {
			t.Errorf("status %d: error got %q, want \"boom\"", tc.status, d.Error)
		}
		if d.Code != tc.want {
			t.Errorf("status %d: code got %q, want %q", tc.status, d.Code, tc.want)
		}
	}
}

func TestJSONErrorCode_UsesExplicitCode(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonErrorCode(rec, CodeConflict, "already exists", http.StatusBadRequest)

	d := decode(t, rec)
	if d.Code != "conflict" {
		t.Errorf("code: got %q, want %q", d.Code, "conflict")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestJSONValidationError(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonValidationError(rec, map[string]string{"name": "required", "email": "invalid"})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", rec.Code)
	}
	d := decode(t, rec)
	if d.Code != "validation_failed" {
		t.Errorf("code: got %q, want %q", d.Code, "validation_failed")
	}
	if d.Fields["name"] != "required" || d.Fields["email"] != "invalid" {
		t.Errorf("fields: got %v", d.Fields)
	}
	// Summary is built from the alphabetically first field so it is stable.
	if d.Error != "email: invalid" {
		t.Errorf("error: got %q, want \"email: invalid\"", d.Error)
	}
}

func TestContentTypeAlwaysJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonOK(rec, nil)
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", got)
	}
}

// TestCodeConstants_PinWireContract ensures the error code constants match
// their wire contract values. These strings are client-visible and part of
// the native mobile client's decoding logic — renaming a constant's value
// without updating this test would silently break the client.
func TestCodeConstants_PinWireContract(t *testing.T) {
	cases := []struct {
		constant string
		want     string
	}{
		{CodeUnauthorized, "unauthorized"},
		{CodeForbidden, "forbidden"},
		{CodeNotFound, "not_found"},
		{CodeConflict, "conflict"},
		{CodeValidationFailed, "validation_failed"},
		{CodeRateLimited, "rate_limited"},
		{CodeInternal, "internal"},
	}
	for _, tc := range cases {
		if tc.constant != tc.want {
			t.Errorf("constant got %q, want %q", tc.constant, tc.want)
		}
	}
}
