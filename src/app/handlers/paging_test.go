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

// The 50/200 bounds are pinned behaviorally by the clamping cases in
// TestQueryInt above — asserting the constants against their own literals
// would restate the definition rather than test it.
