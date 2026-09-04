package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gova/app/db"
	"gova/app/models"
)

func postLoginToken(database *db.DB, email, password string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login_token", credentialsBody(email, password))
	rec := httptest.NewRecorder()
	MobileLoginPOST(database)(rec, req)
	return rec
}

func tokenFrom(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return body.Data.Token
}

func withBearer(method, path, token string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestMobileLoginPOST_IssuesToken(t *testing.T) {
	database, _, _ := authFixture(t)
	rec := postLoginToken(database, testEmail, testPassword)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
	token := tokenFrom(t, rec)
	if len(token) != 64 {
		t.Errorf("token length %d, want 64 hex chars", len(token))
	}
	// Only the hash is persisted — the raw token must not be in the table.
	var count int
	database.Read.QueryRow("SELECT COUNT(*) FROM mobile_tokens WHERE token_hash = ?", token).Scan(&count)
	if count != 0 {
		t.Error("the raw token was stored instead of its hash")
	}
	database.Read.QueryRow("SELECT COUNT(*) FROM mobile_tokens WHERE token_hash = ?", hashToken(token)).Scan(&count)
	if count != 1 {
		t.Error("no hashed token row was written")
	}
}

func TestMobileLoginPOST_WrongPasswordIssuesNothing(t *testing.T) {
	database, _, _ := authFixture(t)
	rec := postLoginToken(database, testEmail, "wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	var count int
	database.Read.QueryRow("SELECT COUNT(*) FROM mobile_tokens").Scan(&count)
	if count != 0 {
		t.Errorf("a failed login issued %d token(s)", count)
	}
}

func TestMobileMeGET_ValidatesBearerToken(t *testing.T) {
	database, _, _ := authFixture(t)
	token := tokenFrom(t, postLoginToken(database, testEmail, testPassword))

	rec := httptest.NewRecorder()
	MobileMeGET(database)(rec, withBearer(http.MethodGet, "/api/v1/auth/me_token", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Data["email"] != testEmail {
		t.Errorf("email = %v, want %s", body.Data["email"], testEmail)
	}
	if _, leaked := body.Data["password_hash"]; leaked {
		t.Error("me_token response contains password_hash")
	}
}

func TestMobileMeGET_RejectsBadTokens(t *testing.T) {
	database, _, _ := authFixture(t)
	h := MobileMeGET(database)

	cases := map[string]*http.Request{
		"no header":     httptest.NewRequest(http.MethodGet, "/api/v1/auth/me_token", nil),
		"empty bearer":  withBearer(http.MethodGet, "/api/v1/auth/me_token", ""),
		"unknown token": withBearer(http.MethodGet, "/api/v1/auth/me_token", "deadbeef"),
	}
	for name, req := range cases {
		rec := httptest.NewRecorder()
		h(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", name, rec.Code)
		}
	}

	// A non-Bearer scheme carrying a real token is still rejected.
	token := tokenFrom(t, postLoginToken(database, testEmail, testPassword))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me_token", nil)
	req.Header.Set("Authorization", "Basic "+token)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("non-Bearer scheme: want 401, got %d", rec.Code)
	}
}

func TestMobileMeGET_RejectsExpiredToken(t *testing.T) {
	database, _, id := authFixture(t)
	tokens := models.NewMobileTokenModel(database)
	if err := tokens.Issue(hashToken("stale-token"), id, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("issue: %v", err)
	}

	rec := httptest.NewRecorder()
	MobileMeGET(database)(rec, withBearer(http.MethodGet, "/api/v1/auth/me_token", "stale-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: want 401, got %d", rec.Code)
	}
}

func TestMobileLogoutDELETE_RevokesToken(t *testing.T) {
	database, _, _ := authFixture(t)
	token := tokenFrom(t, postLoginToken(database, testEmail, testPassword))

	rec := httptest.NewRecorder()
	MobileLogoutDELETE(database)(rec, withBearer(http.MethodDelete, "/api/v1/auth/logout_token", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: want 200, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	MobileMeGET(database)(rec, withBearer(http.MethodGet, "/api/v1/auth/me_token", token))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked token still works (got %d)", rec.Code)
	}

	// Revoking an unknown token is a 200 too — it must not reveal existence.
	rec = httptest.NewRecorder()
	MobileLogoutDELETE(database)(rec, withBearer(http.MethodDelete, "/api/v1/auth/logout_token", "never-issued"))
	if rec.Code != http.StatusOK {
		t.Errorf("revoking an unknown token: want 200, got %d", rec.Code)
	}
}

func TestMobileLoginPOST_RateLimited(t *testing.T) {
	database, _, _ := authFixture(t)
	for range maxAttemptsPerIP {
		postLoginToken(database, testEmail, "wrong")
	}
	if rec := postLoginToken(database, testEmail, testPassword); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", rec.Code)
	}
}

// The two login endpoints keep separate address buckets: a success on one must
// not erase the other's failures, and a lockout on one must not lock the other.
func TestLoginEndpointsHaveIndependentAddressBuckets(t *testing.T) {
	database, _, _ := authFixture(t)

	for range maxAttemptsPerIP {
		postLogin(LoginPOST(database), testEmail, "wrong")
	}
	if rec := postLogin(LoginPOST(database), testEmail, testPassword); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("cookie login should be locked, got %d", rec.Code)
	}
	// The bearer endpoint's own address bucket is untouched. Its shared account
	// bucket has 5 of 20 spent, so a correct password still gets through.
	if rec := postLoginToken(database, testEmail, testPassword); rec.Code != http.StatusOK {
		t.Errorf("cookie-login lockout also locked the bearer endpoint (got %d)", rec.Code)
	}
}
