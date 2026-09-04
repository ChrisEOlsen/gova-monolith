package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"strings"

	"gova/app/models"
)

// Rate-limit bucket keys. The key is the isolation: a successful login deletes
// its buckets, so two actions spelling a key the same way are one action to the
// limiter. Hence one namespace per endpoint, and two kinds of bucket with
// deliberately different budgets. See docs/DECISIONS.md § 4.
const (
	maxAttemptsPerIP      = 5
	maxAttemptsPerAccount = 20
)

func loginBucket(ip string) string      { return "login:" + ip }
func loginTokenBucket(ip string) string { return "login_token:" + ip }

// loginAccountBucket meters the account being attacked, shared by both login
// endpoints. Derived from the submitted address before any lookup and counted
// on the unknown-email path too, so it is not an enumeration oracle. Hashed
// because unauthenticated callers write this table. Lowercased, or varying the
// case bypasses the control.
func loginAccountBucket(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return "login_account:" + hex.EncodeToString(sum[:16])
}

// registerBucket meters account creation, which is also the 409 revealing
// whether an address already has an account. Every attempt counts, not only
// failures — creating accounts is the thing being limited.
func registerBucket(ip string) string { return "register:" + ipv6Prefix(ip) }

// ipv6Prefix widens an IPv6 address to its /64 — the smallest block an ISP
// routes as a unit — so rotating interface identifiers inside one block cannot
// mint fresh budgets. IPv4 is scarce enough per-address and passes through.
func ipv6Prefix(ip string) string {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.To16().Mask(net.CIDRMask(64, 128)).String()
}

// limited reports whether any bucket is locked, writing the response itself.
// A read failure is a 500, not a 429: "too many attempts" during a database
// outage sends the operator to the wrong place.
func limited(w http.ResponseWriter, m *models.UserModel, buckets ...string) bool {
	for _, bucket := range buckets {
		locked, err := m.IsRateLimited(bucket)
		if err != nil {
			log.Printf("handlers: rate-limit read failed for %q: %v", bucket, err)
			jsonError(w, "Something went wrong. Try again.", http.StatusInternalServerError)
			return true
		}
		if locked {
			jsonError(w, "Too many attempts. Try again in 15 minutes.", http.StatusTooManyRequests)
			return true
		}
	}
	return false
}

// recordLoginFailure counts one failure against both buckets. A write failure
// means the attempt went uncounted and the limiter is silently off, so log it —
// and still try the other bucket.
func recordLoginFailure(m *models.UserModel, ipBucket, accountBucket string) {
	record(m, ipBucket, maxAttemptsPerIP)
	record(m, accountBucket, maxAttemptsPerAccount)
}

func record(m *models.UserModel, bucket string, maxAttempts int) {
	if err := m.RecordAttempt(bucket, maxAttempts); err != nil {
		log.Printf("handlers: rate limiter failed to record %q: %v", bucket, err)
	}
}

// clearBuckets forgets what a proven credential entitles the caller to forget:
// their own address, and the account they just authenticated as. Never a bucket
// belonging to an account they have not proven they hold.
func clearBuckets(m *models.UserModel, buckets ...string) {
	for _, bucket := range buckets {
		if err := m.ClearAttempts(bucket); err != nil {
			log.Printf("handlers: rate limiter failed to clear %q: %v", bucket, err)
		}
	}
}
