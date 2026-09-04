package handlers

import (
	"strings"
	"testing"
)

// The key is the isolation: ClearAttempts deletes by key, so two actions that
// spell a key the same way are one action to the limiter, and a success on
// either erases both.
func TestBucketKeysAreDistinct(t *testing.T) {
	const ip = "203.0.113.9"
	keys := map[string]string{
		"login":         loginBucket(ip),
		"login_token":   loginTokenBucket(ip),
		"register":      registerBucket(ip),
		"login_account": loginAccountBucket("ada@example.com"),
	}
	seen := map[string]string{}
	for name, key := range keys {
		if other, dup := seen[key]; dup {
			t.Errorf("%s and %s share the bucket key %q", name, other, key)
		}
		seen[key] = name
		if !strings.Contains(key, ":") {
			t.Errorf("%s key %q is not namespaced", name, key)
		}
	}
}

// The account bucket must not vary with case, or the whole control is bypassed
// by capitalising a letter.
func TestLoginAccountBucketIsCaseAndSpaceInsensitive(t *testing.T) {
	want := loginAccountBucket("ada@example.com")
	for _, variant := range []string{"Ada@Example.com", "  ada@example.com  ", "ADA@EXAMPLE.COM"} {
		if got := loginAccountBucket(variant); got != want {
			t.Errorf("loginAccountBucket(%q) = %q, want %q", variant, got, want)
		}
	}
	if loginAccountBucket("eve@example.com") == want {
		t.Error("different addresses share an account bucket")
	}
}

// Hashed because the table is written by unauthenticated callers: a raw key
// would turn it into a log of every address anybody probed.
func TestLoginAccountBucketDoesNotContainTheAddress(t *testing.T) {
	key := loginAccountBucket("ada@example.com")
	if strings.Contains(key, "ada") || strings.Contains(key, "@") {
		t.Errorf("account bucket key leaks the address: %q", key)
	}
}

// IPv6 makes addresses effectively free, so a per-address registration budget
// is no budget at all: an attacker rotating interface identifiers inside one
// routed /64 gets a fresh allowance per address.
func TestRegisterBucketCollapsesIPv6ToItsSlash64(t *testing.T) {
	same := []string{
		"2001:db8:abcd:1234::1",
		"2001:db8:abcd:1234::dead:beef",
		"2001:db8:abcd:1234:ffff:ffff:ffff:ffff",
	}
	want := registerBucket(same[0])
	for _, ip := range same[1:] {
		if got := registerBucket(ip); got != want {
			t.Errorf("registerBucket(%q) = %q, want %q — same /64 must share a bucket", ip, got, want)
		}
	}
	// A different /64 is a different customer and keeps its own budget.
	if registerBucket("2001:db8:abcd:5678::1") == want {
		t.Error("distinct /64s collapsed into one bucket")
	}
}

func TestRegisterBucketPassesIPv4AndGarbageThrough(t *testing.T) {
	if got := registerBucket("203.0.113.9"); got != "register:203.0.113.9" {
		t.Errorf("IPv4 should pass through unchanged, got %q", got)
	}
	// An unparseable value must not become an unpredictable key.
	if got := registerBucket("not-an-ip"); got != "register:not-an-ip" {
		t.Errorf("garbage should pass through unchanged, got %q", got)
	}
}
