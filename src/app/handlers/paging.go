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
