package handlers

import (
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Whose address is this? It keys the rate limiter, so it is a security
// decision: a caller that can name itself gets an unlimited supply of buckets,
// and a proxy that cannot be seen through puts every caller in one bucket.
//
// The rule is one trusted-peer notion used both ways:
//   - peer is not a trusted proxy -> ignore all headers, use the peer address
//   - peer is a trusted proxy     -> CF-Connecting-IP, else the right-most
//     X-Forwarded-For entry that is not itself a trusted hop, else the peer
//
// Right-most-untrusted because a client can prepend anything it likes to
// X-Forwarded-For; everything left of the first hop we trust is attacker-authored.

// defaultTrustedProxyCIDRs is loopback plus the private ranges a sibling
// ingress container arrives from. Override with TRUSTED_PROXY_CIDRS
// (comma-separated); set it empty to say there is no proxy at all.
var defaultTrustedProxyCIDRs = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16",
	"fc00::/7", "fe80::/10",
}

var (
	trustedProxiesOnce sync.Once
	trustedProxyNets   []*net.IPNet
)

func trustedProxies() []*net.IPNet {
	trustedProxiesOnce.Do(func() {
		specs := defaultTrustedProxyCIDRs
		if raw, ok := os.LookupEnv("TRUSTED_PROXY_CIDRS"); ok {
			specs = nil
			for _, part := range strings.Split(raw, ",") {
				if part = strings.TrimSpace(part); part != "" {
					specs = append(specs, part)
				}
			}
		}
		for _, spec := range specs {
			_, netw, err := net.ParseCIDR(spec)
			if err != nil {
				// Loud but not fatal: a typo here must not stop the process
				// booting, and must not silently widen the trusted set either.
				log.Printf("handlers: TRUSTED_PROXY_CIDRS: ignoring %q: %v", spec, err)
				continue
			}
			trustedProxyNets = append(trustedProxyNets, netw)
		}
	})
	return trustedProxyNets
}

func isTrustedProxy(addr string) bool {
	ip := net.ParseIP(strings.TrimSpace(addr))
	if ip == nil {
		return false
	}
	for _, netw := range trustedProxies() {
		if netw.Contains(ip) {
			return true
		}
	}
	return false
}

// peerIP is RemoteAddr without its port. SplitHostPort rather than a manual
// colon search, so an IPv6 "[::1]:54321" does not become "[::1]".
func peerIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(strings.TrimSpace(remoteAddr), "[]")
}

// clientIP returns the address the rate limiter buckets this request under.
func clientIP(r *http.Request) string {
	peer := peerIP(r.RemoteAddr)
	if !isTrustedProxy(peer) {
		return peer
	}
	// Cloudflare's edge overwrites this header, so a client-supplied value
	// never survives on a tunnelled path. Validated as an address: an
	// unparseable value would let one caller mint arbitrarily many buckets.
	if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); net.ParseIP(cf) != nil {
		return cf
	}
	if fwd := forwardedFor(r.Header.Get("X-Forwarded-For")); fwd != "" {
		return fwd
	}
	warnMissingForwardedIP(peer)
	return peer
}

// forwardedFor returns the right-most X-Forwarded-For entry that is not one of
// our own proxies, or "".
func forwardedFor(header string) string {
	parts := strings.Split(header, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.Trim(strings.TrimSpace(parts[i]), "[]")
		if candidate == "" {
			continue
		}
		if net.ParseIP(candidate) == nil {
			// Stop rather than skip: skipping would let a client push the real
			// boundary leftward by inserting garbage.
			return ""
		}
		if !isTrustedProxy(candidate) {
			return candidate
		}
	}
	return ""
}

var warnedMissingForwardedIP sync.Once

// warnMissingForwardedIP fires when a trusted proxy relayed a request with no
// usable client address, which collapses every caller into one bucket. Once per
// process: it is a deployment fault, not a per-request event.
func warnMissingForwardedIP(peer string) {
	warnedMissingForwardedIP.Do(func() {
		log.Printf("handlers: request from trusted proxy %s carried no CF-Connecting-IP "+
			"or usable X-Forwarded-For — every caller now shares one rate-limit bucket", peer)
	})
}
