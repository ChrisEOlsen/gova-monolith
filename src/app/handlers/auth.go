package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gova/app/db"
	"gova/app/middleware"
	"gova/app/models"
)

// dummyHash lets the unknown-email path pay the same bcrypt cost as the
// wrong-password path, so response time cannot be used to enumerate accounts.
var dummyHash = mustDummyHash()

func mustDummyHash() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return h
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	var c credentials
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return c, false
	}
	c.Email = strings.TrimSpace(c.Email)
	if c.Email == "" || c.Password == "" {
		jsonError(w, "email and password are required", http.StatusBadRequest)
		return c, false
	}
	return c, true
}

// authenticate checks credentials behind both rate-limit buckets. It writes the
// response and returns nil on any failure.
//
// The buckets are read before the user lookup, so a locked-out answer costs the
// same and says the same whether or not the address has an account.
func authenticate(w http.ResponseWriter, users *models.UserModel, c credentials, ipBucket string) *models.User {
	accountBucket := loginAccountBucket(c.Email)
	if limited(w, users, ipBucket, accountBucket) {
		return nil
	}

	user, err := users.FindByEmail(c.Email)
	if err != nil {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(c.Password))
		recordLoginFailure(users, ipBucket, accountBucket)
		jsonError(w, "Invalid email or password.", http.StatusUnauthorized)
		return nil
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(c.Password)) != nil {
		recordLoginFailure(users, ipBucket, accountBucket)
		jsonError(w, "Invalid email or password.", http.StatusUnauthorized)
		return nil
	}

	clearBuckets(users, ipBucket, accountBucket)
	return user
}

func publicUser(u *models.User) map[string]any {
	return map[string]any{"id": u.ID, "name": u.Name, "email": u.Email}
}

// LoginPOST handles POST /api/v1/auth/login
func LoginPOST(database *db.DB) http.HandlerFunc {
	users := models.NewUserModel(database)
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := decodeCredentials(w, r)
		if !ok {
			return
		}
		user := authenticate(w, users, c, loginBucket(clientIP(r)))
		if user == nil {
			return
		}
		// Read the epoch after authenticating, so a revocation that happened
		// moments ago is honored and this new cookie is not born stale.
		middleware.SetSession(w, user.ID, users.SessionEpoch(user.ID))
		jsonOK(w, publicUser(user))
	}
}

// LogoutPOST handles POST /api/v1/auth/logout — this browser only.
func LogoutPOST() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		middleware.ClearSession(w)
		jsonOK(w, map[string]string{"status": "logged out"})
	}
}

// LogoutAllPOST handles POST /api/v1/auth/logout_all — every device, by
// bumping the epoch every issued cookie was signed against.
func LogoutAllPOST(database *db.DB) http.HandlerFunc {
	users := models.NewUserModel(database)
	return func(w http.ResponseWriter, r *http.Request) {
		uid := middleware.UserID(r)
		if err := users.BumpSessionEpoch(uid); err != nil {
			log.Printf("handlers: session epoch bump failed for user %d: %v", uid, err)
			jsonError(w, "Something went wrong. Try again.", http.StatusInternalServerError)
			return
		}
		middleware.ClearSession(w)
		jsonOK(w, map[string]string{"status": "logged out everywhere"})
	}
}

// MeGET handles GET /api/v1/auth/me
func MeGET(database *db.DB) http.HandlerFunc {
	users := models.NewUserModel(database)
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := users.FindByID(middleware.UserID(r))
		if err != nil {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}
		jsonOK(w, publicUser(user))
	}
}
