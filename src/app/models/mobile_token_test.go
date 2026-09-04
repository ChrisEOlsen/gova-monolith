package models

import (
	"errors"
	"testing"
	"time"

	"gova/app/db"
)

func TestMobileTokenIssueAndLookup(t *testing.T) {
	database := db.OpenTest(t, "")
	users := NewUserModel(database)
	tokens := NewMobileTokenModel(database)

	uid, err := users.Create("Ada", "ada@example.com", "password123")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tokens.Issue("hash-a", uid, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("issue: %v", err)
	}

	got, err := tokens.UserID("hash-a")
	if err != nil || got != uid {
		t.Errorf("lookup: got (%d, %v), want (%d, nil)", got, err, uid)
	}
}

func TestMobileTokenUnknownAndExpiredAreIndistinguishable(t *testing.T) {
	database := db.OpenTest(t, "")
	users := NewUserModel(database)
	tokens := NewMobileTokenModel(database)
	uid, _ := users.Create("Ada", "ada@example.com", "password123")

	issued := time.Now()
	if err := tokens.Issue("hash-expired", uid, issued.Add(time.Hour)); err != nil {
		t.Fatalf("issue: %v", err)
	}

	_, unknownErr := tokens.UserID("never-issued")
	_, expiredErr := tokens.userIDAt("hash-expired", issued.Add(2*time.Hour))

	if !errors.Is(unknownErr, ErrTokenInvalid) {
		t.Errorf("unknown token: want ErrTokenInvalid, got %v", unknownErr)
	}
	// Telling the two apart would confirm that a stolen token was once real.
	if !errors.Is(expiredErr, ErrTokenInvalid) {
		t.Errorf("expired token: want ErrTokenInvalid, got %v", expiredErr)
	}
}

func TestMobileTokenStillValidJustBeforeExpiry(t *testing.T) {
	database := db.OpenTest(t, "")
	users := NewUserModel(database)
	tokens := NewMobileTokenModel(database)
	uid, _ := users.Create("Ada", "ada@example.com", "password123")

	expiry := time.Now().Add(time.Hour)
	tokens.Issue("hash-a", uid, expiry)

	if _, err := tokens.userIDAt("hash-a", expiry.Add(-time.Second)); err != nil {
		t.Errorf("token one second before expiry must be valid, got %v", err)
	}
	if _, err := tokens.userIDAt("hash-a", expiry.Add(time.Second)); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("token after expiry must be invalid, got %v", err)
	}
}

func TestMobileTokenRevoke(t *testing.T) {
	database := db.OpenTest(t, "")
	users := NewUserModel(database)
	tokens := NewMobileTokenModel(database)
	uid, _ := users.Create("Ada", "ada@example.com", "password123")
	tokens.Issue("hash-a", uid, time.Now().Add(time.Hour))

	if err := tokens.Revoke("hash-a"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := tokens.UserID("hash-a"); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("revoked token still valid: %v", err)
	}
	// Revoking a token that was never issued is not an error — logout must not
	// reveal whether it existed.
	if err := tokens.Revoke("never-issued"); err != nil {
		t.Errorf("revoking an unknown token: %v", err)
	}
}

func TestMobileTokenDeleteExpired(t *testing.T) {
	database := db.OpenTest(t, "")
	users := NewUserModel(database)
	tokens := NewMobileTokenModel(database)
	uid, _ := users.Create("Ada", "ada@example.com", "password123")

	now := time.Now()
	tokens.Issue("live", uid, now.Add(time.Hour))
	tokens.Issue("dead", uid, now.Add(-time.Hour))

	n, err := tokens.DeleteExpired(now)
	if err != nil || n != 1 {
		t.Fatalf("delete expired: got (%d, %v), want (1, nil)", n, err)
	}
	if _, err := tokens.UserID("live"); err != nil {
		t.Errorf("live token was pruned: %v", err)
	}
}
