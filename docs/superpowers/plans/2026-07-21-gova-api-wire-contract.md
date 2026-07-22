# GOVA API Wire Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the generated GOVA JSON API a strict, self-describing contract so a typed native client (Swift) can consume it without inference, guesswork, or defensive parsing.

**Architecture:** All changes land in two places — the shared runtime helpers under `src/app/` (envelope, timestamp type, paging helper, version endpoint) and the code generator under `src/builder/` (schema introspection plus the templates that emit models and handlers). Generated application code is an output, never edited by hand. The web client changes in exactly one place (`api.js`), by design: the envelope was shaped to keep every existing consumer working.

**Tech Stack:** Go 1.x (`net/http`, `chi`, `database/sql`, `mattn/go-sqlite3`), `text/template` code generation, `mark3labs/mcp-go` for the MCP tool surface, vanilla ES modules on the client, Docker Compose.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-07-21-gova-api-wire-contract-design.md` — authoritative for every decision below.
- **Branch:** `build/api-wire-contract`. Never use a git worktree in this repo — the `gova-builder` MCP container's bind mounts are path-bound (see `CLAUDE.md`).
- **Timestamps** serialize as RFC3339, UTC, second precision. No fractional seconds, ever.
- **Error codes** are exactly: `unauthorized` (401), `forbidden` (403), `not_found` (404), `conflict` (409), `validation_failed` (422), `rate_limited` (429), `internal` (500 and any unmapped status).
- **Pagination:** `limit` default 50, minimum 1, maximum 200. `offset` minimum 0. Invalid or absent values fall back to the default.
- **`jsonError(w, msg, status)` must keep its exact existing signature.** Every already-generated handler calls it. Breaking it is a plan failure.
- **No raw SQL in handlers** — model methods only. **No HTML from Go handlers** — JSON only. **No `innerHTML`** in JS. These are enforced by `runPatternChecks()` in `src/builder/main.go` and will fail scaffolding if violated.
- **No Node.js, no npm, no JS test runner.** Client-side changes are verified by hand or via `curl`.
- Go tests run two places and **both** are required: `docker compose exec app go test ./...` and `cd src/builder && go test ./...`.

---

## File Structure

**New files:**

| File | Responsibility |
|---|---|
| `src/app/models/time.go` | `models.Time` — the single definition of the timestamp wire format |
| `src/app/models/time_test.go` | Marshal, Scan, and Value coverage for the above |
| `src/app/handlers/version.go` | API version constants and the `_version` endpoint |
| `src/app/handlers/json_test.go` | Envelope, error-code mapping, and nil-slice guard coverage |
| `src/app/handlers/paging.go` | `queryInt` plus the paging bounds constants |
| `src/app/handlers/paging_test.go` | Clamping coverage for the above |
| `src/builder/schema.go` | `PRAGMA table_info` introspection and the field/schema mismatch guard |
| `src/builder/schema_test.go` | Introspection and guard coverage against a temp SQLite database |

**Modified files:**

| File | Change |
|---|---|
| `src/app/handlers/json.go` | Envelope fields, code mapping, nil-slice normalization, new helpers |
| `src/app/main.go` | `// @gova-routes` marker, `_version` route |
| `src/app/static/js/lib/api.js` | `get()` stops discarding the server error body |
| `src/builder/main.go` | `Field.Nullable`, pointer-aware funcMap helpers, guard wiring, `/api/v1` output strings |
| `src/builder/render_test.go` | Golden assertions for nullable, `GetPage`, `Time`, `/api/v1` |
| `src/builder/templates/model.go.tmpl` | `Time`, nullable pointers, `GetPage`, non-nil slice init |
| `src/builder/templates/model_test.go.tmpl` | `NOT NULL` in the fixture schema, `GetPage` coverage |
| `src/builder/templates/list_handler.go.tmpl` | Query-param clamping, `jsonList` |
| `src/builder/templates/list_handler_test.go.tmpl` | Clamping, empty-list, and meta coverage |
| `src/builder/templates/list_page.js.tmpl` | `/api/v1` path |
| `src/builder/templates/js_form.js.tmpl` | `/api/v1` path |
| `src/builder/templates/login.js.tmpl` | `/api/v1` path |
| `src/builder/templates/register.js.tmpl` | `/api/v1` path |
| `src/builder/templates/mobile_auth_handler.go.tmpl` | `/api/v1` in doc comments |
| `src/builder/templates/auth_test.go.tmpl` | `/api/v1` request paths |
| `src/builder/templates/register_test.go.tmpl` | `/api/v1` request paths |
| `src/builder/templates/mobile_auth_test.go.tmpl` | `/api/v1` request paths |
| `CLAUDE.md` | `GetPage`, envelope, error codes, `/api/v1` |
| `../gova-ios/CLAUDE.md` | Step 2 mobile auth paths |

---

## Task 1: `models.Time` — pin the timestamp wire format

**Files:**
- Create: `src/app/models/time.go`
- Test: `src/app/models/time_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type Time time.Time` in package `models`, implementing `json.Marshaler`, `json.Unmarshaler`, `sql.Scanner`, and `driver.Valuer`. Task 5's `model.go.tmpl` declares `CreatedAt Time` and scans directly into it.

**Context:** `src/app/models/` currently holds only `.gitkeep` — this task creates the package. `db.OpenTest(t, schema)` (in `src/app/db/testutil.go`) is available for any test needing a database, though this task does not need one.

- [ ] **Step 1: Write the failing test**

Create `src/app/models/time_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTime_MarshalJSON_DropsFractionalSeconds(t *testing.T) {
	ts := Time(time.Date(2026, 7, 21, 18, 45, 0, 123456789, time.UTC))
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `"2026-07-21T18:45:00Z"`
	if string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}

func TestTime_MarshalJSON_NormalizesToUTC(t *testing.T) {
	zone := time.FixedZone("CEST", 2*60*60)
	ts := Time(time.Date(2026, 7, 21, 20, 45, 0, 0, zone))
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `"2026-07-21T18:45:00Z"`
	if string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}

func TestTime_UnmarshalJSON_RoundTrips(t *testing.T) {
	var got Time
	if err := json.Unmarshal([]byte(`"2026-07-21T18:45:00Z"`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)
	if !time.Time(got).Equal(want) {
		t.Errorf("got %v, want %v", time.Time(got), want)
	}
}

func TestTime_Scan(t *testing.T) {
	want := time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)
	cases := []struct {
		name string
		src  any
	}{
		{"time.Time", want},
		{"string SQLite CURRENT_TIMESTAMP", "2026-07-21 18:45:00"},
		{"string RFC3339", "2026-07-21T18:45:00Z"},
		{"bytes RFC3339", []byte("2026-07-21T18:45:00Z")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Time
			if err := got.Scan(tc.src); err != nil {
				t.Fatalf("Scan(%v): %v", tc.src, err)
			}
			if !time.Time(got).Equal(want) {
				t.Errorf("got %v, want %v", time.Time(got), want)
			}
		})
	}
}

func TestTime_Scan_Nil(t *testing.T) {
	var got Time
	if err := got.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if !time.Time(got).IsZero() {
		t.Errorf("got %v, want zero time", time.Time(got))
	}
}

func TestTime_Scan_Unsupported(t *testing.T) {
	var got Time
	if err := got.Scan(42); err == nil {
		t.Error("Scan(int): expected error, got nil")
	}
}

func TestTime_Value(t *testing.T) {
	ts := Time(time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC))
	v, err := ts.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	got, ok := v.(time.Time)
	if !ok {
		t.Fatalf("Value: got %T, want time.Time", v)
	}
	if !got.Equal(time.Time(ts)) {
		t.Errorf("got %v, want %v", got, time.Time(ts))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `docker compose exec app go test ./models/... -v`

Expected: FAIL — `undefined: Time`.

- [ ] **Step 3: Write the implementation**

Create `src/app/models/time.go`:

```go
package models

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Time wraps time.Time to pin the JSON wire format to RFC3339 in UTC with
// second precision.
//
// Go's default time.Time marshaling emits RFC3339Nano. Swift's built-in
// .iso8601 decoding strategy rejects fractional seconds outright, so a
// default-marshaled timestamp fails to decode on iOS while parsing fine in
// JavaScript — the exact class of asymmetry this type exists to remove.
//
// Sub-second precision is discarded. SQLite's CURRENT_TIMESTAMP, the only
// timestamp source in generated schemas, has one-second resolution anyway.
type Time time.Time

