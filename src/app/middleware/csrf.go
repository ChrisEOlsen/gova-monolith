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

// csrfCookieMaxAge is how long the double-submit cookie lives.
//
// It used to have no MaxAge at all, which made it a BROWSER-SESSION cookie
// while the session cookie is persistent. Those two lifetimes diverging is what
// puts a browser into the state "has a session, has no CSRF cookie" every time
// it is closed and reopened — see the hasSession block in CSRF below. Matching
// the session's own life removes the divergence rather than compensating for
// it. The value is not a secret and is deliberately readable by JS (api.js
// reads it to build the X-CSRF-Token header), so a longer life costs nothing.
//
// Keep this in step with the TTL handed to SetSession at login.
const csrfCookieMaxAge = 24 * 60 * 60

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func CSRFToken(r *http.Request) string {
	v, _ := r.Context().Value(csrfKey).(string)
	return v
}

// isSafeMethod reports whether a method is one a CSRF token is not required
// for — the three RFC 9110 calls "safe", meaning they are not expected to
// change state.
//
// It is the ONE definition used by both halves of CSRF below: the decision to
// mint a cookie, and the decision to verify a header. Two lists that must agree
// are two lists that eventually will not.
func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// isBearerRequest reports whether a request authenticated (or is
// authenticating) with a bearer token, which CSRF never applies to: such a
// client carries no ambient cookie for this origin, so a forged cross-site
// request has nothing to replay. Named rather than inlined because it is the
// shared definition of "this request is not in the browser threat model" for
// BOTH halves of the exemption — the middleware's skip and the route-level
// test that pins it. A future token scheme that ALSO carries a cookie must
// land here first; a single predicate is one thing to update, and this
// comment is where its CSRF consequences get reasoned through.
func isBearerRequest(r *http.Request) bool {
	// login_token is exempted by path, not header: it is the request that
	// ISSUES the token, so it cannot carry one yet. A native app calling it
	// directly was never reachable by a cross-site forgery in the first
	// place.
	if r.URL.Path == "/api/v1/auth/login_token" {
		return true
	}
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bearer-token requests (mobile clients) carry no cookies for this
		// origin, so a forged cross-site request can't replay them the way
		// it can a session cookie — CSRF doesn't apply to them.
		if isBearerRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Read the INCOMING cookie once, before we might set one. Its
		// presence is the signal that this client is a browser participating
		// in the double-submit scheme (an earlier safe-method load set it).
		var token string
		hasCookie := false
		if cookie, err := r.Cookie("csrf_token"); err == nil {
			token = cookie.Value
			hasCookie = true
		} else {
			// No cookie yet. Mint one so browser JS can read it, but only
			// issue it on safe methods — a mutating request that brought no
			// cookie is a native/non-browser client we must NOT force into
			// the scheme (doing so is the bug that 403'd every mobile write).
			token = generateToken()
			if isSafeMethod(r.Method) {
				http.SetCookie(w, &http.Cookie{
					Name:     "csrf_token",
					Value:    token,
					Path:     "/",
					HttpOnly: false,
					Secure:   secureCookies,
					SameSite: http.SameSiteStrictMode,
					MaxAge:   csrfCookieMaxAge,
				})
			}
		}

		// AN AMBIENT SESSION COOKIE IS ITSELF A REASON TO REQUIRE THE TOKEN.
		//
		// The `!hasCookie` escape below used to be justified by this sentence:
		// "a browser forged cross-site POST DOES carry the cookie
		// (auto-attached) but cannot carry the matching header." That sentence
		// was FALSE, and the csrf_token cookie above is why — it is
		// SameSite=Strict, so a cross-site forged POST carries neither cookie,
		// lands in the no-cookie branch, and would be waved through UNVERIFIED.
		//
		// That was not exploitable on its own, because the session cookie is
		// Strict too and is not sent on a cross-site POST either — so the forged
		// request arrives unauthenticated and RequireAuth turns it away. But it
		// means the double-submit check was contributing NOTHING: the entire
		// defence rested on one attribute of a DIFFERENT cookie set in a
		// different file, while this file's comment asserted a mechanism that
		// did not exist. A control whose stated reason is false is a control
		// nobody can reason about the next time either half changes.
		//
		// It stops being theoretical the moment an app serves more than one
		// hostname. SameSite is computed on the REGISTRABLE DOMAIN, so a page on
		// sub.example.com is same-site with example.com and a POST from it
		// carries the session cookie in full. Whether it also carries csrf_token
		// is then a coin toss — the session cookie is persistent and csrf_token
		// used to be a browser-session cookie, so any user who closed and
		// reopened their browser held the first and not the second: exactly the
		// state in which the check skipped itself.
		//
		// Two changes close it, and both matter:
		//   1. Presence of a session cookie forces verification, whether or not
		//      a csrf_token cookie came with it. When it did not, `token` above
		//      is a freshly minted random the client cannot know, so
		//      verification fails closed rather than being skipped.
		//   2. csrf_token is now persistent with the same life as the session
		//      (csrfCookieMaxAge), so the two stop diverging on a browser
		//      restart and a legitimate write cannot land in the fail-closed
		//      case. A browser that somehow does reaches it recovers on the next
		//      page load, which mints a fresh pair.
		//
		// Bearer clients are still exempt at the top of this function and carry
		// no session cookie, so nothing about the native path changes.
		hasSession := false
		if _, err := r.Cookie(SessionCookieName); err == nil {
			hasSession = true
		}

		ctx := context.WithValue(r.Context(), csrfKey, token)

		// ALLOWLIST THE SAFE METHODS, rather than listing the unsafe ones.
		//
		// This was `POST || PUT || DELETE` — three real unsafe methods, and
		// nothing about the line looked wrong. PATCH is not among them, so the
		// check simply did not run on it: a browser's ambient session cookie
		// plus a cross-site form could rewrite whatever the first PATCH handler
		// in the app touched, with no token to match. That is why a denylist is
		// the wrong SHAPE rather than a list with one entry missing — a method
		// nobody thought of defaults to UNPROTECTED, silently, and the symptom
		// is invisible until somebody goes looking for it.
		//
		// Inverted, the default flips: the three methods below are the ones RFC
		// 9110 defines as safe, they are the same three the cookie is minted on
		// above, and every method that exists or is ever added is verified
		// without anyone having to remember to add it here.
		if !isSafeMethod(r.Method) {
			// A client that brought NEITHER cookie is not a browser riding an
			// ambient credential — there is no session for a forgery to spend,
			// so there is nothing for CSRF to protect and forcing the scheme on
			// it is the bug that 403'd every mobile write. A request carrying a
			// session cookie is the opposite case and is verified below.
			if !hasCookie && !hasSession {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			headerToken := r.Header.Get("X-CSRF-Token")
			if !hmac.Equal([]byte(token), []byte(headerToken)) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"ok":false,"error":"invalid CSRF token","code":"forbidden"}`))
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
