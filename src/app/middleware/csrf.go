package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

const csrfKey ctxKey = "csrf_token"

const CSRFCookieName = "csrf_token"

// generateToken returns 32 random bytes as hex. crypto/rand.Read never returns
// an error on Go 1.24+ — it panics rather than produce weak randomness.
func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func CSRFToken(r *http.Request) string {
	v, _ := r.Context().Value(csrfKey).(string)
	return v
}

// isSafeMethod reports the RFC 9110 safe methods. Allowlisted rather than
// listing the unsafe ones, so a method nobody thought of is still verified.
func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// isBearerRequest reports whether the caller authenticates with a bearer token.
// Such a client holds no ambient cookie for this origin, so there is nothing a
// cross-site request could replay.
//
// This used to also exempt /api/v1/auth/login_token by path, on the grounds
// that the request issuing a token cannot yet carry one. The path string was
// load-bearing security pinned to a route name — rename the route and the
// exemption silently moves. It was also unnecessary: a native client holds no
// cookies at all, so the verification below never fires for it. What the path
// exemption actually covered was a *browser* calling login_token, which should
// send a CSRF token like every other unsafe request from a browser.
func isBearerRequest(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// CSRF implements the double-submit cookie scheme.
//
// The token is minted on safe methods and read back from the cookie by api.js,
// which sends it as X-CSRF-Token. Verification runs on any unsafe request that
// carries a cookie for this origin — that is what identifies a browser holding
// ambient authority a forged cross-site request could spend. A caller with no
// cookies (a native client, a webhook) has nothing to replay and is let through
// rather than being forced into a scheme it cannot participate in.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isBearerRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		token, hasToken := csrfCookie(r)
		if !hasToken {
			token = generateToken()
			if isSafeMethod(r.Method) {
				http.SetCookie(w, &http.Cookie{
					Name:     CSRFCookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: false, // api.js must read it
					Secure:   secureCookies,
					SameSite: http.SameSiteStrictMode,
					MaxAge:   int(SessionTTL.Seconds()),
				})
			}
		}

		ctx := context.WithValue(r.Context(), csrfKey, token)
		if !isSafeMethod(r.Method) && (hasToken || hasSessionCookie(r)) {
			// A missing cookie leaves token as a fresh random the client cannot
			// know, so this fails closed rather than skipping.
			if !hmac.Equal([]byte(token), []byte(r.Header.Get("X-CSRF-Token"))) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"ok":false,"error":"invalid CSRF token","code":"forbidden"}`))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func csrfCookie(r *http.Request) (string, bool) {
	c, err := r.Cookie(CSRFCookieName)
	if err != nil {
		return "", false
	}
	return c.Value, true
}

func hasSessionCookie(r *http.Request) bool {
	_, err := r.Cookie(SessionCookieName)
	return err == nil
}
