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

// SessionCookieName is the one name the session cookie is written, read and
// cleared under. It is exported because csrf.go needs to ask "is this browser
// carrying an ambient credential?" — a question CSRF cannot answer without
// naming the same cookie this file sets, and a second string literal over there
// is a second thing to keep in sync.
const SessionCookieName = "gova_session"

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
		Name:     SessionCookieName,
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
		Name:   SessionCookieName,
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
		cookie, err := r.Cookie(SessionCookieName)
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
// RequirePageAuth is RequireAuth for a HUMAN-FACING URL: no session, redirect
// to the sign-in page instead of writing a JSON 401 nobody will read.
//
// It exists because a page's `auth: true` in api.json used to enforce NOTHING —
// the flag was written to the manifest, rendered nowhere, and read like a
// security control. The defensible half of that was the behaviour: wrapping a
// page shell in RequireAuth would answer a browser with a JSON body, which is
// worse than not guarding it. What was not defensible was a manifest field that
// looks like protection and is inert.
//
// What this actually buys, and what it does not:
//
//   - It does NOT protect data. The shell is inert HTML; every datum on the page
//     comes from an /api/v1/ endpoint, and THOSE are what must carry auth:true.
//     A guard here is a courtesy to the user, not a boundary.
//   - It DOES remove the flash: without it a signed-out visitor gets the full
//     page, and only once its JS module has loaded and called requireAuth() does
//     the redirect happen. The server knew from the cookie alone.
//
// The redirect target is /login, which scaffold_auth registers. A deployment
// without it still gets a correct 302 to a 404 — visibly wrong, rather than
// silently unguarded.
func RequirePageAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserID(r) == 0 {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserID(r) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"ok":false,"error":"unauthorized","code":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
