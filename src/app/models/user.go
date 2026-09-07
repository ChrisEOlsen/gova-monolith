package models

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"gova/app/db"
)

// MaxPasswordBytes is bcrypt's input limit. bcrypt rejects longer input rather
// than truncating it, so the boundary validates and answers 400.
const MaxPasswordBytes = 72

// BcryptCost is the work factor for every password hash in the app, including
// the dummy hash the unknown-email login path pays. 12 rather than bcrypt's
// DefaultCost of 10: 10 was chosen when the package was written and hardware
// has moved on. Exported so handlers/auth.go's timing-equalization hash uses
// the identical cost — a cheaper dummy would restore the timing oracle it
// exists to close.
const BcryptCost = 12

var (
	ErrDuplicateEmail  = errors.New("an account with that email already exists")
	ErrPasswordTooLong = errors.New("password must be at most 72 bytes")
	ErrUserNotFound    = errors.New("user not found")
)

type User struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	CreatedAt    Time   `json:"created_at"`
}

type UserModel struct {
	db *db.DB
}

func NewUserModel(database *db.DB) *UserModel {
	return &UserModel{db: database}
}

func (m *UserModel) Create(name, email, password string) (int64, error) {
	if len(password) > MaxPasswordBytes {
		return 0, ErrPasswordTooLong
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return 0, err
	}
	res, err := m.db.Write.Exec(
		"INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?)",
		name, normalizeEmail(email), string(hashed),
	)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			return 0, ErrDuplicateEmail
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (m *UserModel) FindByEmail(email string) (*User, error) {
	return m.scanUser(m.db.Read.QueryRow(
		"SELECT id, name, email, password_hash, created_at FROM users WHERE email = ?",
		normalizeEmail(email),
	))
}

func (m *UserModel) FindByID(id int64) (*User, error) {
	return m.scanUser(m.db.Read.QueryRow(
		"SELECT id, name, email, password_hash, created_at FROM users WHERE id = ?", id,
	))
}

func (m *UserModel) scanUser(row *sql.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// SessionEpoch returns the user's current epoch, or 0 for an unknown user.
// Satisfies middleware.EpochStore.
func (m *UserModel) SessionEpoch(userID int64) int64 {
	var epoch int64
	if err := m.db.Read.QueryRow(
		"SELECT session_epoch FROM users WHERE id = ?", userID,
	).Scan(&epoch); err != nil {
		return 0
	}
	return epoch
}

// RevokeAllSessions retires every credential a user holds, on every device:
// the epoch bump kills all outstanding session cookies, and the delete kills
// all outstanding bearer tokens.
//
// One transaction, one method, because "log out everywhere" is one promise. The
// epoch bump alone used to be the whole implementation, which left native
// clients signed in — an operator containing a compromised account had no way
// to retire the token on the attacker's device. Anything that revokes sessions
// calls this; nothing bumps the epoch on its own.
func (m *UserModel) RevokeAllSessions(userID int64) error {
	tx, err := m.db.Write.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"UPDATE users SET session_epoch = session_epoch + 1 WHERE id = ?", userID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM mobile_tokens WHERE user_id = ?", userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ─── rate limiting ───────────────────────────────────────────────────────────

// RateLimitWindow is both the counting window and the lockout duration.
const RateLimitWindow = 15 * time.Minute

// IsRateLimited reports whether a bucket is currently locked out.
func (m *UserModel) IsRateLimited(bucket string) (bool, error) {
	return m.rateLimitedAt(bucket, time.Now())
}

func (m *UserModel) rateLimitedAt(bucket string, now time.Time) (bool, error) {
	var lockedUntil sql.NullInt64
	err := m.db.Read.QueryRow(
		"SELECT locked_until FROM rate_limits WHERE bucket = ?", bucket,
	).Scan(&lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return lockedUntil.Valid && now.Unix() < lockedUntil.Int64, nil
}

// RecordAttempt counts one attempt against a bucket and locks it at maxAttempts.
// A bucket whose last attempt predates the window starts a fresh count, so the
// limit is a rate rather than a lifetime quota.
func (m *UserModel) RecordAttempt(bucket string, maxAttempts int) error {
	return m.recordAttemptAt(bucket, maxAttempts, time.Now())
}

func (m *UserModel) recordAttemptAt(bucket string, maxAttempts int, now time.Time) error {
	_, err := m.db.Write.Exec(`
		INSERT INTO rate_limits (bucket, attempts, locked_until, updated_at)
		VALUES (?, 1, NULL, ?)
		ON CONFLICT(bucket) DO UPDATE SET
			attempts = CASE WHEN updated_at < ? THEN 1 ELSE attempts + 1 END,
			locked_until = CASE
				WHEN updated_at < ? THEN NULL
				WHEN attempts + 1 >= ? THEN ?
				ELSE locked_until END,
			updated_at = ?
		`,
		bucket, now.Unix(),
		now.Add(-RateLimitWindow).Unix(),
		now.Add(-RateLimitWindow).Unix(),
		maxAttempts, now.Add(RateLimitWindow).Unix(),
		now.Unix(),
	)
	return err
}

// ClearAttempts forgets a bucket after the credential it protects was proven.
func (m *UserModel) ClearAttempts(bucket string) error {
	_, err := m.db.Write.Exec("DELETE FROM rate_limits WHERE bucket = ?", bucket)
	return err
}