func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(time.Time(t).UTC().Format(time.RFC3339))), nil
}

func (t *Time) UnmarshalJSON(b []byte) error {
	s, err := strconv.Unquote(string(b))
	if err != nil {
		return fmt.Errorf("models.Time: %w", err)
	}
	if s == "" {
		*t = Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("models.Time: %w", err)
	}
	*t = Time(parsed)
	return nil
}

// scanLayouts covers every shape the SQLite driver can hand back for a
// DATETIME column: a driver-parsed time.Time, or a raw string when the
// column's declared type does not trigger driver-side parsing.
var scanLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// Scan implements sql.Scanner. Every generated model scans created_at
// directly into this type, so it must accept all of the above.
func (t *Time) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*t = Time{}
		return nil
	case time.Time:
		*t = Time(v)
		return nil
	case string:
		return t.parseString(v)
	case []byte:
		return t.parseString(string(v))
	}
	return fmt.Errorf("models.Time: cannot scan %T", src)
}

func (t *Time) parseString(s string) error {
	s = strings.TrimSpace(s)
	for _, layout := range scanLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			*t = Time(parsed)
			return nil
		}
	}
	return fmt.Errorf("models.Time: cannot parse %q", s)
}

// Value implements driver.Valuer.
func (t Time) Value() (driver.Value, error) {
	return time.Time(t).UTC(), nil
}

