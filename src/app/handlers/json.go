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
