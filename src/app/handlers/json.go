package handlers

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
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
	CodeMethodNotAllowed = "method_not_allowed"
	CodeUnavailable      = "unavailable"
	CodeInternal         = "internal"
)

// codeForStatus maps an HTTP status onto the machine-readable failure kind a
// client switches on.
//
// THE DEFAULT USED TO BE `internal` FOR EVERYTHING, and that is wrong for a
// whole class rather than for one value. Every 4xx nobody enumerated — 400
// first among them — told the caller that their own malformed request was this
// server's fault. `jsonError(w, "invalid request body", 400)` is the shortest
// helper and the one a handler naturally reaches for, so the defect reproduced
// itself in every generated app: the body says one thing, the code the client
// branches on says another, and the client is sent to the wrong place to look.
//
// 413 is the same sentence one status along, and it is the one that bites next:
// a handler that caps its body with http.MaxBytesReader and answers the
// overflow with a bare jsonError told an uploader their oversized file was a
// server bug.
//
// So the default is split by class rather than extended by one case at a time.
// A 4xx is by definition something about the REQUEST, so validation_failed is
// the honest fallback; anything else is ours. That fails safe in the direction
// that is true more often, and a status nobody thought of no longer arrives
// mislabelled.
func codeForStatus(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusMethodNotAllowed:
		return CodeMethodNotAllowed
	case http.StatusConflict:
		return CodeConflict
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return CodeValidationFailed
	case http.StatusTooManyRequests:
		return CodeRateLimited
	case http.StatusServiceUnavailable:
		return CodeUnavailable
	}
	if status >= 400 && status < 500 {
		return CodeValidationFailed
	}
	return CodeInternal
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

// apiPathPrefix is the namespace that answers in the envelope. Everything
// outside it is a URL a person navigates to, not one a client decodes.
const apiPathPrefix = "/api/"

// NotFoundHandler and MethodNotAllowedHandler are the router's fallbacks: the
// first for a request that matched no route at all, the second for one whose
// path matched but whose method did not.
//
// They exist because "every JSON response uses one envelope" was not true at
// the two places a client is most likely to land — a mistyped path and a wrong
// verb. chi's built-in fallbacks write plain text ("404 page not found"), so a
// caller that had just been promised {ok, error, code} got text/plain. api.js
// no longer throws on that — every one of its functions now synthesizes an
// envelope when the body will not parse — but a synthesized {code: "internal"}
// is a guess, and this is the server saying what actually happened.
// The CodeNotFound and CodeMethodNotAllowed
// constants above were unreachable through routing at all: only a handler
// passing those statuses by hand could ever produce them, which is the reverse
// of how a client encounters them.
//
// 405 is the one that bites in practice. scaffold_list registers a GET and no
// POST, so a creation form pointed at a read-only resource lands here rather
// than on a handler — and it is a client-side mistake, which is exactly what
// the envelope's `code` is for.
//
// The split by prefix is the same judgement RequireAuth and RequirePageAuth
// make: an envelope is the right answer under /api/, and the wrong answer for a
// browser that mistyped a page URL, which should get the ordinary page-level
// response its user agent knows how to render. So non-API paths keep the
// standard text response.
func NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, apiPathPrefix) {
			jsonErrorCode(w, CodeNotFound, "Not found", http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	}
}

func MethodNotAllowedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, apiPathPrefix) {
			jsonErrorCode(w, CodeMethodNotAllowed, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}
