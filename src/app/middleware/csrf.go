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

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func CSRFToken(r *http.Request) string {
	v, _ := r.Context().Value(csrfKey).(string)
	return v
}

func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bearer-token requests (mobile clients) carry no cookies for this
		// origin, so a forged cross-site request can't replay them the way
		// it can a session cookie — CSRF doesn't apply to them.
		//
		// login_token is exempted by path for the same reason even though
		// it can't carry a Bearer header yet — it's the request that issues
		// the token, so there's nothing to attach. CSRF's threat model is a
		// browser auto-attaching credentials to a forged cross-site request;
		// a native app calling this endpoint directly was never reachable
		// that way in the first place.
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.URL.Path == "/api/auth/login_token" {
			next.ServeHTTP(w, r)
			return
		}

		token := ""
		if cookie, err := r.Cookie("csrf_token"); err == nil {
			token = cookie.Value
		} else {
			token = generateToken()
			http.SetCookie(w, &http.Cookie{
				Name:     "csrf_token",
				Value:    token,
				Path:     "/",
				HttpOnly: false,
				Secure:   secureCookies,
				SameSite: http.SameSiteStrictMode,
			})
		}

		ctx := context.WithValue(r.Context(), csrfKey, token)

		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			headerToken := r.Header.Get("X-CSRF-Token")
			if !hmac.Equal([]byte(token), []byte(headerToken)) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"ok":false,"error":"invalid CSRF token"}`))
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
