package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type ctxKey string

const userIDKey ctxKey = "user_id"

type sessionPayload struct {
	UserID    int64 `json:"uid"`
	ExpiresAt int64 `json:"exp"`
}

var (
	sessionKey    = []byte(os.Getenv("SESSION_SECRET"))
	secureCookies = os.Getenv("APP_ENV") == "production"
)

func SetSession(w http.ResponseWriter, userID int64, ttl time.Duration) {
	payload, _ := json.Marshal(sessionPayload{
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl).Unix(),
	})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, sessionKey)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name:     "gova_session",
		Value:    encoded + "|" + sig,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

func ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   "gova_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

func UserID(r *http.Request) int64 {
	v, _ := r.Context().Value(userIDKey).(int64)
	return v
}

func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("gova_session")
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		parts := strings.SplitN(cookie.Value, "|", 2)
		if len(parts) != 2 {
			next.ServeHTTP(w, r)
			return
		}
		encoded, sig := parts[0], parts[1]
		mac := hmac.New(sha256.New, sessionKey)
		mac.Write([]byte(encoded))
		expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(sig), []byte(expected)) {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		var p sessionPayload
		if err := json.Unmarshal(raw, &p); err != nil || time.Now().Unix() > p.ExpiresAt {
			next.ServeHTTP(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, p.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth returns JSON 401 for unauthenticated API requests.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserID(r) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"ok":false,"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
