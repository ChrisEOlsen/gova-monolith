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

// UserIDKey is exported so tests can frame an authenticated request without
// minting a real cookie.
const UserIDKey ctxKey = "user_id"

// SessionCookieName is shared with csrf.go, which needs to know whether the
// browser is carrying an ambient credential.
const SessionCookieName = "gova_session"

// SessionTTL is how long a session lasts. csrf.go matches it, so the two
// cookies expire together.
const SessionTTL = 24 * time.Hour

// EpochStore reports a user's current session epoch. Implemented by
// models.UserModel; declared here so this package stays a leaf.
type EpochStore interface {
	SessionEpoch(userID int64) int64
}

// TokenStore resolves a native client's bearer token to its user, or reports
// that it is unknown, expired or revoked. Implemented by
// models.MobileTokenModel, which does the hashing — the raw token never leaves
// the request and this package never learns how it is stored.
type TokenStore interface {
	UserIDForToken(rawToken string) (int64, bool)
}

type sessionPayload struct {
	UserID    int64 `json:"uid"`
	Epoch     int64 `json:"epo"`
	ExpiresAt int64 `json:"exp"`
}

var secureCookies = os.Getenv("APP_ENV") == "production"

// sessionSecret is read per call so a value set after package init (as tests
// do) is honored. The length check is free next to the HMAC itself.
func sessionSecret() []byte {
	key := []byte(os.Getenv("SESSION_SECRET"))
	if len(key) < 32 {
		panic("middleware: SESSION_SECRET must be set and at least 32 characters")
	}
	return key
}

// SetSession issues a session cookie bound to the user's current epoch.
func SetSession(w http.ResponseWriter, userID, epoch int64) {
	payload, _ := json.Marshal(sessionPayload{
		UserID:    userID,
		Epoch:     epoch,
		ExpiresAt: time.Now().Add(SessionTTL).Unix(),
	})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	http.SetCookie(w, sessionCookie(encoded+"|"+sign(encoded), int(SessionTTL.Seconds())))
}

// ClearSession deletes the cookie from this browser. It cannot reach copies on
// other devices — see models.UserModel.RevokeAllSessions for that.
func ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, sessionCookie("", -1))
}

// sessionCookie keeps every attribute in one place: a deletion cookie must
// carry the same attributes as the one it replaces or some browsers ignore it.
func sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	}
}

func sign(encoded string) string {
	mac := hmac.New(sha256.New, sessionSecret())
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// UserID returns the authenticated user, or 0.
func UserID(r *http.Request) int64 {
	v, _ := r.Context().Value(UserIDKey).(int64)
	return v
}

// Auth identifies the caller and puts their user id in the request context. It
// never blocks: RequireAuth and RequirePageAuth do that.
//
// Both credential kinds are read here, cookie first. That is what makes
// `auth: true` on a route mean the same thing to a browser and to a native
// client — before this, RequireAuth read only the cookie, so every guarded
// endpoint answered 401 to the iOS client no matter how valid its token was,
// and the bearer credential reached exactly three hand-written handlers.
func Auth(sessions EpochStore, tokens TokenStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := sessionUser(r, sessions)
			if !ok {
				uid, ok = bearerUser(r, tokens)
			}
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserIDKey, uid)))
		})
	}
}

// BearerToken returns the raw token from an Authorization header, or "".
// Exported because handlers need the same value to revoke it on logout.
func BearerToken(r *http.Request) string {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimPrefix(auth, prefix)
}

func bearerUser(r *http.Request, tokens TokenStore) (int64, bool) {
	if tokens == nil {
		return 0, false
	}
	token := BearerToken(r)
	if token == "" {
		return 0, false
	}
	return tokens.UserIDForToken(token)
}

func sessionUser(r *http.Request, store EpochStore) (int64, bool) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return 0, false
	}
	encoded, sig, found := strings.Cut(cookie.Value, "|")
	if !found || !hmac.Equal([]byte(sig), []byte(sign(encoded))) {
		return 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return 0, false
	}
	var p sessionPayload
	if err := json.Unmarshal(raw, &p); err != nil || time.Now().Unix() > p.ExpiresAt {
		return 0, false
	}
	// Checked only after the signature, so the epoch claim is ours. A cookie
	// issued before a revocation is behind the stored epoch and dies here.
	if p.Epoch < store.SessionEpoch(p.UserID) {
		return 0, false
	}
	return p.UserID, true
}

// RequireAuth guards a JSON endpoint.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserID(r) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"ok":false,"error":"unauthorized","code":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequirePageAuth guards a human-facing URL by redirecting, since a browser
// navigation must not be answered with a JSON body. The page shell holds no
// data — the endpoints behind it are the real boundary; this only saves the
// visitor from seeing a page they are about to be bounced off.
func RequirePageAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserID(r) == 0 {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
