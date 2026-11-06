package auth

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/example/live-platform/internal/projectionevent"
)

// RebuildProjectionBatch republishes an authoritative identity snapshot through
// the normal Outbox/Kafka path. The serializable transaction prevents a stale
// snapshot event from being ordered after a concurrent user update.
func (r *Repository) RebuildProjectionBatch(ctx context.Context, afterID int64, limit int) (int64, int, error) {
	if strings.TrimSpace(r.identityTopic) == "" {
		return afterID, 0, errors.New("identity projection topic is required")
	}
	limit = projectionBatchLimit(limit)
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return afterID, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
SELECT id, nickname, avatar, status
FROM users
WHERE id > ?
ORDER BY id
LIMIT ?`, afterID, limit)
	if err != nil {
		return afterID, 0, err
	}
	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Nickname, &user.Avatar, &user.Status); err != nil {
			_ = rows.Close()
			return afterID, 0, err
		}
		users = append(users, user)
	}
	if err := rows.Close(); err != nil {
		return afterID, 0, err
	}
	if err := rows.Err(); err != nil {
		return afterID, 0, err
	}
	for _, user := range users {
		if err := projectionevent.Enqueue(ctx, tx, r.identityTopic, "IDENTITY", projectionevent.UserAggregateID(user.ID), projectionevent.IdentityUserUpserted, strconv.FormatInt(user.ID, 10), projectionevent.IdentityPayload{
			UserID: user.ID, Nickname: user.Nickname, Avatar: user.Avatar, Status: user.Status,
		}); err != nil {
			return afterID, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return afterID, 0, err
	}
	if len(users) == 0 {
		return afterID, 0, nil
	}
	return users[len(users)-1].ID, len(users), nil
}

func projectionBatchLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}
