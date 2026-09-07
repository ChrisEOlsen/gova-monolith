package models

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gova/app/db"
)

func newUserModel(t *testing.T) *UserModel {
	t.Helper()
	return NewUserModel(db.OpenTest(t, ""))
}

func TestUserCreateAndFind(t *testing.T) {
	m := newUserModel(t)
	id, err := m.Create("Ada", "Ada@Example.com", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	byEmail, err := m.FindByEmail("ada@example.com")
	if err != nil {
		t.Fatalf("find by email: %v", err)
	}
	if byEmail.ID != id || byEmail.Name != "Ada" {
		t.Errorf("got %+v, want id %d name Ada", byEmail, id)
	}
	// Stored lowercased, so a differently-cased login still finds the account.
	if byEmail.Email != "ada@example.com" {
		t.Errorf("email not normalized: %q", byEmail.Email)
	}
	if byEmail.PasswordHash == "correct-horse-battery-staple" {
		t.Fatal("password stored in plaintext")
	}

	byID, err := m.FindByID(id)
	if err != nil || byID.Email != byEmail.Email {
		t.Errorf("find by id: %+v, %v", byID, err)
	}
}

func TestUserFindUnknownReturnsSentinel(t *testing.T) {
	m := newUserModel(t)
	if _, err := m.FindByEmail("nobody@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("want ErrUserNotFound, got %v", err)
	}
	if _, err := m.FindByID(999); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("want ErrUserNotFound, got %v", err)
	}
}

func TestUserCreateDuplicateEmail(t *testing.T) {
	m := newUserModel(t)
	if _, err := m.Create("Ada", "ada@example.com", "password123"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Different case, same account — the UNIQUE index must still catch it.
	_, err := m.Create("Ada Two", "ADA@example.com", "password123")
	if !errors.Is(err, ErrDuplicateEmail) {
		t.Errorf("want ErrDuplicateEmail, got %v", err)
	}
}

// bcrypt rejects input over 72 bytes rather than truncating it, so the boundary
// must answer before the hash call.
func TestUserCreateRejectsOverlongPassword(t *testing.T) {
	m := newUserModel(t)
	_, err := m.Create("Ada", "ada@example.com", strings.Repeat("x", MaxPasswordBytes+1))
	if !errors.Is(err, ErrPasswordTooLong) {
		t.Errorf("want ErrPasswordTooLong, got %v", err)
	}
	if _, err := m.Create("Ada", "ada@example.com", strings.Repeat("x", MaxPasswordBytes)); err != nil {
		t.Errorf("exactly 72 bytes must be accepted, got %v", err)
	}
}

func TestSessionEpochBump(t *testing.T) {
	m := newUserModel(t)
	id, _ := m.Create("Ada", "ada@example.com", "password123")

	if got := m.SessionEpoch(id); got != 0 {
		t.Errorf("fresh user epoch = %d, want 0", got)
	}
	if err := m.RevokeAllSessions(id); err != nil {
		t.Fatalf("bump: %v", err)
	}
	if got := m.SessionEpoch(id); got != 1 {
		t.Errorf("after bump epoch = %d, want 1", got)
	}
	// An unknown user reads as 0 rather than erroring — the middleware treats
	// that as "no revocation in force".
	if got := m.SessionEpoch(999); got != 0 {
		t.Errorf("unknown user epoch = %d, want 0", got)
	}
}

func TestRateLimitLocksAtBudget(t *testing.T) {
	m := newUserModel(t)
	const bucket = "login:203.0.113.9"

	for i := range 4 {
		if err := m.RecordAttempt(bucket, 5); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if locked, _ := m.IsRateLimited(bucket); locked {
			t.Fatalf("locked after %d attempts, budget is 5", i+1)
		}
	}
	m.RecordAttempt(bucket, 5)
	if locked, _ := m.IsRateLimited(bucket); !locked {
		t.Fatal("the fifth attempt must lock the bucket")
	}
}

func TestRateLimitUnknownBucketIsNotLimited(t *testing.T) {
	m := newUserModel(t)
	if locked, err := m.IsRateLimited("login:never-seen"); locked || err != nil {
		t.Errorf("unknown bucket: locked=%v err=%v", locked, err)
	}
}

// The window makes the limit a rate, not a lifetime quota. Without decay, five
// failures ever leave the counter at 5 and the bucket re-locks on its next
// attempt forever — which behind a shared NAT is every user on the address.
func TestRateLimitDecaysAfterWindow(t *testing.T) {
	m := newUserModel(t)
	const bucket = "login:203.0.113.9"
	start := time.Now()

	for range 5 {
		m.recordAttemptAt(bucket, 5, start)
	}
	if locked, _ := m.rateLimitedAt(bucket, start); !locked {
		t.Fatal("5 attempts inside the window should lock")
	}

	later := start.Add(RateLimitWindow + time.Minute)
	if locked, _ := m.rateLimitedAt(bucket, later); locked {
		t.Fatal("the lock should have lapsed once the window passed")
	}

	// The first failure in a fresh window restarts the count at 1. A counter
	// that resumed at 6 would re-lock right here.
	m.recordAttemptAt(bucket, 5, later)
	if locked, _ := m.rateLimitedAt(bucket, later); locked {
		t.Fatal("bucket re-locked on the first failure after the window — attempts never decay")
	}
	// Three more (four total) must still pass; only the fifth locks again.
	for range 3 {
		m.recordAttemptAt(bucket, 5, later)
	}
	if locked, _ := m.rateLimitedAt(bucket, later); locked {
		t.Fatal("4 attempts in a fresh window must not lock")
	}
	m.recordAttemptAt(bucket, 5, later)
	if locked, _ := m.rateLimitedAt(bucket, later); !locked {
		t.Fatal("the fifth attempt in a fresh window must lock again")
	}
}

func TestClearAttemptsForgetsOnlyItsBucket(t *testing.T) {
	m := newUserModel(t)
	for range 5 {
		m.RecordAttempt("login:a", 5)
		m.RecordAttempt("login:b", 5)
	}
	if err := m.ClearAttempts("login:a"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if locked, _ := m.IsRateLimited("login:a"); locked {
		t.Error("cleared bucket is still locked")
	}
	if locked, _ := m.IsRateLimited("login:b"); !locked {
		t.Error("clearing one bucket released another")
	}
}

// "Log out everywhere" is one promise covering both credential kinds. The epoch
// bump retires session cookies; the delete retires bearer tokens. A native
// client left signed in after this call is the containment failure the method
// exists to prevent.
func TestRevokeAllSessions_RetiresBearerTokensToo(t *testing.T) {
	m := newUserModel(t)
	id, err := m.Create("Ada", "ada@example.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	other, err := m.Create("Bob", "bob@example.com", "password123")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	tokens := NewMobileTokenModel(m.db)
	if err := tokens.Issue(HashToken("ada-token"), id, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := tokens.Issue(HashToken("bob-token"), other, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("issue other: %v", err)
	}

	if err := m.RevokeAllSessions(id); err != nil {
		t.Fatalf("RevokeAllSessions: %v", err)
	}

	if got := m.SessionEpoch(id); got != 1 {
		t.Errorf("epoch = %d, want 1 — outstanding cookies were not retired", got)
	}
	if _, ok := tokens.UserIDForToken("ada-token"); ok {
		t.Error("the revoked user's bearer token still authenticates")
	}
	// Another account's tokens are untouched: revocation is per user.
	if _, ok := tokens.UserIDForToken("bob-token"); !ok {
		t.Error("another user's bearer token was revoked too")
	}
}
