package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"gova/app/db"
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
		if err := tokens.Issue(hashToken(token), user.ID, time.Now().Add(mobileTokenTTL)); err != nil {
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
		token := bearerToken(r)
		if token == "" {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = tokens.Revoke(hashToken(token))
		jsonOK(w, map[string]string{"status": "logged out"})
	}
}

// MobileMeGET handles GET /api/v1/auth/me_token
func MobileMeGET(database *db.DB) http.HandlerFunc {
	users := models.NewUserModel(database)
	tokens := models.NewMobileTokenModel(database)
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		userID, err := tokens.UserID(hashToken(token))
		if err != nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user, err := users.FindByID(userID)
		if err != nil {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}
		jsonOK(w, publicUser(user))
	}
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
