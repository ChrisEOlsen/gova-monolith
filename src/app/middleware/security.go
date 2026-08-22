package middleware

import "net/http"

// contentSecurityPolicy is the second line of defence behind the JS rules in
// CLAUDE.md's Critical Constraint 3.
//
// Those rules — textContent not innerHTML, createElement not string
// concatenation — are the primary defence, and they are enforced by review.
// This header is what holds when one of them is missed: `script-src 'self'`
// means an injected <script>, an inline handler, or a javascript: URL does not
// execute even if it reaches the DOM. A template that ships XSS rules and no
// CSP is relying entirely on every future edit being correct.
//
// The policy can be this strict because of how the stack is already built:
//
//   - Every page loads its JS as <script type="module" src="/static/js/...">.
//     There are no inline <script> blocks and no on* attributes anywhere in the
//     generated HTML, so 'unsafe-inline' is not needed for scripts.
//   - Tailwind compiles to a linked stylesheet (entrypoint.sh), and there are
//     no <style> blocks or style="" attributes. Note that this does NOT
//     restrict el.style.setProperty(...) — CSP governs markup and stylesheets,
//     not CSSOM writes from JS, which is how the generated list animations set
//     their stagger index.
//   - Critical Constraint 4 forbids CDN script tags, so 'self' is the whole
//     origin set. api.js only ever fetches same-origin paths, so connect-src
//     needs nothing more either.
//
// object-src 'none' and base-uri 'none' close the two classic bypasses that
// script-src alone leaves open: a plugin document, and a rewritten <base> that
// re-points every relative script URL. frame-ancestors restates
// X-Frame-Options for browsers that honour CSP; both are kept because the older
// header is the one some scanners still look for.
//
// If an app genuinely needs an outside origin — a font host, an image CDN — widen
// the ONE directive that needs it rather than dropping the header.
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

func Security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}
