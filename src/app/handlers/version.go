package handlers

import "net/http"

// APIVersion identifies the contract this server speaks. MinClientVersion is
// the oldest client build it will still serve correctly — a native client
// checks it at launch and blocks itself if it is older, turning an opaque
// decode failure into an actionable "update required" prompt.
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