func (t Time) String() string {
	return time.Time(t).UTC().Format(time.RFC3339)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `docker compose exec app go test ./models/... -v`

Expected: PASS — all seven test functions.

- [ ] **Step 5: Commit**

```bash
git add src/app/models/time.go src/app/models/time_test.go
git commit -m "feat: models.Time pins timestamps to RFC3339 UTC second precision"
```

---

## Task 2: Envelope, error codes, and the nil-slice guard

**Files:**
- Modify: `src/app/handlers/json.go` (full rewrite, 27 lines)
- Modify: `src/app/static/js/lib/api.js:3-9` (the `get` function)
- Test: `src/app/handlers/json_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces, in package `handlers`:
  - `type Meta struct { Limit, Offset, Total int }` with json tags `limit`, `offset`, `total`
  - `jsonOK(w http.ResponseWriter, data any)` — signature unchanged
  - `jsonError(w http.ResponseWriter, msg string, status int)` — **signature unchanged**, now derives `code`
  - `jsonErrorCode(w http.ResponseWriter, code, msg string, status int)`
  - `jsonValidationError(w http.ResponseWriter, fields map[string]string)`
  - `jsonList(w http.ResponseWriter, items any, meta Meta)` — used by Task 6's list handler template
  - Exported code constants: `CodeUnauthorized`, `CodeForbidden`, `CodeNotFound`, `CodeConflict`, `CodeValidationFailed`, `CodeRateLimited`, `CodeInternal`

- [ ] **Step 1: Write the failing test**

Create `src/app/handlers/json_test.go`:

```go
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
		{http.StatusUnauthorized, CodeUnauthorized},
		{http.StatusForbidden, CodeForbidden},
		{http.StatusNotFound, CodeNotFound},
		{http.StatusConflict, CodeConflict},
		{http.StatusUnprocessableEntity, CodeValidationFailed},
		{http.StatusTooManyRequests, CodeRateLimited},
		{http.StatusInternalServerError, CodeInternal},
		{http.StatusTeapot, CodeInternal},
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
	if d.Code != CodeConflict {
		t.Errorf("code: got %q, want %q", d.Code, CodeConflict)
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
	if d.Code != CodeValidationFailed {
		t.Errorf("code: got %q, want %q", d.Code, CodeValidationFailed)
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `docker compose exec app go test ./handlers/... -run 'TestJSON|TestContentType' -v`

Expected: FAIL — `undefined: Meta`, `undefined: jsonList`, `undefined: CodeUnauthorized`.

- [ ] **Step 3: Rewrite `src/app/handlers/json.go`**

Replace the entire file with:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
)

// Meta carries list-window information alongside a paginated response.
type Meta struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

// envelope is the single response shape for every JSON endpoint.
//
// error stays a plain string so existing consumers (api.js, the generated JS
// modules, and the iOS APIClient) keep working unchanged; code and fields are
// purely additive, for clients that want to branch on failure kind.
type envelope struct {
	OK     bool              `json:"ok"`
	Data   any               `json:"data,omitempty"`
	Meta   *Meta             `json:"meta,omitempty"`
	Error  string            `json:"error,omitempty"`
	Code   string            `json:"code,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

// Machine-readable failure kinds. This list is closed — clients switch on it.
const (
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeValidationFailed = "validation_failed"
	CodeRateLimited      = "rate_limited"
	CodeInternal         = "internal"
)

func codeForStatus(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeConflict
	case http.StatusUnprocessableEntity:
		return CodeValidationFailed
	case http.StatusTooManyRequests:
		return CodeRateLimited
	default:
		return CodeInternal
	}
}

// normalizeData replaces a nil slice with an empty one.
//
// encoding/json marshals a nil slice held in a non-nil interface as null, not
// [] — and omitempty does not strip it, because the interface itself is not
// nil. A strict client decoding an array then fails on an empty result set.
// Generated models also initialize their slices non-nil; this is the second
// guard, covering hand-written handlers the templates cannot reach.
func normalizeData(data any) any {
	if data == nil {
		return nil
	}
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Slice && v.IsNil() {
		return reflect.MakeSlice(v.Type(), 0, 0).Interface()
	}
	return data
}

func writeJSON(w http.ResponseWriter, status int, env envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(env)
}

func jsonOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, envelope{OK: true, Data: normalizeData(data)})
}

// jsonList is the paginated counterpart to jsonOK.
func jsonList(w http.ResponseWriter, items any, meta Meta) {
	writeJSON(w, http.StatusOK, envelope{OK: true, Data: normalizeData(items), Meta: &meta})
}

// jsonError keeps its original signature — every generated handler calls it
// with exactly these three arguments. The code is derived from the status.
func jsonError(w http.ResponseWriter, msg string, status int) {
	jsonErrorCode(w, codeForStatus(status), msg, status)
}

// jsonErrorCode sets the code explicitly, for cases where the HTTP status
// does not imply the failure kind on its own.
func jsonErrorCode(w http.ResponseWriter, code, msg string, status int) {
	writeJSON(w, status, envelope{OK: false, Error: msg, Code: code})
}

// jsonValidationError responds 422 with a per-field failure map.
func jsonValidationError(w http.ResponseWriter, fields map[string]string) {
	writeJSON(w, http.StatusUnprocessableEntity, envelope{
		OK:     false,
		Error:  summarizeFields(fields),
		Code:   CodeValidationFailed,
		Fields: fields,
	})
}

// summarizeFields builds the human-readable error string from the
// alphabetically first field, so the message is deterministic across runs
// rather than dependent on Go's randomized map iteration order.
func summarizeFields(fields map[string]string) string {
	if len(fields) == 0 {
		return "validation failed"
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys[0] + ": " + fields[keys[0]]
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `docker compose exec app go test ./handlers/... -v`

Expected: PASS. The pre-existing `home.go` handler calls `jsonOK` and must still compile — the whole package building is part of this check.

- [ ] **Step 5: Fix `get()` in `src/app/static/js/lib/api.js`**

`get()` currently discards the server's response body on any non-2xx status other than 401, which would throw away the new `code` and `fields`. Replace lines 3-9:

```js
export async function get(path) {
  const res = await fetch(path, { credentials: 'same-origin' });
  try {
    return await res.json();
  } catch {
    // Body was not JSON (proxy error page, network truncation) — synthesize
    // an envelope so callers never have to branch on shape.
    return { ok: false, error: `HTTP ${res.status}`, code: 'internal' };
  }
}
```

`post`, `put`, and `del` already return the parsed body unmodified, so `code` and `fields` reach callers with no change. Leave them alone.

- [ ] **Step 6: Verify the client change by hand**

There is no JS test runner in this stack (Critical Constraint 4). Confirm by reading: `get()` no longer has an early `if (!res.ok ...)` return, and `post`/`put`/`del` are untouched.

Run: `grep -n "res.status" src/app/static/js/lib/api.js`

Expected: exactly one match, inside the `catch` block.

- [ ] **Step 7: Commit**

```bash
git add src/app/handlers/json.go src/app/handlers/json_test.go src/app/static/js/lib/api.js
git commit -m "feat: structured error codes, meta, and nil-slice guard in envelope"
```

---

## Task 3: Version endpoint and the route marker

**Files:**
- Create: `src/app/handlers/version.go`
- Modify: `src/app/main.go:47-52` (the generated-routes comment block)
- Test: `src/app/handlers/version_test.go` (create)

**Interfaces:**
- Consumes: `jsonOK` from Task 2.
- Produces: `handlers.APIVersion`, `handlers.MinClientVersion` (string constants), `handlers.VersionGET() http.HandlerFunc`.

**Context:** The `// @gova-routes` marker has no behavior in this build. It replaces the prose comment so Build 2's auto-registration has a stable anchor to write against — a zero-risk prerequisite worth landing now.

- [ ] **Step 1: Write the failing test**

Create `src/app/handlers/version_test.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/_version", nil)
	rec := httptest.NewRecorder()

	VersionGET()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			APIVersion       string `json:"api_version"`
			MinClientVersion string `json:"min_client_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if !body.OK {
		t.Error("ok: got false, want true")
	}
	if body.Data.APIVersion != APIVersion {
		t.Errorf("api_version: got %q, want %q", body.Data.APIVersion, APIVersion)
	}
	if body.Data.MinClientVersion != MinClientVersion {
		t.Errorf("min_client_version: got %q, want %q", body.Data.MinClientVersion, MinClientVersion)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `docker compose exec app go test ./handlers/... -run TestVersionGET -v`

Expected: FAIL — `undefined: VersionGET`.

- [ ] **Step 3: Write the implementation**

Create `src/app/handlers/version.go`:

```go
package handlers

import "net/http"

// APIVersion identifies the contract this server speaks. MinClientVersion is
// the oldest client build it will still serve correctly.
//
// A path prefix alone does nothing for a stale App Store build — it only makes
// a future /api/v2 possible. This endpoint is the signal that turns an opaque
// client-side decode failure into an actionable "update required" prompt.
//
// Bumped by hand for now. Build 2's API manifest becomes the natural owner
// once it can hash the route and model set.
const (
	APIVersion       = "1.0.0"
	MinClientVersion = "1.0.0"
)

type versionInfo struct {
	APIVersion       string `json:"api_version"`
	MinClientVersion string `json:"min_client_version"`
}

// VersionGET handles GET /api/v1/_version
func VersionGET() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonOK(w, versionInfo{APIVersion: APIVersion, MinClientVersion: MinClientVersion})
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `docker compose exec app go test ./handlers/... -run TestVersionGET -v`

Expected: PASS.

- [ ] **Step 5: Wire the route and add the marker**

In `src/app/main.go`, replace this block:

```go
	// Generated API routes registered here by MCP tools
	// Use database.Read for GET handlers, database.Write for POST handlers
	// Example:
	//   r.Post("/api/auth/login",  handlers.LoginPOST(database.Read, database.Write, appCache))
	//   r.Post("/api/auth/logout", handlers.LogoutPOST())
	//   r.Get("/api/auth/me",      handlers.MeGET(database.Read, database.Write, appCache))
```

with:

```go
	// API
	r.Get("/api/v1/_version", handlers.VersionGET())

	// Generated API routes registered here by MCP tools.
	// Use database.Read for GET handlers, database.Write for POST handlers.
	// Example:
	//   r.Post("/api/v1/auth/login",  handlers.LoginPOST(database.Read, database.Write, appCache))
	//   r.Post("/api/v1/auth/logout", handlers.LogoutPOST())
	//   r.Get("/api/v1/auth/me",      handlers.MeGET(database.Read, database.Write, appCache))
	// @gova-routes
```

- [ ] **Step 6: Verify the endpoint end-to-end**

```bash
docker compose restart app
sleep 3
curl -s localhost:8080/api/v1/_version
```

Expected: `{"ok":true,"data":{"api_version":"1.0.0","min_client_version":"1.0.0"}}`

- [ ] **Step 7: Commit**

```bash
git add src/app/handlers/version.go src/app/handlers/version_test.go src/app/main.go
git commit -m "feat: /api/v1/_version endpoint and @gova-routes marker"
```

---

## Task 4: Schema introspection and the mismatch guard

**Files:**
- Create: `src/builder/schema.go`
- Create: `src/builder/schema_test.go`
- Modify: `src/builder/main.go` — add `Nullable` to `Field` (around line 187), wire the guard into `handleCreateModel` and `handleScaffoldList`

**Interfaces:**
- Consumes: `Field`, `isSafeIdent`, `toPlural`, `errResult` from `src/builder/main.go`.
- Produces:
  - `Field` gains `Nullable bool`
  - `applySchemaAt(dsn, table string, fields []Field) ([]Field, error)` — testable against any database
  - `applySchema(table string, fields []Field) ([]Field, error)` — calls the above with the production `sqliteDSN`
  - Task 5's templates read `.Nullable` on every field.

**Context:** `PRAGMA table_info(x)` cannot take a bound parameter, so the table name is interpolated. This is safe **only because** `isSafeIdent` (`^[a-zA-Z0-9_]+$`) has already validated the model name, and `toPlural` cannot introduce unsafe characters. The guard re-validates defensively anyway. This ordering is mandatory — do not interpolate before validating.

- [ ] **Step 1: Write the failing test**

Create `src/builder/schema_test.go`:

```go
package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func testDSN(t *testing.T, schema string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	dsn := "file:" + path + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return dsn
}

const widgetSchema = `CREATE TABLE widgets (
	id INTEGER PRIMARY KEY,
	title TEXT NOT NULL,
	notes TEXT,
	count INTEGER NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`

func TestApplySchema_MarksNullableFields(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{
		{Name: "title", Type: "string"},
		{Name: "notes", Type: "string"},
		{Name: "count", Type: "int"},
	}

	got, err := applySchemaAt(dsn, "widgets", in)
	if err != nil {
		t.Fatalf("applySchemaAt: %v", err)
	}
	want := map[string]bool{"title": false, "notes": true, "count": false}
	for _, f := range got {
		if f.Nullable != want[f.Name] {
			t.Errorf("%s: Nullable got %v, want %v", f.Name, f.Nullable, want[f.Name])
		}
	}
}

func TestApplySchema_PreservesFieldOrder(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{
		{Name: "count", Type: "int"},
		{Name: "title", Type: "string"},
	}

	got, err := applySchemaAt(dsn, "widgets", in)
	if err != nil {
		t.Fatalf("applySchemaAt: %v", err)
	}
	if len(got) != 2 || got[0].Name != "count" || got[1].Name != "title" {
		t.Errorf("order not preserved: got %v", got)
	}
}

func TestApplySchema_UnknownFieldFails(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{{Name: "stat", Type: "string"}}

	_, err := applySchemaAt(dsn, "widgets", in)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
	// The message must name the real columns so the caller can self-correct.
	for _, want := range []string{"stat", "title", "notes", "count"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestApplySchema_TypeMismatchFails(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{{Name: "title", Type: "int"}}

	_, err := applySchemaAt(dsn, "widgets", in)
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("error %q missing field name", err)
	}
}

func TestApplySchema_MissingTableFails(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	in := []Field{{Name: "title", Type: "string"}}

	_, err := applySchemaAt(dsn, "gadgets", in)
	if err == nil {
		t.Fatal("expected error for missing table, got nil")
	}
	if !strings.Contains(err.Error(), "execute_sql") {
		t.Errorf("error %q should point at execute_sql", err)
	}
}

func TestApplySchema_NullablePasswordFails(t *testing.T) {
	dsn := testDSN(t, `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		email TEXT NOT NULL,
		password TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	in := []Field{{Name: "password", Type: "password"}}

	_, err := applySchemaAt(dsn, "users", in)
	if err == nil {
		t.Fatal("expected error for nullable password column, got nil")
	}
}

func TestApplySchema_RejectsReservedModelName(t *testing.T) {
	dsn := testDSN(t, `CREATE TABLE times (
		id INTEGER PRIMARY KEY,
		label TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err := checkReservedName("time"); err == nil {
		t.Error("expected 'time' to be rejected as a model name")
	}
	if err := checkReservedName("widget"); err != nil {
		t.Errorf("widget should be allowed, got %v", err)
	}
	_ = dsn
}

func TestApplySchema_UnsafeTableNameRejected(t *testing.T) {
	dsn := testDSN(t, widgetSchema)
	_, err := applySchemaAt(dsn, "widgets; DROP TABLE widgets", []Field{{Name: "title", Type: "string"}})
	if err == nil {
		t.Fatal("expected error for unsafe table name, got nil")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src/builder && go test ./... -run TestApplySchema -v`

Expected: FAIL — `undefined: applySchemaAt`, `undefined: checkReservedName`, and `Field` has no field `Nullable`.

- [ ] **Step 3: Add `Nullable` to `Field`**

In `src/builder/main.go`, change:

```go
type Field struct {
	Name string
	Type string
}
```

to:

```go
type Field struct {
	Name string
	Type string
	// Nullable is filled in by applySchema from the real table's
	// PRAGMA table_info output — never from the caller's field argument.
	Nullable bool
}
```

- [ ] **Step 4: Write `src/builder/schema.go`**

```go
package main

import (
	"database/sql"
	"fmt"
	"strings"
)

// reservedModelNames would collide with hand-written identifiers in the
// generated models package. models.Time is the shared timestamp type; a model
// named "time" would produce a duplicate declaration that fails to compile.
var reservedModelNames = map[string]bool{
	"time": true,
}

func checkReservedName(name string) error {
	if reservedModelNames[strings.ToLower(name)] {
		return fmt.Errorf("model name %q is reserved — it would collide with a type in the models package", name)
	}
	return nil
}

type column struct {
	Name    string
	SQLType string
	NotNull bool
}

// tableColumnsAt reads a table's shape from SQLite's schema.
//
// PRAGMA does not accept bound parameters, so the table name is interpolated.
// The isSafeIdent check below is what makes that safe — it must stay.
func tableColumnsAt(dsn, table string) ([]column, error) {
	if !isSafeIdent(table) {
		return nil, fmt.Errorf("unsafe table name %q", table)
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := []column{}
	for rows.Next() {
		var (
			cid      int
			name     string
			declType string
			notNull  int
			dflt     sql.NullString
			pk       int
		)
		if err := rows.Scan(&cid, &name, &declType, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, column{Name: name, SQLType: normalizeSQLType(declType), NotNull: notNull == 1})
	}
	return cols, rows.Err()
}

// normalizeSQLType strips length qualifiers and casing so VARCHAR(255) and
// varchar both compare equal to TEXT's affinity family.
func normalizeSQLType(t string) string {
	t = strings.ToUpper(strings.TrimSpace(t))
	if i := strings.Index(t, "("); i >= 0 {
		t = t[:i]
	}
	switch {
	case strings.Contains(t, "INT"):
		return "INTEGER"
	case strings.Contains(t, "CHAR"), strings.Contains(t, "TEXT"), strings.Contains(t, "CLOB"):
		return "TEXT"
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"):
		return "REAL"
	}
	return t
}

// expectedSQLType mirrors the sqlType funcMap helper in main.go.
func expectedSQLType(fieldType string) string {
	switch fieldType {
	case "int", "boolean":
		return "INTEGER"
	case "float":
		return "REAL"
	default:
		return "TEXT"
	}
}

// applySchemaAt validates declared fields against the real table and fills in
// Nullable from it.
//
// The fields argument stays a declaration of intent; the table is the source
// of truth. A mismatch fails the tool with a diff rather than silently
// generating a model that lies about the data.
func applySchemaAt(dsn, table string, fields []Field) ([]Field, error) {
	cols, err := tableColumnsAt(dsn, table)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %q does not exist — run execute_sql to create it before scaffolding", table)
	}

	byName := make(map[string]column, len(cols))
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		byName[c.Name] = c
		names = append(names, c.Name)
	}

	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		c, ok := byName[f.Name]
		if !ok {
			return nil, fmt.Errorf("field %q is not a column of table %q (columns: %s)",
				f.Name, table, strings.Join(names, ", "))
		}
		if want := expectedSQLType(f.Type); c.SQLType != want {
			return nil, fmt.Errorf("field %q declared as %s (expects %s) but column %q.%s is %s",
				f.Name, f.Type, want, table, f.Name, c.SQLType)
		}
		if f.Type == "password" && !c.NotNull {
			return nil, fmt.Errorf("field %q is a password field but column %q.%s is nullable — declare it NOT NULL",
				f.Name, table, f.Name)
		}
		f.Nullable = !c.NotNull
		out = append(out, f)
	}
	return out, nil
}

// applySchema is the production entry point, against the live app database.
func applySchema(table string, fields []Field) ([]Field, error) {
	return applySchemaAt(sqliteDSN, table, fields)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd src/builder && go test ./... -run 'TestApplySchema' -v`

Expected: PASS — all eight test functions.

- [ ] **Step 6: Wire the guard into both scaffolding tools**

In `src/builder/main.go`, in **both** `handleCreateModel` and `handleScaffoldList`, immediately after the existing empty-fields check:

```go
	fields := parseFields(rawFieldsToStrings(rawFields))
	if len(fields) == 0 {
		return errResult("at least one field is required"), nil
	}
```

insert exactly this:

```go
	if err := checkReservedName(name); err != nil {
		return errResult(err.Error()), nil
	}
	fields, applyErr := applySchema(toPlural(name), fields)
	if applyErr != nil {
		return errResult(applyErr.Error()), nil
	}
```

The name `applyErr` rather than `err` is deliberate — `fields` is already declared by the `:=` above, so a plain `err` here would be a redeclaration in the same scope only if `err` also already exists. Using a distinct name makes the statement valid regardless of what either handler declared earlier.

- [ ] **Step 7: Verify the builder still compiles and all tests pass**

Run: `cd src/builder && go build ./... && go test ./... -v`

Expected: build succeeds, all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add src/builder/schema.go src/builder/schema_test.go src/builder/main.go
git commit -m "feat: PRAGMA schema introspection with field mismatch guard"
```

---

## Task 5: Model template — `Time`, nullable pointers, and `GetPage`

**Files:**
- Modify: `src/builder/main.go` — funcMap helpers
- Modify: `src/builder/templates/model.go.tmpl`
- Modify: `src/builder/templates/model_test.go.tmpl`
- Modify: `src/builder/render_test.go`

**Interfaces:**
- Consumes: `models.Time` (Task 1), `Field.Nullable` (Task 4).
- Produces, on every generated model `X`:
  - `GetPage(limit, offset int) ([]X, int, error)` — page, total, error. **Replaces `GetAll()`.** Task 6's list handler calls this.
  - `Find(id int64) (*X, error)` — unchanged signature, nullable-aware internally
  - `Create(...)` — nullable fields become pointer parameters
  - `Delete(id int64) error` — unchanged

- [ ] **Step 1: Add the funcMap helpers**

In `src/builder/main.go`, add these entries to `funcMap` (keep the existing `goType`, `sqlType`, `joinNames`, `placeholders`, `titleCase`, `toPascal`, `toPlural`, `testArgs` entries — `scanFields` is superseded by `scanTargets` and may be removed once nothing references it):

```go
	// goFieldType is goType plus nullability: a nullable column becomes a Go
	// pointer, which marshals to JSON null and maps to a Swift optional.
	"goFieldType": func(f Field) string {
		base := goTypeFor(f.Type)
		if f.Nullable {
			return "*" + base
		}
		return base
	},
	// scanDecls emits the temporaries a row scan needs for nullable columns.
	"scanDecls": func(fields []Field, indent string) string {
		lines := []string{}
		for _, f := range fields {
			if f.Nullable {
				lines = append(lines, indent+"var "+f.Name+"Null "+nullTypeFor(f.Type))
			}
		}
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n") + "\n"
	},
	// scanTargets emits the &-arguments for rows.Scan, routing nullable
	// columns through their temporaries.
	"scanTargets": func(fields []Field, prefix string) string {
		refs := make([]string, len(fields))
		for i, f := range fields {
			if f.Nullable {
				refs[i] = "&" + f.Name + "Null"
			} else {
				refs[i] = prefix + toPascal(f.Name)
			}
		}
		return strings.Join(refs, ", ")
	},
	// scanAssigns copies valid temporaries back onto the struct as pointers.
	"scanAssigns": func(fields []Field, target, indent string) string {
		lines := []string{}
		for _, f := range fields {
			if !f.Nullable {
				continue
			}
			lines = append(lines,
				indent+"if "+f.Name+"Null.Valid {",
				indent+"\t"+target+toPascal(f.Name)+" = &"+f.Name+"Null."+nullFieldFor(f.Type),
				indent+"}")
		}
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n") + "\n"
	},
```

Update `createParams` to be pointer-aware and `testArgs` to supply pointer values, replacing the existing entries:

```go
	"createParams": func(fields []Field) string {
		params := make([]string, len(fields))
		for i, f := range fields {
			goT := goTypeFor(f.Type)
			if f.Nullable {
				goT = "*" + goT
			}
			params[i] = f.Name + " " + goT
		}
		return strings.Join(params, ", ")
	},
	"testArgs": func(fields []Field) string {
		vals := make([]string, len(fields))
		for i, f := range fields {
			if f.Nullable {
				// Non-nil pointer so the round-trip actually exercises the
				// nullable scan path rather than short-circuiting on NULL.
				vals[i] = "&" + f.Name + "TestVal"
				continue
			}
			vals[i] = testLiteralFor(f.Type)
		}
		return strings.Join(vals, ", ")
	},
	// testDecls declares the addressable locals testArgs points at.
	"testDecls": func(fields []Field, indent string) string {
		lines := []string{}
		for _, f := range fields {
			if f.Nullable {
				lines = append(lines, indent+f.Name+"TestVal := "+testLiteralFor(f.Type))
			}
		}
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n") + "\n"
	},
	// sqlNotNull emits the NOT NULL clause for generated fixture schemas so
	// the test table's shape matches the model the test exercises.
	"sqlNotNull": func(f Field) string {
		if f.Nullable {
			return ""
		}
		return " NOT NULL"
	},
```

Add these package-level helpers to `src/builder/main.go` (they back the funcMap entries above and keep the type mapping defined in exactly one place):

```go
func goTypeFor(t string) string {
	switch t {
	case "int":
		return "int64"
	case "boolean":
		return "bool"
	case "float":
		return "float64"
	default:
		return "string"
	}
}

func nullTypeFor(t string) string {
	switch t {
	case "int":
		return "sql.NullInt64"
	case "boolean":
		return "sql.NullBool"
	case "float":
		return "sql.NullFloat64"
	default:
		return "sql.NullString"
	}
}

func nullFieldFor(t string) string {
	switch t {
	case "int":
		return "Int64"
	case "boolean":
		return "Bool"
	case "float":
		return "Float64"
	default:
		return "String"
	}
}

func testLiteralFor(t string) string {
	switch t {
	case "int":
		return "int64(1)"
	case "boolean":
		return "true"
	case "float":
		return "1.5"
	default:
		return `"test"`
	}
}
```

Change the existing `goType` funcMap entry to delegate: `"goType": goTypeFor,`.

- [ ] **Step 2: Write the failing render test**

In `src/builder/render_test.go`, add a nullable sample and the golden assertions:

```go
func sampleFieldsWithNullable() []Field {
	return []Field{
		{Name: "title", Type: "string", Nullable: false},
		{Name: "notes", Type: "string", Nullable: true},
		{Name: "count", Type: "int", Nullable: false},
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

	if !strings.Contains(out, "Create(title string, notes *string, count int64)") {
		t.Errorf("Create should take a pointer for the nullable field:\n%s", out)
	}
}

func TestModelTestTemplate_NullableIsValidGo(t *testing.T) {
	data := newData("widget", sampleFieldsWithNullable())
	renderAndParse(t, "model_test.go.tmpl", data)
}
```

Add `"strings"` to the import block of `render_test.go`.

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd src/builder && go test ./... -run 'TestModelTemplate|TestModelTestTemplate' -v`

Expected: FAIL — the template still emits `GetAll`, `time.Time`, and non-pointer fields.

- [ ] **Step 4: Rewrite `src/builder/templates/model.go.tmpl`**

```gotemplate
package models

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"gova/app/cache"
	{{- if .HasPassword}}
	"golang.org/x/crypto/bcrypt"
	{{- end}}
)

type {{.PascalName}} struct {
	ID        int64     `json:"id"`
	{{- range .Fields}}
	{{toPascal .Name}} {{goFieldType .}} `json:"{{.Name}}"`
	{{- end}}
	CreatedAt Time `json:"created_at"`
}

// {{.Name}}Page is the cache payload for a single page of results — items and
// total travel together so a cache hit does not need a second COUNT query.
type {{.Name}}Page struct {
	Items []{{.PascalName}} `json:"items"`
	Total int               `json:"total"`
}

type {{.PascalName}}Model struct {
	readDB  *sql.DB
	writeDB *sql.DB
	cache   *cache.Cache
}

func New{{.PascalName}}Model(readDB, writeDB *sql.DB, c *cache.Cache) *{{.PascalName}}Model {
	return &{{.PascalName}}Model{readDB: readDB, writeDB: writeDB, cache: c}
}

// GetPage returns one window of rows plus the unfiltered total.
//
// Callers clamp limit and offset before calling — see handlers/paging.go.
func (m *{{.PascalName}}Model) GetPage(limit, offset int) ([]{{.PascalName}}, int, error) {
	cacheKey := fmt.Sprintf("{{.PluralName}}:page:%d:%d", limit, offset)
	if hit, ok := m.cache.Get(cacheKey); ok {
		var page {{.Name}}Page
		if err := json.Unmarshal(hit, &page); err == nil {
			return page.Items, page.Total, nil
		}
	}

	var total int
	if err := m.readDB.QueryRow("SELECT COUNT(*) FROM {{.PluralName}}").Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := m.readDB.Query(
		"SELECT id, {{joinNames .Fields}}, created_at FROM {{.PluralName}} ORDER BY created_at DESC LIMIT ? OFFSET ?",
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	// Initialized non-nil: a nil slice marshals as JSON null, which breaks
	// strictly-typed clients decoding an array.
	items := []{{.PascalName}}{}
	for rows.Next() {
		var item {{.PascalName}}
{{scanDecls .Fields "\t\t"}}		if err := rows.Scan(&item.ID, {{scanTargets .Fields "&item."}}, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
{{scanAssigns .Fields "item." "\t\t"}}		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if data, err := json.Marshal({{.Name}}Page{Items: items, Total: total}); err == nil {
		m.cache.Set(cacheKey, data, 5*time.Minute)
	}
	return items, total, nil
}

func (m *{{.PascalName}}Model) Find(id int64) (*{{.PascalName}}, error) {
	row := m.readDB.QueryRow("SELECT id, {{joinNames .Fields}}, created_at FROM {{.PluralName}} WHERE id = ?", id)
	var item {{.PascalName}}
{{scanDecls .Fields "\t"}}	if err := row.Scan(&item.ID, {{scanTargets .Fields "&item."}}, &item.CreatedAt); err != nil {
		return nil, err
	}
{{scanAssigns .Fields "item." "\t"}}	return &item, nil
}

func (m *{{.PascalName}}Model) Create({{createParams .Fields}}) (int64, error) {
	{{- range .Fields}}{{if eq .Type "password"}}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil { return 0, err }
	{{end}}{{end}}
	// A nil pointer binds to SQL NULL directly — no sql.Null* wrapper needed
	// on the insert path.
	res, err := m.writeDB.Exec(
		"INSERT INTO {{.PluralName}} ({{joinNames .Fields}}) VALUES ({{placeholders .Fields}})",
		{{insertArgs .Fields}},
	)
	if err != nil {
		return 0, err
	}
	m.cache.Bust("{{.PluralName}}:")
	return res.LastInsertId()
}

func (m *{{.PascalName}}Model) Delete(id int64) error {
	_, err := m.writeDB.Exec("DELETE FROM {{.PluralName}} WHERE id = ?", id)
	if err == nil {
		m.cache.Bust("{{.PluralName}}:")
	}
	return err
}
```

Note: the existing `m.cache.Bust("{{.PluralName}}:")` prefix sweep already invalidates every `{{.PluralName}}:page:*` key. No invalidation change is needed.

- [ ] **Step 5: Rewrite `src/builder/templates/model_test.go.tmpl`**

```gotemplate
package models

import (
	"testing"

	"gova/app/cache"
	"gova/app/db"
)

func Test{{.PascalName}}Model_CRUD(t *testing.T) {
	testDB := db.OpenTest(t, `CREATE TABLE {{.PluralName}} (
		id INTEGER PRIMARY KEY,
		{{- range .Fields}}
		{{.Name}} {{sqlType .Type}}{{sqlNotNull .}},
		{{- end}}
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	m := New{{.PascalName}}Model(testDB.Read, testDB.Write, cache.New())

{{testDecls .Fields "\t"}}	id, err := m.Create({{testArgs .Fields}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == 0 {
		t.Fatal("Create returned id 0")
	}

	found, err := m.Find(id)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.ID != id {
		t.Errorf("Find: got ID %d, want %d", found.ID, id)
	}

	items, total, err := m.GetPage(50, 0)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if total != 1 {
		t.Errorf("GetPage: got total %d, want 1", total)
	}
	if len(items) != 1 {
		t.Fatalf("GetPage: got %d items, want 1", len(items))
	}
	if items[0].ID != id {
		t.Errorf("GetPage: got ID %d, want %d", items[0].ID, id)
	}

	// An offset past the end returns an empty (non-nil) slice, not an error,
	// and the total still reflects the full table.
	page2, total2, err := m.GetPage(50, 50)
	if err != nil {
		t.Fatalf("GetPage(offset 50): %v", err)
	}
	if page2 == nil {
		t.Error("GetPage past the end returned a nil slice; want empty")
	}
	if len(page2) != 0 {
		t.Errorf("GetPage(offset 50): got %d items, want 0", len(page2))
	}
	if total2 != 1 {
		t.Errorf("GetPage(offset 50): got total %d, want 1", total2)
	}

	if err := m.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Find(id); err == nil {
		t.Error("Find after Delete: expected error, got nil")
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd src/builder && go test ./... -v`

Expected: PASS — including the pre-existing `TestRenderAndParse_ExistingTemplateIsValidGo`, which proves the non-nullable path still renders valid Go.

- [ ] **Step 7: Commit**

```bash
git add src/builder/main.go src/builder/render_test.go \
        src/builder/templates/model.go.tmpl src/builder/templates/model_test.go.tmpl
git commit -m "feat: model template emits Time, nullable pointers, and GetPage"
```

---

## Task 6: Paging helper and the list handler template

**Files:**
- Create: `src/app/handlers/paging.go`
- Create: `src/app/handlers/paging_test.go`
- Modify: `src/builder/templates/list_handler.go.tmpl`
- Modify: `src/builder/templates/list_handler_test.go.tmpl`

**Interfaces:**
- Consumes: `jsonList` and `Meta` (Task 2), `GetPage` (Task 5).
- Produces: `handlers.queryInt(r *http.Request, key string, def, min, max int) int`, plus `defaultPageLimit = 50` and `maxPageLimit = 200`.

- [ ] **Step 1: Write the failing test**

Create `src/app/handlers/paging_test.go`:

```go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryInt(t *testing.T) {
	cases := []struct {
		name  string
		query string
		key   string
		def   int
		min   int
		max   int
		want  int
	}{
		{"absent falls back to default", "", "limit", 50, 1, 200, 50},
		{"empty falls back to default", "?limit=", "limit", 50, 1, 200, 50},
		{"valid value passes through", "?limit=25", "limit", 50, 1, 200, 25},
		{"above max clamps to max", "?limit=5000", "limit", 50, 1, 200, 200},
		{"below min clamps to min", "?limit=0", "limit", 50, 1, 200, 1},
		{"negative clamps to min", "?limit=-3", "limit", 50, 1, 200, 1},
		{"non-numeric falls back to default", "?limit=abc", "limit", 50, 1, 200, 50},
		{"offset zero allowed", "?offset=0", "offset", 0, 0, 1 << 30, 0},
		{"negative offset clamps to zero", "?offset=-10", "offset", 0, 0, 1 << 30, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/things"+tc.query, nil)
			if got := queryInt(req, tc.key, tc.def, tc.min, tc.max); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPagingBounds(t *testing.T) {
	if defaultPageLimit != 50 {
		t.Errorf("defaultPageLimit: got %d, want 50", defaultPageLimit)
	}
	if maxPageLimit != 200 {
		t.Errorf("maxPageLimit: got %d, want 200", maxPageLimit)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `docker compose exec app go test ./handlers/... -run 'TestQueryInt|TestPagingBounds' -v`

Expected: FAIL — `undefined: queryInt`, `undefined: defaultPageLimit`.

- [ ] **Step 3: Write `src/app/handlers/paging.go`**

```go
package handlers

import (
	"net/http"
	"strconv"
)

// Paging bounds. Every generated list endpoint is bounded by default —
// an unbounded list is the failure mode that hurts mobile clients most.
const (
	defaultPageLimit = 50
	maxPageLimit     = 200
	maxPageOffset    = 1 << 30
)

// queryInt reads a bounded integer query parameter.
//
// Absent, empty, and unparseable values all fall back to def; parseable
// values outside [min, max] are clamped rather than rejected, so a client
// that guesses wrong still gets a usable response instead of a 400.
func queryInt(r *http.Request, key string, def, min, max int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `docker compose exec app go test ./handlers/... -v`

Expected: PASS.

- [ ] **Step 5: Rewrite `src/builder/templates/list_handler.go.tmpl`**

```gotemplate
package handlers

import (
	"database/sql"
	"net/http"
	"gova/app/cache"
	"gova/app/models"
)

// {{.PascalName}}ListGET handles GET /api/v1/{{.PluralName}}
// Query: ?limit=<1..200, default 50>&offset=<0.., default 0>
func {{.PascalName}}ListGET(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := queryInt(r, "limit", defaultPageLimit, 1, maxPageLimit)
		offset := queryInt(r, "offset", 0, 0, maxPageOffset)

		model := models.New{{.PascalName}}Model(readDB, writeDB, appCache)
		items, total, err := model.GetPage(limit, offset)
		if err != nil {
			jsonError(w, "failed to load", 500)
			return
		}
		jsonList(w, items, Meta{Limit: limit, Offset: offset, Total: total})
	}
}
```

- [ ] **Step 6: Rewrite `src/builder/templates/list_handler_test.go.tmpl`**

```gotemplate
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gova/app/cache"
	"gova/app/db"
	"gova/app/models"
)

func {{.Name}}ListSchema() string {
	return `CREATE TABLE {{.PluralName}} (
		id INTEGER PRIMARY KEY,
		{{- range .Fields}}
		{{.Name}} {{sqlType .Type}}{{sqlNotNull .}},
		{{- end}}
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
}

type {{.Name}}ListBody struct {
	OK   bool            `json:"ok"`
	Data json.RawMessage `json:"data"`
	Meta struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Total  int `json:"total"`
	} `json:"meta"`
}

func do{{.PascalName}}ListRequest(t *testing.T, testDB *db.DB, appCache *cache.Cache, target string) {{.Name}}ListBody {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()

	{{.PascalName}}ListGET(testDB.Read, testDB.Write, appCache)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body {{.Name}}ListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if !body.OK {
		t.Errorf("ok: got false, want true")
	}
	return body
}

func Test{{.PascalName}}ListGET(t *testing.T) {
	testDB := db.OpenTest(t, {{.Name}}ListSchema())
	appCache := cache.New()
	model := models.New{{.PascalName}}Model(testDB.Read, testDB.Write, appCache)
{{testDecls .Fields "\t"}}	if _, err := model.Create({{testArgs .Fields}}); err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	body := do{{.PascalName}}ListRequest(t, testDB, appCache, "/api/v1/{{.PluralName}}")

	if body.Meta.Total != 1 {
		t.Errorf("meta.total: got %d, want 1", body.Meta.Total)
	}
	if body.Meta.Limit != 50 {
		t.Errorf("meta.limit: got %d, want 50 (default)", body.Meta.Limit)
	}
	if body.Meta.Offset != 0 {
		t.Errorf("meta.offset: got %d, want 0", body.Meta.Offset)
	}
}

// An empty table must serialize data as [] — never null. A strictly-typed
// client decoding an array fails outright on null.
func Test{{.PascalName}}ListGET_EmptyRendersArray(t *testing.T) {
	testDB := db.OpenTest(t, {{.Name}}ListSchema())
	appCache := cache.New()

	body := do{{.PascalName}}ListRequest(t, testDB, appCache, "/api/v1/{{.PluralName}}")

	if string(body.Data) != "[]" {
		t.Errorf("data: got %s, want []", body.Data)
	}
	if body.Meta.Total != 0 {
		t.Errorf("meta.total: got %d, want 0", body.Meta.Total)
	}
}

func Test{{.PascalName}}ListGET_ClampsLimit(t *testing.T) {
	testDB := db.OpenTest(t, {{.Name}}ListSchema())
	appCache := cache.New()

	over := do{{.PascalName}}ListRequest(t, testDB, appCache, "/api/v1/{{.PluralName}}?limit=5000")
	if over.Meta.Limit != 200 {
		t.Errorf("limit=5000: got meta.limit %d, want 200", over.Meta.Limit)
	}

	under := do{{.PascalName}}ListRequest(t, testDB, appCache, "/api/v1/{{.PluralName}}?limit=0")
	if under.Meta.Limit != 1 {
		t.Errorf("limit=0: got meta.limit %d, want 1", under.Meta.Limit)
	}

	bad := do{{.PascalName}}ListRequest(t, testDB, appCache, "/api/v1/{{.PluralName}}?limit=abc")
	if bad.Meta.Limit != 50 {
		t.Errorf("limit=abc: got meta.limit %d, want 50 (default)", bad.Meta.Limit)
	}
}

func Test{{.PascalName}}ListGET_ClampsOffset(t *testing.T) {
	testDB := db.OpenTest(t, {{.Name}}ListSchema())
	appCache := cache.New()

	body := do{{.PascalName}}ListRequest(t, testDB, appCache, "/api/v1/{{.PluralName}}?offset=-5")
	if body.Meta.Offset != 0 {
		t.Errorf("offset=-5: got meta.offset %d, want 0", body.Meta.Offset)
	}
}
```

- [ ] **Step 7: Verify the templates render as valid Go**

Run: `cd src/builder && go test ./... -run 'TestListHandlerTestTemplate|TestRenderAndParse' -v`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add src/app/handlers/paging.go src/app/handlers/paging_test.go \
        src/builder/templates/list_handler.go.tmpl src/builder/templates/list_handler_test.go.tmpl
git commit -m "feat: bounded pagination in generated list handlers"
```

---

## Task 7: `/api/v1` path migration

**Files:**
- Modify: `src/builder/main.go:571-573`, `:594`, `:610`, `:703-705`
- Modify: `src/builder/templates/list_page.js.tmpl:9`
- Modify: `src/builder/templates/js_form.js.tmpl` (the `post(...)` call)
- Modify: `src/builder/templates/login.js.tmpl:16`
- Modify: `src/builder/templates/register.js.tmpl:17`
- Modify: `src/builder/templates/mobile_auth_handler.go.tmpl` (doc comments)
- Modify: `src/builder/templates/auth_test.go.tmpl:45,66,89,110`
- Modify: `src/builder/templates/register_test.go.tmpl:24,51`
- Modify: `src/builder/templates/mobile_auth_test.go.tmpl:30,59,71,86,107`

**Interfaces:**
- Consumes: nothing new.
- Produces: every generated route path and every client call site under `/api/v1/`.

**Context:** `add_js_form` derives its generated function name from the endpoint via `endpointSlug := strings.TrimPrefix(apiEndpoint, "/api/")` at `src/builder/main.go:610`. Left alone, a `/api/v1/projects` endpoint would produce a form named after `v1/projects`. This is the easiest breakage in the task to miss.

- [ ] **Step 1: Update the route instruction strings in `src/builder/main.go`**

Lines 571-573 (`handleScaffoldAuth` output) become:

```go
		"  r.Post(\"/api/v1/auth/login\",  handlers.LoginPOST(database.Read, database.Write, appCache))\n"+
		"  r.Post(\"/api/v1/auth/logout\", handlers.LogoutPOST())\n"+
		"  r.Get(\"/api/v1/auth/me\",      handlers.MeGET(database.Read, database.Write, appCache))")
```

Line 594 (`handleScaffoldRegistration` output) becomes:

```go
		"  r.Post(\"/api/v1/auth/register\", handlers.RegisterPOST(database.Read, database.Write, appCache))")
```

Lines 703-705 (`handleScaffoldMobileAuth` output) become:

```go
  r.Post("/api/v1/auth/login_token",    handlers.MobileLoginPOST(database.Read, database.Write, appCache))
  r.Delete("/api/v1/auth/logout_token", handlers.MobileLogoutDELETE(database.Write))
  r.Get("/api/v1/auth/me_token",        handlers.MobileMeGET(database.Read, database.Write, appCache))
```

- [ ] **Step 2: Fix the `add_js_form` slug derivation**

At `src/builder/main.go:610`, replace:

```go
	endpointSlug := strings.TrimPrefix(apiEndpoint, "/api/")
```

with:

```go
	// Strip the versioned API prefix so the generated form function is named
	// after the resource, not after "v1".
	endpointSlug := strings.TrimPrefix(apiEndpoint, "/api/v1/")
	endpointSlug = strings.TrimPrefix(endpointSlug, "/api/")
	endpointSlug = strings.TrimPrefix(endpointSlug, "/")
```

- [ ] **Step 3: Update every template path**

Apply these exact replacements:

| File | From | To |
|---|---|---|
| `list_page.js.tmpl:9` | `get('/api/{{.PluralName}}')` | `get('/api/v1/{{.PluralName}}')` |
| `login.js.tmpl:16` | `post('/api/auth/login', ` | `post('/api/v1/auth/login', ` |
| `register.js.tmpl:17` | `post('/api/auth/register', ` | `post('/api/v1/auth/register', ` |
| `auth_test.go.tmpl` (4 sites) | `"/api/auth/login"` | `"/api/v1/auth/login"` |
| `register_test.go.tmpl` (2 sites) | `"/api/auth/register"` | `"/api/v1/auth/register"` |
| `mobile_auth_test.go.tmpl` (3 sites) | `"/api/auth/login_token"` | `"/api/v1/auth/login_token"` |
| `mobile_auth_test.go.tmpl` (2 sites) | `"/api/auth/me_token"` | `"/api/v1/auth/me_token"` |
| `mobile_auth_handler.go.tmpl` | `// MobileLoginPOST handles POST /api/auth/login_token` | `// MobileLoginPOST handles POST /api/v1/auth/login_token` |
| `mobile_auth_handler.go.tmpl` | remaining `/api/auth/...` doc comments | `/api/v1/auth/...` |

In `js_form.js.tmpl`, the endpoint comes from the `.APIEndpoint` template variable supplied by the caller — no literal to change there. Verify by reading that the `post('{{.APIEndpoint}}', data)` line is unchanged.

- [ ] **Step 4: Verify no unversioned API paths remain**

```bash
cd src/builder && grep -rn "'/api/\|\"/api/" templates/ main.go | grep -v "/api/v1/"
```

Expected: no output.

- [ ] **Step 5: Verify all templates still render as valid Go**

Run: `cd src/builder && go build ./... && go test ./... -v`

Expected: build succeeds, all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add src/builder/main.go src/builder/templates/
git commit -m "feat: move all generated API routes under /api/v1"
```

---

## Task 8: Documentation

**Files:**
- Modify: `CLAUDE.md` (this repo)
- Modify: `../gova-ios/CLAUDE.md`

**Interfaces:**
- Consumes: everything above. Produces no code.

**Context:** `CLAUDE.md` is loaded into context for every future build in this repo. A stale `model.GetAll()` example there will cause a future agent to write code that does not compile.

- [ ] **Step 1: Update the monolith `CLAUDE.md`**

Make these edits:

1. **Critical Constraint 1** — change the correct example from `model.GetAll()` to `model.GetPage(limit, offset)`.
2. **The Golden Recipe, step 3** — change the `add_js_form` example endpoint from `/api/projects_create` to `/api/v1/projects`.
3. **Frontend Patterns, JS module structure** — change `get('/api/items')` to `get('/api/v1/items')`.
4. **Add a new section** after Critical Constraints:

```markdown
## API Wire Contract

Every JSON response uses one envelope:

​```json
{ "ok": true, "data": [ ... ], "meta": { "limit": 50, "offset": 0, "total": 123 } }
{ "ok": false, "error": "Name is required", "code": "validation_failed", "fields": { "name": "required" } }
​```

- **`data` is never `null` for a list.** Models initialize slices non-nil and
  `jsonOK`/`jsonList` normalize as a second guard. A typed client decoding an
  array must never see `null`.
- **`error` is always a plain string.** `code` and `fields` are additive.
- **Codes:** `unauthorized`, `forbidden`, `not_found`, `conflict`,
  `validation_failed`, `rate_limited`, `internal`.
- **Timestamps** are RFC3339, UTC, second precision — via `models.Time`. Never
  use a bare `time.Time` in a model struct.
- **Lists are paginated by default:** `?limit=` (1–200, default 50) and
  `?offset=`. Use `jsonList(w, items, Meta{...})`, not `jsonOK`.
- **All API routes live under `/api/v1/`.**
- `GET /api/v1/_version` reports `api_version` and `min_client_version`.

Helpers in `handlers/json.go`: `jsonOK`, `jsonList`, `jsonError`,
`jsonErrorCode`, `jsonValidationError`.
```

5. **Tool Cheat Sheet** — add a note under `create_model` and `scaffold_list`: "Validates `fields` against the real table via `PRAGMA table_info`; a mismatch fails the call. Nullable columns become Go pointers."

- [ ] **Step 2: Update `../gova-ios/CLAUDE.md`**

In the Step 2 endpoint list, change the three paths:

- `POST /api/auth/login_token` → `POST /api/v1/auth/login_token`
- `DELETE /api/auth/logout_token` → `DELETE /api/v1/auth/logout_token`
- `GET /api/auth/me_token` → `GET /api/v1/auth/me_token`

In the Step 3 field type mapping table, replace the note about checking nullability manually with:

```markdown
The web app's model marshals nullable columns as JSON `null` and the manifest
records them explicitly — a `*string` in the Go struct means `String?` in Swift.
You no longer need to read the SQL schema to determine this.
```

Add a row to the type mapping table: `created_at | Date` becomes `created_at | Date (RFC3339, decode with .iso8601)`.

- [ ] **Step 3: Verify no stale references remain**

```bash
grep -n "GetAll" CLAUDE.md
grep -rn '"/api/auth\|/api/items\|/api/projects_create' CLAUDE.md ../gova-ios/CLAUDE.md
```

Expected: no output from either command.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: API wire contract, GetPage, and /api/v1 in build context"
cd ../gova-ios && git add CLAUDE.md && \
  git commit -m "docs: mobile auth endpoints moved to /api/v1" && cd -
```

---

## Task 9: End-to-end verification

**Files:** none modified — this task only runs and observes.

**Interfaces:** consumes everything above.

**Context:** Every prior task verified its own unit. This task proves the generator and the runtime agree — that a scaffold call produces code that compiles, runs, and returns the contracted shape. Note that a scaffolded app's own tests land in `src/app/`, so the full app suite covers the generated output too.

- [ ] **Step 1: Bring the stack up clean**

```bash
docker compose down -v
docker compose up -d
sleep 5
docker compose ps
```

Expected: both `app` and `mcp` containers running.

- [ ] **Step 2: Run both test suites**

```bash
docker compose exec app go test ./...
cd src/builder && go test ./... && cd -
```

Expected: `ok` for every package in both. Any FAIL stops this task — fix before continuing.

- [ ] **Step 3: Scaffold a resource with a nullable column via MCP**

Call `execute_sql`:

```sql
CREATE TABLE projects (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

Then call `scaffold_list(name='project', fields=['name:string', 'notes:string'])`.

Expected: six files created, pattern check passes.

- [ ] **Step 4: Verify the guard rejects a bad field**

Call `scaffold_list(name='project', fields=['name:string', 'bogus:string'])`.

Expected: an error naming `bogus` and listing the real columns (`name`, `notes`). No files written.

- [ ] **Step 5: Verify the generated code compiles and its tests pass**

```bash
docker compose restart app
sleep 5
docker compose exec app go test ./...
```

Expected: `ok` including the newly generated `models/Project_test.go` and `handlers/project_list_test.go`.

- [ ] **Step 6: Confirm the nullable field is a pointer**

```bash
grep -n "Notes\|Name " src/app/models/Project.go
```

Expected: `Notes *string` and `Name string` — pointer only on the nullable column.

- [ ] **Step 7: Wire the route and verify the live wire format**

Add to `src/app/main.go` above the `// @gova-routes` marker:

```go
	r.Get("/api/v1/projects", handlers.ProjectListGET(database.Read, database.Write, appCache))
```

Then:

```bash
docker compose restart app
sleep 5
curl -s "localhost:8080/api/v1/projects"
```

Expected — the empty-table case, which is the whole point of the build:

```json
{"ok":true,"data":[],"meta":{"limit":50,"offset":0,"total":0}}
```

`data` must be `[]`, **not** `null`.

- [ ] **Step 8: Verify paging, clamping, and the timestamp format with real data**

Seed rows with the MCP `execute_sql` tool (it accepts DML, and unlike a `sqlite3` CLI invocation it is guaranteed to be available — the app container ships only the Go binary):

```sql
INSERT INTO projects (name, notes) VALUES ('Roof', NULL), ('Deck', 'urgent');
```

Then:

```bash
docker compose restart app
sleep 5
curl -s "localhost:8080/api/v1/projects"
curl -s "localhost:8080/api/v1/projects?limit=1"
curl -s "localhost:8080/api/v1/projects?limit=9999"
```

Expected:
- Unfiltered: two items; the `Roof` row has `"notes":null`; every `created_at` matches `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$` with **no fractional seconds and no offset other than `Z`**.
- `?limit=1`: one item, `"meta":{"limit":1,"offset":0,"total":2}`.
- `?limit=9999`: `"meta"` reports `"limit":200`.

- [ ] **Step 9: Verify the error shape**

```bash
curl -s -i "localhost:8080/api/v1/nonexistent" | head -1
curl -s "localhost:8080/api/v1/_version"
```

Expected: a 404 from chi for the unknown route, and the version endpoint returning `api_version` and `min_client_version`.

- [ ] **Step 10: Revert the scratch scaffold**

The `projects` resource was scaffolded only to prove the pipeline. Remove it so the template repo ships clean:

```bash
git status --short
git checkout -- src/app/main.go
rm -f src/app/models/Project.go src/app/models/Project_test.go \
      src/app/handlers/project_list.go src/app/handlers/project_list_test.go \
      src/app/static/pages/projects.html src/app/static/js/projects.js
docker compose down -v
```

Run `git status --short` again — expected: clean, no untracked scaffold output.

- [ ] **Step 11: Final full-suite confirmation**

```bash
docker compose up -d
sleep 5
docker compose exec app go test ./...
cd src/builder && go test ./... && cd -
git log --oneline main..HEAD
```

Expected: both suites pass on the clean tree, and the log shows eight feature/docs commits.

- [ ] **Step 12: Commit any remaining changes**

```bash
git status --short
```

Expected: clean. If anything is outstanding, commit it with an explanatory message rather than discarding it.

---

## Verification Summary

| Concern | Where it is proven |
|---|---|
| Empty list is `[]` not `null` | Task 2 Step 1, Task 6 Step 6, Task 9 Step 7 |
| Timestamps have no fractional seconds | Task 1 Step 1, Task 9 Step 8 |
| Nullable columns become optionals | Task 4 Step 1, Task 5 Step 2, Task 9 Step 6 |
| Schema mismatch fails loudly | Task 4 Step 1, Task 9 Step 4 |
| Error codes map from status | Task 2 Step 1 |
| `jsonError` signature preserved | Task 2 Step 1 (`TestJSONError_DerivesCodeFromStatus`) |
| Pagination bounds and clamping | Task 6 Step 1, Task 9 Step 8 |
| All routes under `/api/v1` | Task 7 Step 4, Task 9 Step 7 |
| Version endpoint exists | Task 3 Step 6, Task 9 Step 9 |
| Generated code compiles and runs | Task 9 Step 5 |
