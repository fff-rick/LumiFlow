package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Repository struct{ db DB }

func NewRepository(db DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(ctx context.Context, username, nickname, passwordHash string) (User, error) {
	res, err := r.db.ExecContext(ctx, `
INSERT INTO users(username, nickname, avatar, password_hash, status, created_at, updated_at)
VALUES (?, ?, ELT(FLOOR(1 + RAND() * 7), '/front_con/ava/布偶.svg', '/front_con/ava/暹罗猫.svg', '/front_con/ava/波斯猫.svg', '/front_con/ava/狸花猫.svg', '/front_con/ava/缅因猫.svg', '/front_con/ava/female.png', '/front_con/ava/male.png'), ?, 1, NOW(3), NOW(3))`, username, nickname, passwordHash)
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return r.ByID(ctx, id)
}

func (r *Repository) ByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := r.db.QueryRowContext(ctx, `
SELECT id, username, nickname, avatar, password_hash, status, created_at
FROM users WHERE username = ? LIMIT 1`, username).
		Scan(&u.ID, &u.Username, &u.Nickname, &u.Avatar, &u.PasswordHash, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	u.Account = u.Username
	return u, err
}

func (r *Repository) ByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := r.db.QueryRowContext(ctx, `
SELECT id, username, nickname, avatar, password_hash, status, created_at
FROM users WHERE id = ? LIMIT 1`, id).
		Scan(&u.ID, &u.Username, &u.Nickname, &u.Avatar, &u.PasswordHash, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	u.Account = u.Username
	return u, err
}

// SyncExternal maintains a local, non-login projection for a FluxFeed user.
// The mapping, rather than the external numeric ID, is the durable identity
// boundary between the two services.
func (r *Repository) SyncExternal(ctx context.Context, external ExternalUser) (User, error) {
	provider := strings.TrimSpace(external.Provider)
	subject := strings.TrimSpace(external.Subject)
	username := strings.TrimSpace(external.Username)
	nickname := limitRunes(strings.TrimSpace(external.Nickname), 64)
	avatar := strings.TrimSpace(external.Avatar)
	if provider == "" || subject == "" || username == "" || nickname == "" || len(username) > 32 || len(nickname) > 64 || len(avatar) > 512 {
		return User{}, ErrInvalidInput
	}

	var userID int64
	err := r.db.QueryRowContext(ctx, `SELECT user_id FROM external_identities WHERE provider=? AND subject=? LIMIT 1`, provider, subject).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = r.db.ExecContext(ctx, `
INSERT INTO users(username, nickname, avatar, password_hash, status, created_at, updated_at)
VALUES (?, ?, ?, 'external-identity', 1, NOW(3), NOW(3))
ON DUPLICATE KEY UPDATE nickname=VALUES(nickname), avatar=VALUES(avatar), status=1, updated_at=NOW(3)`, username, nickname, avatar); err != nil {
			return User{}, err
		}
		created, err := r.ByUsername(ctx, username)
		if err != nil {
			return User{}, err
		}
		if _, err = r.db.ExecContext(ctx, `
INSERT INTO external_identities(provider, subject, user_id, created_at, updated_at)
VALUES (?, ?, ?, NOW(3), NOW(3))
ON DUPLICATE KEY UPDATE user_id=user_id, updated_at=NOW(3)`, provider, subject, created.ID); err != nil {
			return User{}, err
		}
		if err = r.db.QueryRowContext(ctx, `SELECT user_id FROM external_identities WHERE provider=? AND subject=? LIMIT 1`, provider, subject).Scan(&userID); err != nil {
			return User{}, fmt.Errorf("read external identity: %w", err)
		}
	} else if err != nil {
		return User{}, err
	}
	if _, err = r.db.ExecContext(ctx, `UPDATE users SET nickname=?, avatar=?, status=1, updated_at=NOW(3) WHERE id=?`, nickname, avatar, userID); err != nil {
		return User{}, err
	}
	return r.ByID(ctx, userID)
}

func limitRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
