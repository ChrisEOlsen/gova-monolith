package handlers

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// resetTrustedProxies lets a test change TRUSTED_PROXY_CIDRS despite the
// sync.Once that parses it.
func resetTrustedProxies(t *testing.T) {
	t.Helper()
	trustedProxiesOnce = sync.Once{}
	trustedProxyNets = nil
	t.Cleanup(func() {
		trustedProxiesOnce = sync.Once{}
		trustedProxyNets = nil
	})
}

func requestFrom(remoteAddr string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

// A direct caller does not get to name itself. Without this, anyone who can
// reach the origin sets a forwarding header and mints a fresh rate-limit bucket
// per request — unlimited password guessing with the limiter reporting healthy.
func TestClientIP_UntrustedPeerCannotSpoofItsAddress(t *testing.T) {
	resetTrustedProxies(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8")

	req := requestFrom("203.0.113.9:5555", map[string]string{
		"CF-Connecting-IP": "1.2.3.4",
		"X-Forwarded-For":  "5.6.7.8",
	})
	if got := clientIP(req); got != "203.0.113.9" {
		t.Errorf("clientIP = %q, want the peer address 203.0.113.9", got)
	}
}

func TestClientIP_TrustedProxyUsesCFConnectingIP(t *testing.T) {
	resetTrustedProxies(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8")

	req := requestFrom("10.1.2.3:5555", map[string]string{"CF-Connecting-IP": "198.51.100.7"})
	if got := clientIP(req); got != "198.51.100.7" {
		t.Errorf("clientIP = %q, want 198.51.100.7", got)
	}
}

// An unparseable header is one we do not understand; inventing a bucket key out
// of it would let one caller occupy arbitrarily many.
func TestClientIP_TrustedProxyIgnoresUnparseableCFHeader(t *testing.T) {
	resetTrustedProxies(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8")

	req := requestFrom("10.1.2.3:5555", map[string]string{
		"CF-Connecting-IP": "not-an-ip",
		"X-Forwarded-For":  "198.51.100.7",
	})
	if got := clientIP(req); got != "198.51.100.7" {
		t.Errorf("clientIP = %q, want the X-Forwarded-For fallback", got)
	}
}

// Behind a reverse proxy with no usable header, every caller shares the proxy's
// address as one bucket — five bad logins would lock the whole deployment.
func TestClientIP_FallsBackToXForwardedFor(t *testing.T) {
	resetTrustedProxies(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "172.16.0.0/12")

	req := requestFrom("172.18.0.5:5555", map[string]string{"X-Forwarded-For": "198.51.100.7"})
	if got := clientIP(req); got != "198.51.100.7" {
		t.Errorf("clientIP = %q, want 198.51.100.7", got)
	}
}

// A client can prepend anything to X-Forwarded-For, so everything left of the
// first hop we trust is attacker-authored. Walk from the right.
func TestClientIP_TakesTheRightmostUntrustedEntry(t *testing.T) {
	resetTrustedProxies(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "172.16.0.0/12")

	req := requestFrom("172.18.0.5:5555", map[string]string{
		"X-Forwarded-For": "1.2.3.4, 198.51.100.7, 172.18.0.9",
	})
	if got := clientIP(req); got != "198.51.100.7" {
		t.Errorf("clientIP = %q, want 198.51.100.7 (rightmost untrusted)", got)
	}
}

// Stop at a malformed entry rather than skipping it: skipping would let a
// client push the real boundary leftward by inserting garbage.
func TestClientIP_MalformedForwardedEntryStopsTheWalk(t *testing.T) {
	resetTrustedProxies(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "172.16.0.0/12")

	req := requestFrom("172.18.0.5:5555", map[string]string{
		"X-Forwarded-For": "198.51.100.7, garbage",
	})
	if got := clientIP(req); got != "172.18.0.5" {
		t.Errorf("clientIP = %q, want the peer address after a malformed entry", got)
	}
}

func TestClientIP_AllProxiesFallsBackToPeer(t *testing.T) {
	resetTrustedProxies(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "172.16.0.0/12")

	req := requestFrom("172.18.0.5:5555", map[string]string{"X-Forwarded-For": "172.18.0.9, 172.18.0.10"})
	if got := clientIP(req); got != "172.18.0.5" {
		t.Errorf("clientIP = %q, want 172.18.0.5", got)
	}
}

// SplitHostPort, not a manual colon search: the last colon of "[::1]:54321" is
// inside the brackets, so a hand-rolled split keys one caller as "[::1]" and
// another spelling of the same host as "::1".
func TestPeerIP_IPv6HasNoBrackets(t *testing.T) {
	if got := peerIP("[2001:db8::1]:54321"); got != "2001:db8::1" {
		t.Errorf("peerIP = %q, want 2001:db8::1", got)
	}
	if got := peerIP("203.0.113.9"); got != "203.0.113.9" {
		t.Errorf("peerIP without a port = %q, want 203.0.113.9", got)
	}
}

func TestClientIP_LoopbackIsTrustedByDefault(t *testing.T) {
	resetTrustedProxies(t)
	req := requestFrom("127.0.0.1:5555", map[string]string{"X-Forwarded-For": "198.51.100.7"})
	if got := clientIP(req); got != "198.51.100.7" {
		t.Errorf("clientIP = %q, want 198.51.100.7", got)
	}
}

// An explicitly empty value says "there is no proxy" — RemoteAddr is the client
// and no header may override it.
func TestClientIP_EmptyTrustedSetTrustsNoProxy(t *testing.T) {
	resetTrustedProxies(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	req := requestFrom("127.0.0.1:5555", map[string]string{"X-Forwarded-For": "198.51.100.7"})
	if got := clientIP(req); got != "127.0.0.1" {
		t.Errorf("clientIP = %q, want 127.0.0.1", got)
	}
}
