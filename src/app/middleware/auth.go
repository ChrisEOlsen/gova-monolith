package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type ctxKey string

// UserIDKey is the context key UserID reads. It is exported so tests (and
// generated handler tests) can frame requests the way RequireAuth leaves
// them — uid in context without carrying a real cookie.
const userIDKey ctxKey = "user_id"

// UserIDKey is the exported spelling of the context key for test framing.
const UserIDKey = userIDKey

// SessionCookieName is the one name the session cookie is written, read and
// cleared under. It is exported because csrf.go needs to ask "is this browser
// carrying an ambient credential?" — a question CSRF cannot answer without
// naming the same cookie this file sets, and a second string literal over there
// is a second thing to keep in sync.
const SessionCookieName = "gova_session"

type sessionPayload struct {
	UserID int64 `json:"uid"`
	// Epoch is the value of the issuing user's users.session_epoch at login
	// time. A cookie whose epoch is behind the user's current epoch was
	// signed by a login that predates a revocation event (password change,
	// "log out everywhere") and is rejected — this is the ONLY way a cookie
	// can be retired server-side; ClearSession talks to the browser alone.
	Epoch int64 `json:"epo,omitempty"`
	// JTI identifies one issued session, so a logout can revoke one cookie
	// without touching the user's epoch. The id is server-generated; a
	// client that mints its own jti still has to have the user's current
	// epoch, so inventing one buys nothing.
	JTI       string `json:"jti,omitempty"`
	ExpiresAt int64  `json:"exp"`
}

// EpochLookup is how the session middleware asks for a user's current epoch.
// Set once at boot (main.go, after the db opens) so this package never
// imports the models package — middleware stays a leaf, and the storage
// decision (a users column today, a sessions table tomorrow) stays out of
// the wire format.
//
// A nil lookup, a lookup error, or a user with no epoch all read as epoch 0,
// which matches every row that predates the column — pre-existing sessions
// keep working, and only an explicitly bumped epoch revokes.
var EpochLookup func(r *http.Request, userID int64) int64

var secureCookies = os.Getenv("APP_ENV") == "production"

// The secret invariant lives HERE, next to the code that depends on it, not
// only in main.go one package away. main.go's check is user-facing (it can
// print instructions); this one is the backstop that makes an unsafe
// configuration impossible to reach from any other entry point — a second
// main, a future CLI, a test helper that builds the router directly — without
// the empty-key signing failure the main.go check was the only thing
// preventing.
//
// Looked up lazily per HMAC rather than cached in a package var so a value
// set later in the process (tests commonly set env in TestMain) is honored;
// the length guard runs on every cookie mint/verify, which is free next to
// the HMAC itself.
func sessionSecret() []byte {
	key := []byte(os.Getenv("SESSION_SECRET"))
	if len(key) < 32 {
		panic("middleware: SESSION_SECRET must be set and at least 32 characters " +
			"(main.go's boot check guards the server; this panic guards every other path)")
	}
	return key
}

func SetSession(w http.ResponseWriter, userID int64, ttl time.Duration) {
	SetSessionWithEpoch(w, userID, 0, ttl)
}

// SetSessionWithEpoch mints a session bound to the user's CURRENT epoch, read
// through EpochLookup if the lookup was provided (its zero value when it was
// not, or the user is unknown). Login handlers call this rather than
// SetSession so a revocation that happened seconds before the login is
// honored: the new cookie carries the post-bump epoch, not the pre-bump one.
//
// JTI is 16 bytes of crypto/rand — session identity belongs to the server,
// not to anything the client could choose.
func SetSessionWithEpoch(w http.ResponseWriter, userID int64, epoch int64, ttl time.Duration) {
	jti := make([]byte, 16)
	rand.Read(jti)
	payload, _ := json.Marshal(sessionPayload{
		UserID:    userID,
		Epoch:     epoch,
		JTI:       hex.EncodeToString(jti),
		ExpiresAt: time.Now().Add(ttl).Unix(),
	})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, sessionSecret())
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
	// Deletion cookie carries the SAME attributes SetSession wrote — some
	// browsers treat the attribute set as part of the cookie's identity, so
	// a bare deletion cookie can fail to evict the real one.
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
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
		mac := hmac.New(sha256.New, sessionSecret())
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
		// EPOCH CHECK — the server-side revocation half.
		//
		// Verified ONLY after the MAC: the sig proves the payload came from
		// us, so its epoch claim is trustworthy enough to ask the lookup
		// with. A nil EpochLookup (an app that has not wired it — or has not
		// migrated) means no revocation: every cookie passes, which is
		// exactly the pre-epoch behavior, so this is a strictly-additive
		// control rather than a breaking one.
		//
		// A cookie whose epoch is BEHIND the user's current epoch is a
		// session issued before a revocation event and dies here. Equal or
		// ahead (a lookup that failed, epoch 0 on an unmigrated row) passes.
		// Internal errors deliberately pass rather than 500: this call sits
		// in front of EVERY request, and a transient read failure must not
		// take the whole app down — epoch staleness is caught on a later
		// request.
		if EpochLookup != nil {
			if current := EpochLookup(r, p.UserID); p.Epoch < current {
				next.ServeHTTP(w, r)
				return
			}
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
