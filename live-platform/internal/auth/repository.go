package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/example/live-platform/internal/projectionevent"
)

type DB interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Repository struct {
	db            DB
	identityTopic string
}

func NewRepository(db DB, identityTopic ...string) *Repository {
	r := &Repository{db: db}
	if len(identityTopic) > 0 {
		r.identityTopic = strings.TrimSpace(identityTopic[0])
	}
	return r
}

func (r *Repository) Create(ctx context.Context, username, nickname, passwordHash string) (User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
INSERT INTO users(username, nickname, avatar, password_hash, status, created_at, updated_at)
VALUES (?, ?, ELT(FLOOR(1 + RAND() * 7), '/front_con/ava/布偶.svg', '/front_con/ava/暹罗猫.svg', '/front_con/ava/波斯猫.svg', '/front_con/ava/狸花猫.svg', '/front_con/ava/缅因猫.svg', '/front_con/ava/female.png', '/front_con/ava/male.png'), ?, 1, NOW(3), NOW(3))`, username, nickname, passwordHash)
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	u, err := userByID(ctx, tx, id)
	if err != nil {
		return User{}, err
	}
	if err := r.enqueueIdentity(ctx, tx, u); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func (r *Repository) ByUsername(ctx context.Context, username string) (User, error) {
	return userByUsername(ctx, r.db, username)
}

type userQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func userByUsername(ctx context.Context, db userQueryer, username string) (User, error) {
	var u User
	err := db.QueryRowContext(ctx, `
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
	return userByID(ctx, r.db, id)
}

func userByID(ctx context.Context, db userQueryer, id int64) (User, error) {
	var u User
	err := db.QueryRowContext(ctx, `
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
	status := 1
	if external.Enabled != nil && !*external.Enabled {
		status = 2
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID int64
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM external_identities WHERE provider=? AND subject=? LIMIT 1`, provider, subject).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO users(username, nickname, avatar, password_hash, status, created_at, updated_at)
VALUES (?, ?, ?, 'external-identity', ?, NOW(3), NOW(3))
ON DUPLICATE KEY UPDATE nickname=VALUES(nickname), avatar=VALUES(avatar), status=VALUES(status), updated_at=NOW(3)`, username, nickname, avatar, status); err != nil {
			return User{}, err
		}
		created, err := userByUsername(ctx, tx, username)
		if err != nil {
			return User{}, err
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO external_identities(provider, subject, user_id, created_at, updated_at)
VALUES (?, ?, ?, NOW(3), NOW(3))
ON DUPLICATE KEY UPDATE user_id=user_id, updated_at=NOW(3)`, provider, subject, created.ID); err != nil {
			return User{}, err
		}
		if err = tx.QueryRowContext(ctx, `SELECT user_id FROM external_identities WHERE provider=? AND subject=? LIMIT 1`, provider, subject).Scan(&userID); err != nil {
			return User{}, fmt.Errorf("read external identity: %w", err)
		}
	} else if err != nil {
		return User{}, err
	}
	query := `UPDATE users SET nickname=?, avatar=?, updated_at=NOW(3) WHERE id=?`
	args := []any{nickname, avatar, userID}
	if external.Enabled != nil {
		query = `UPDATE users SET nickname=?, avatar=?, status=?, updated_at=NOW(3) WHERE id=?`
		args = []any{nickname, avatar, status, userID}
	}
	if _, err = tx.ExecContext(ctx, query, args...); err != nil {
		return User{}, err
	}
	u, err := userByID(ctx, tx, userID)
	if err != nil {
		return User{}, err
	}
	if err := r.enqueueIdentity(ctx, tx, u); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func (r *Repository) enqueueIdentity(ctx context.Context, tx *sql.Tx, user User) error {
	if r.identityTopic == "" {
		return nil
	}
	return projectionevent.Enqueue(ctx, tx, r.identityTopic, "IDENTITY", projectionevent.UserAggregateID(user.ID), projectionevent.IdentityUserUpserted, strconv.FormatInt(user.ID, 10), projectionevent.IdentityPayload{
		UserID: user.ID, Nickname: user.Nickname, Avatar: user.Avatar, Status: user.Status,
	})
}

func limitRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
