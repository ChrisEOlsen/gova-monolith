package handlers

import (
	"encoding/json"
	"errors"
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

// envelope is the single response shape for every JSON endpoint. See
// docs/API-CONTRACT.md — clients depend on this shape.
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

// codeForStatus maps an HTTP status onto the failure kind a client switches on.
//
// The default is split by class rather than falling through to `internal`: a
// 4xx is by definition something about the request, so an unenumerated one is
// validation_failed. Only 5xx is ours.
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

// normalizeData replaces a nil slice with an empty one. encoding/json marshals
// a nil slice held in a non-nil interface as null, and omitempty does not strip
// it — so without this a typed client decoding an array fails on an empty
// result set. Models initialize their slices non-nil too; this covers
// hand-written handlers.
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
	// API responses are per-session data. Without this the back button can
	// serve a logged-out visitor the previous user's /auth/me out of the
	// browser's history cache on a shared machine.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(env)
}

// maxRequestBody caps a JSON request body. Without a cap, one POST feeds the
// decoder unbounded memory, and SQLite's single writer then serializes the
// flood behind it. Raise it on the one handler that needs it — an upload — not
// here.
const maxRequestBody = 1 << 20 // 1 MiB

// readJSON decodes a capped request body into dst, writing the failure response
// itself and reporting whether the caller may continue.
//
// Every handler that reads a body goes through this. MaxBytesReader is what
// makes the cap real: it stops the read at the limit rather than trusting a
// Content-Length the client wrote.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

func jsonOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, envelope{OK: true, Data: normalizeData(data)})
}

// jsonList is the paginated counterpart to jsonOK.
func jsonList(w http.ResponseWriter, items any, meta Meta) {
	writeJSON(w, http.StatusOK, envelope{OK: true, Data: normalizeData(items), Meta: &meta})
}

// jsonError derives the machine-readable code from the status.
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

// summarizeFields takes the alphabetically first field, so the message is
// deterministic rather than dependent on Go's randomized map iteration.
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

// apiPathPrefix is the namespace that answers in the envelope.
const apiPathPrefix = "/api/"

// NotFoundHandler and MethodNotAllowedHandler are the router's fallbacks: a
// path that matched no route, and a path that matched with the wrong method.
// chi's built-ins answer in plain text, which breaks the envelope for the two
// failures a client is most likely to hit.
//
// Split by prefix, the same judgement RequireAuth and RequirePageAuth make: an
// envelope under /api/, and the browser's ordinary 404 page elsewhere.
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
