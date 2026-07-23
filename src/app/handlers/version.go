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
