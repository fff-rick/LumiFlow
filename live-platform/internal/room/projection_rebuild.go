package room

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/example/live-platform/internal/projectionevent"
)

// RebuildRoomProjectionBatch republishes authoritative rooms through Outbox.
func (r *Repository) RebuildRoomProjectionBatch(ctx context.Context, afterID int64, limit int) (int64, int, error) {
	if strings.TrimSpace(r.roomTopic) == "" {
		return afterID, 0, errors.New("room projection topic is required")
	}
	limit = projectionBatchLimit(limit)
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return afterID, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
SELECT id, anchor_id, title, status, started_at, ended_at, created_at
FROM live_rooms
WHERE id > ?
ORDER BY id
LIMIT ?`, afterID, limit)
	if err != nil {
		return afterID, 0, err
	}
	var rooms []Room
	for rows.Next() {
		var value Room
		var started, ended sql.NullTime
		if err := rows.Scan(&value.ID, &value.AnchorID, &value.Title, &value.Status, &started, &ended, &value.CreatedAt); err != nil {
			_ = rows.Close()
			return afterID, 0, err
		}
		if started.Valid {
			value.StartedAt = &started.Time
		}
		if ended.Valid {
			value.EndedAt = &ended.Time
		}
		rooms = append(rooms, value)
	}
	if err := rows.Close(); err != nil {
		return afterID, 0, err
	}
	if err := rows.Err(); err != nil {
		return afterID, 0, err
	}
	for _, value := range rooms {
		if err := r.enqueueRoom(ctx, tx, value); err != nil {
			return afterID, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return afterID, 0, err
	}
	if len(rooms) == 0 {
		return afterID, 0, nil
	}
	return rooms[len(rooms)-1].ID, len(rooms), nil
}

// RebuildGovernanceProjectionBatch includes historical aggregate IDs so a
// rebuild also emits explicit false states for users who were unmuted/unbanned.
func (r *Repository) RebuildGovernanceProjectionBatch(ctx context.Context, afterRoomID, afterUserID int64, limit int) (int64, int64, int, error) {
	if strings.TrimSpace(r.governanceTopic) == "" {
		return afterRoomID, afterUserID, 0, errors.New("governance projection topic is required")
	}
	limit = projectionBatchLimit(limit)
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return afterRoomID, afterUserID, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
SELECT aggregates.room_id, aggregates.user_id,
       EXISTS(SELECT 1 FROM room_bans b WHERE b.room_id=aggregates.room_id AND b.user_id=aggregates.user_id),
       EXISTS(SELECT 1 FROM room_mutes m WHERE m.room_id=aggregates.room_id AND m.user_id=aggregates.user_id AND (m.muted_until IS NULL OR m.muted_until>NOW(3))),
       (SELECT muted_until FROM room_mutes m WHERE m.room_id=aggregates.room_id AND m.user_id=aggregates.user_id LIMIT 1)
FROM (
    SELECT room_id, user_id FROM room_bans
    UNION
    SELECT room_id, user_id FROM room_mutes
    UNION
    SELECT CAST(SUBSTRING_INDEX(aggregate_id, ':', 1) AS UNSIGNED),
           CAST(SUBSTRING_INDEX(aggregate_id, ':', -1) AS UNSIGNED)
    FROM outbox_events WHERE aggregate_type='ROOM_GOVERNANCE'
) AS aggregates
WHERE aggregates.room_id > ? OR (aggregates.room_id = ? AND aggregates.user_id > ?)
ORDER BY aggregates.room_id, aggregates.user_id
LIMIT ?`, afterRoomID, afterRoomID, afterUserID, limit)
	if err != nil {
		return afterRoomID, afterUserID, 0, err
	}
	type governance struct {
		roomID, userID int64
		banned, muted  int
		mutedUntil     sql.NullTime
	}
	var items []governance
	for rows.Next() {
		var item governance
		if err := rows.Scan(&item.roomID, &item.userID, &item.banned, &item.muted, &item.mutedUntil); err != nil {
			_ = rows.Close()
			return afterRoomID, afterUserID, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return afterRoomID, afterUserID, 0, err
	}
	if err := rows.Err(); err != nil {
		return afterRoomID, afterUserID, 0, err
	}
	for _, item := range items {
		var mutedUntil *time.Time
		if item.mutedUntil.Valid {
			mutedUntil = &item.mutedUntil.Time
		}
		if err := projectionevent.Enqueue(ctx, tx, r.governanceTopic, "ROOM_GOVERNANCE", projectionevent.GovernanceAggregateID(item.roomID, item.userID), projectionevent.GovernanceChanged, strconv.FormatInt(item.roomID, 10), projectionevent.GovernancePayload{
			RoomID: item.roomID, UserID: item.userID, Banned: item.banned == 1, Muted: item.muted == 1, MutedUntil: mutedUntil,
		}); err != nil {
			return afterRoomID, afterUserID, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return afterRoomID, afterUserID, 0, err
	}
	if len(items) == 0 {
		return afterRoomID, afterUserID, 0, nil
	}
	last := items[len(items)-1]
	return last.roomID, last.userID, len(items), nil
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
