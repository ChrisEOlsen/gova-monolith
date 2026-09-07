package middleware

import "net/http"

// contentSecurityPolicy is the backstop behind the JS rules in CLAUDE.md:
// script-src 'self' means an injected script does not execute even if it
// reaches the DOM. The policy can be this strict because every page loads its
// JS as an external module and Tailwind compiles to a linked stylesheet — no
// inline script or style anywhere.
//
// object-src and base-uri close the two classic script-src bypasses: a plugin
// document, and a rewritten <base> that re-points every relative script URL.
//
// If an app needs an outside origin, widen the one directive that needs it
// rather than dropping the header.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'self'"

// strictTransportSecurity is sent unconditionally. A browser ignores the
// header on a plain-HTTP response, so there is nothing to gate on APP_ENV and
// no way for a local dev run to poison a later production one. Two years with
// subdomains included; no `preload`, which is a submission to a browser vendor
// and not a decision a template makes for a downstream app.
const strictTransportSecurity = "max-age=63072000; includeSubDomains"

func Security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", strictTransportSecurity)
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}
