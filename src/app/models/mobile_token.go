package models

import (
	"database/sql"
	"errors"
	"time"

	"gova/app/db"
)

// ErrTokenInvalid covers an unknown token and an expired one alike — telling
// them apart would confirm that a stolen token was once real.
var ErrTokenInvalid = errors.New("mobile token invalid or expired")

type MobileTokenModel struct {
	db *db.DB
}

func NewMobileTokenModel(database *db.DB) *MobileTokenModel {
	return &MobileTokenModel{db: database}
}

// Issue stores a bearer token's SHA-256 hash. The raw token is never persisted.
func (m *MobileTokenModel) Issue(tokenHash string, userID int64, expiresAt time.Time) error {
	_, err := m.db.Write.Exec(
		"INSERT INTO mobile_tokens (token_hash, user_id, expires_at) VALUES (?, ?, ?)",
		tokenHash, userID, expiresAt.Unix(),
	)
	return err
}

// Revoke deletes a token hash. An unknown hash is not an error: logout must not
// reveal whether the token existed.
func (m *MobileTokenModel) Revoke(tokenHash string) error {
	_, err := m.db.Write.Exec("DELETE FROM mobile_tokens WHERE token_hash = ?", tokenHash)
	return err
}

// UserID returns the user a live token belongs to.
func (m *MobileTokenModel) UserID(tokenHash string) (int64, error) {
	return m.userIDAt(tokenHash, time.Now())
}

func (m *MobileTokenModel) userIDAt(tokenHash string, now time.Time) (int64, error) {
	var userID int64
	err := m.db.Read.QueryRow(
		"SELECT user_id FROM mobile_tokens WHERE token_hash = ? AND expires_at > ?",
		tokenHash, now.Unix(),
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrTokenInvalid
	}
	return userID, err
}

// DeleteExpired prunes the table. Nothing calls it on the request path; wire it
// to a periodic job if the table needs pruning.
func (m *MobileTokenModel) DeleteExpired(now time.Time) (int64, error) {
	res, err := m.db.Write.Exec(
		"DELETE FROM mobile_tokens WHERE expires_at <= ?", now.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
