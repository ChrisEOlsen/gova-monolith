package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gova/app/db"
	"gova/app/middleware"
	"gova/app/models"
)

const (
	testEmail    = "ada@example.com"
	testPassword = "correct-horse-battery-staple"
)

func authFixture(t *testing.T) (*db.DB, *models.UserModel, int64) {
	t.Helper()
	t.Setenv("SESSION_SECRET", strings.Repeat("k", 64))
	database := db.OpenTest(t, "")
	users := models.NewUserModel(database)
	id, err := users.Create("Ada", testEmail, testPassword)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return database, users, id
}

func credentialsBody(email, password string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{"email": email, "password": password})
	return bytes.NewBuffer(b)
}

func postLogin(h http.HandlerFunc, email, password string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", credentialsBody(email, password))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.SessionCookieName {
			return c
		}
	}
	return nil
}

func TestLoginPOST_Success(t *testing.T) {
	database, _, _ := authFixture(t)
	rec := postLogin(LoginPOST(database), testEmail, testPassword)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
	if sessionCookie(rec) == nil {
		t.Error("no session cookie set on success")
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("login response leaks a password field: %s", rec.Body.String())
	}
}

func TestLoginPOST_WrongPasswordAndUnknownEmailAreIdentical(t *testing.T) {
	database, _, _ := authFixture(t)
	h := LoginPOST(database)

	wrong := postLogin(h, testEmail, "not-the-password")
	unknown := postLogin(h, "nobody@example.com", testPassword)

	if wrong.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("want 401/401, got %d/%d", wrong.Code, unknown.Code)
	}
	// Identical bodies, so the response cannot be used to enumerate accounts.
	if wrong.Body.String() != unknown.Body.String() {
		t.Errorf("wrong password and unknown email differ:\n %s\n %s", wrong.Body, unknown.Body)
	}
	if sessionCookie(wrong) != nil {
		t.Error("a failed login set a session cookie")
	}
}

func TestLoginPOST_RateLimitedAfterBudget(t *testing.T) {
	database, _, _ := authFixture(t)
	h := LoginPOST(database)

	for i := range maxAttemptsPerIP {
		if code := postLogin(h, testEmail, "wrong").Code; code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: want 401, got %d", i+1, code)
		}
	}
	// The budget is spent — even the correct password is refused now.
	rec := postLogin(h, testEmail, testPassword)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 after %d failures, got %d", maxAttemptsPerIP, rec.Code)
	}
}

func TestLoginPOST_SuccessClearsItsOwnBucket(t *testing.T) {
	database, users, _ := authFixture(t)
	h := LoginPOST(database)

	postLogin(h, testEmail, "wrong")
	postLogin(h, testEmail, "wrong")
	if rec := postLogin(h, testEmail, testPassword); rec.Code != http.StatusOK {
		t.Fatalf("login after 2 failures should succeed, got %d", rec.Code)
	}
	// A user who mistyped twice and then got it right is not left throttled.
	locked, _ := users.IsRateLimited(loginBucket("192.0.2.1"))
	if locked {
		t.Error("successful login did not clear its own address bucket")
	}
}

// The bypass this closes: an attacker clearing the address bucket by logging
// into an account they hold, between guesses at a victim's.
func TestLoginPOST_SuccessDoesNotClearAnotherAccountsBucket(t *testing.T) {
	database, users, _ := authFixture(t)
	if _, err := users.Create("Eve", "eve@example.com", "eve-password-1"); err != nil {
		t.Fatalf("seed attacker: %v", err)
	}
	h := LoginPOST(database)

	postLogin(h, testEmail, "wrong")
	postLogin(h, testEmail, "wrong")
	if rec := postLogin(h, "eve@example.com", "eve-password-1"); rec.Code != http.StatusOK {
		t.Fatalf("attacker login should succeed, got %d", rec.Code)
	}

	if locked, _ := users.IsRateLimited(loginAccountBucket(testEmail)); locked {
		t.Fatal("victim account locked too early — fixture assumption wrong")
	}
	// The victim's account bucket must still hold both failures.
	for range maxAttemptsPerAccount - 2 {
		users.RecordAttempt(loginAccountBucket(testEmail), maxAttemptsPerAccount)
	}
	if locked, _ := users.IsRateLimited(loginAccountBucket(testEmail)); !locked {
		t.Error("a success on another account reset the victim's account bucket")
	}
}

func TestLoginPOST_MalformedBody(t *testing.T) {
	database, _, _ := authFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{"))
	rec := httptest.NewRecorder()
	LoginPOST(database)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestLoginPOST_MissingFields(t *testing.T) {
	database, _, _ := authFixture(t)
	if rec := postLogin(LoginPOST(database), "", testPassword); rec.Code != http.StatusBadRequest {
		t.Errorf("empty email: want 400, got %d", rec.Code)
	}
	if rec := postLogin(LoginPOST(database), testEmail, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("empty password: want 400, got %d", rec.Code)
	}
}

func TestLoginPOST_SessionCarriesCurrentEpoch(t *testing.T) {
	database, users, id := authFixture(t)
	if err := users.RevokeAllSessions(id); err != nil {
		t.Fatalf("bump: %v", err)
	}

	rec := postLogin(LoginPOST(database), testEmail, testPassword)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d", rec.Code)
	}
	// The cookie must carry the post-bump epoch, or Auth rejects the session
	// that was just issued.
	authed := authProbe(t, database, sessionCookie(rec))
	if authed != http.StatusOK {
		t.Errorf("freshly issued session was rejected by Auth (got %d)", authed)
	}
}

// authProbe runs a cookie through the real Auth middleware.
func authProbe(t *testing.T, database *db.DB, c *http.Cookie) int {
	t.Helper()
	if c == nil {
		t.Fatal("no session cookie to probe")
	}
	h := middleware.Auth(models.NewUserModel(database), models.NewMobileTokenModel(database))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if middleware.UserID(r) == 0 {
				w.WriteHeader(http.StatusTeapot)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// The revocation property end to end: a session issued before "log out
// everywhere" stops working on every device, not just the one that asked.
func TestLogoutAllPOST_RetiresEarlierSessions(t *testing.T) {
	database, _, id := authFixture(t)

	old := sessionCookie(postLogin(LoginPOST(database), testEmail, testPassword))
	if authProbe(t, database, old) != http.StatusOK {
		t.Fatal("fresh session should authenticate")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout_all", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, id))
	rec := httptest.NewRecorder()
	LogoutAllPOST(database)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout_all: %d", rec.Code)
	}

	if code := authProbe(t, database, old); code != http.StatusTeapot {
		t.Errorf("session issued before logout_all still authenticates (got %d)", code)
	}
}

func TestLogoutPOST_ClearsCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	LogoutPOST()(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	c := sessionCookie(rec)
	if c == nil || c.MaxAge >= 0 {
		t.Errorf("logout did not send a deletion cookie: %+v", c)
	}
}

func TestMeGET_ReturnsPublicUserOnly(t *testing.T) {
	database, _, id := authFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, id))
	rec := httptest.NewRecorder()
	MeGET(database)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if _, leaked := body.Data["password_hash"]; leaked {
		t.Error("me response contains password_hash")
	}
	if body.Data["email"] != testEmail {
		t.Errorf("email = %v, want %s", body.Data["email"], testEmail)
	}
}
