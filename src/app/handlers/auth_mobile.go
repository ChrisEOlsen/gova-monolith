package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"gova/app/db"
	"gova/app/middleware"
	"gova/app/models"
)

// mobileTokenTTL is how long a bearer token lasts before the client must log in
// again.
const mobileTokenTTL = 30 * 24 * time.Hour

// MobileLoginPOST handles POST /api/v1/auth/login_token
// Returns { token, user }. Only the token's SHA-256 hash is stored.
func MobileLoginPOST(database *db.DB) http.HandlerFunc {
	users := models.NewUserModel(database)
	tokens := models.NewMobileTokenModel(database)
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := decodeCredentials(w, r)
		if !ok {
			return
		}
		user := authenticate(w, users, c, loginTokenBucket(clientIP(r)))
		if user == nil {
			return
		}

		// crypto/rand.Read never returns an error on Go 1.24+ — it panics
		// rather than produce weak randomness.
		raw := make([]byte, 32)
		rand.Read(raw)
		token := hex.EncodeToString(raw)
		if err := tokens.Issue(models.HashToken(token), user.ID, time.Now().Add(mobileTokenTTL)); err != nil {
			jsonError(w, "Something went wrong. Try again.", http.StatusInternalServerError)
			return
		}

		jsonOK(w, map[string]any{"token": token, "user": publicUser(user)})
	}
}

// MobileLogoutDELETE handles DELETE /api/v1/auth/logout_token.
// Always 200 for a well-formed request — it does not reveal whether the token
// existed.
func MobileLogoutDELETE(database *db.DB) http.HandlerFunc {
	tokens := models.NewMobileTokenModel(database)
	return func(w http.ResponseWriter, r *http.Request) {
		token := middleware.BearerToken(r)
		if token == "" {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = tokens.Revoke(models.HashToken(token))
		jsonOK(w, map[string]string{"status": "logged out"})
	}
}

// MobileMeGET handles GET /api/v1/auth/me_token
//
// The bearer token is resolved by middleware.Auth, the same place the session
// cookie is, so this reads the user id from the context exactly as MeGET does.
func MobileMeGET(database *db.DB) http.HandlerFunc {
	users := models.NewUserModel(database)
	return func(w http.ResponseWriter, r *http.Request) {
		uid := middleware.UserID(r)
		if uid == 0 {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := users.FindByID(uid)
		if err != nil {
			// 401, not 404. A live token whose user row is gone is a dead
			// credential, and "not found" here would answer a different
			// question than every other failure on this route.
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		jsonOK(w, publicUser(user))
	}
}
