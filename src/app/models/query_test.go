package models

import (
	"errors"
	"testing"
)

func TestOrderByClause_Default(t *testing.T) {
	got, err := orderByClause("", []string{"id", "name", "created_at"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "ORDER BY created_at DESC" {
		t.Errorf("default: got %q", got)
	}
}

func TestOrderByClause_AscAndDesc(t *testing.T) {
	allowed := []string{"id", "name", "created_at"}
	asc, err := orderByClause("name", allowed)
	if err != nil || asc != "ORDER BY name ASC" {
		t.Errorf("asc: got %q err %v", asc, err)
	}
	desc, err := orderByClause("-created_at", allowed)
	if err != nil || desc != "ORDER BY created_at DESC" {
		t.Errorf("desc: got %q err %v", desc, err)
	}
}

func TestOrderByClause_UnknownColumnRejected(t *testing.T) {
	_, err := orderByClause("bogus", []string{"id", "name"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("unknown column: want ErrInvalidQuery, got %v", err)
	}
}

func TestOrderByClause_InjectionRejected(t *testing.T) {
	// A would-be injection string is not exactly a whitelisted column, so it
	// is rejected — the whitelist is the security boundary.
	_, err := orderByClause("name; DROP TABLE users", []string{"id", "name"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("injection: want ErrInvalidQuery, got %v", err)
	}
	_, err = orderByClause("-name; DROP TABLE users", []string{"id", "name"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("injection desc: want ErrInvalidQuery, got %v", err)
	}
}

func TestFilterField_Valid(t *testing.T) {
	got, err := filterField("status", []string{"id", "status", "created_at"})
	if err != nil || got != "status" {
		t.Errorf("valid filter: got %q err %v", got, err)
	}
}

func TestFilterField_UnknownRejected(t *testing.T) {
	_, err := filterField("bogus", []string{"id", "status"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("unknown filter: want ErrInvalidQuery, got %v", err)
	}
	_, err = filterField("status = 1 OR 1=1", []string{"id", "status"})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("injection filter: want ErrInvalidQuery, got %v", err)
	}
}
