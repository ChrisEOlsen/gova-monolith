package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gova/app/db"
	"gova/app/models"
)

func postRegister(database *db.DB, name, email, password string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]string{"name": name, "email": email, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBuffer(b))
	rec := httptest.NewRecorder()
	RegisterPOST(database)(rec, req)
	return rec
}

func registerFixture(t *testing.T) *db.DB {
	t.Helper()
	t.Setenv("SESSION_SECRET", strings.Repeat("k", 64))
	return db.OpenTest(t, "")
}

func TestRegisterPOST_Success(t *testing.T) {
	database := registerFixture(t)
	rec := postRegister(database, "Ada", "ada@example.com", "password123")

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
	if sessionCookie(rec) == nil {
		t.Error("registration did not sign the new user in")
	}
	if _, err := models.NewUserModel(database).FindByEmail("ada@example.com"); err != nil {
		t.Errorf("user was not created: %v", err)
	}
}

func TestRegisterPOST_DuplicateEmailIsConflict(t *testing.T) {
	database := registerFixture(t)
	postRegister(database, "Ada", "ada@example.com", "password123")

	rec := postRegister(database, "Ada Two", "ada@example.com", "password123")
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec.Code)
	}
}

func TestRegisterPOST_Validation(t *testing.T) {
	cases := []struct {
		name, userName, email, password, wantField string
	}{
		{"empty name", "", "ada@example.com", "password123", "name"},
		{"no at sign", "Ada", "not-an-email", "password123", "email"},
		{"short password", "Ada", "ada@example.com", "short", "password"},
		{"overlong password", "Ada", "ada@example.com", strings.Repeat("x", 73), "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			database := registerFixture(t)
			rec := postRegister(database, tc.userName, tc.email, tc.password)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("want 422, got %d body %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Code   string            `json:"code"`
				Fields map[string]string `json:"fields"`
			}
			json.Unmarshal(rec.Body.Bytes(), &body)
			if body.Code != CodeValidationFailed {
				t.Errorf("code = %q, want %q", body.Code, CodeValidationFailed)
			}
			if _, ok := body.Fields[tc.wantField]; !ok {
				t.Errorf("fields = %v, want a %q entry", body.Fields, tc.wantField)
			}
		})
	}
}

func TestRegisterPOST_MalformedBody(t *testing.T) {
	database := registerFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader("{"))
	rec := httptest.NewRecorder()
	RegisterPOST(database)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// Every attempt counts, success included — account creation itself is the thing
// being limited, and the 409 above is an existence oracle if asking is free.
func TestRegisterPOST_RateLimitedIncludingSuccesses(t *testing.T) {
	database := registerFixture(t)
	for i := range maxAttemptsPerIP {
		rec := postRegister(database, "User", "user"+string(rune('a'+i))+"@example.com", "password123")
		if rec.Code != http.StatusOK {
			t.Fatalf("signup %d: want 200, got %d", i+1, rec.Code)
		}
	}
	rec := postRegister(database, "One More", "onemore@example.com", "password123")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 after %d signups, got %d", maxAttemptsPerIP, rec.Code)
	}
}
