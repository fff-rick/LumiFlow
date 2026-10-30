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

type DB interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Repository struct {
	db               DB
	integrationTopic string
	roomTopic        string
	governanceTopic  string
}

func NewRepository(db DB, topics ...string) *Repository {
	r := &Repository{db: db}
	if len(topics) > 0 {
		r.integrationTopic = strings.TrimSpace(topics[0])
	}
	if len(topics) > 1 {
		r.roomTopic = strings.TrimSpace(topics[1])
	}
	if len(topics) > 2 {
		r.governanceTopic = strings.TrimSpace(topics[2])
	}
	return r
}

func (r *Repository) Create(ctx context.Context, anchorID int64, title string) (Room, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
INSERT INTO live_rooms(anchor_id, title, status, created_at, updated_at)
VALUES (?, ?, 'PREPARING', NOW(3), NOW(3))`, anchorID, title)
	if err != nil {
		return Room{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Room{}, err
	}
	v, err := roomByID(ctx, tx, id)
	if err != nil {
		return Room{}, err
	}
	if err := r.enqueueRoom(ctx, tx, v); err != nil {
		return Room{}, err
	}
	if err := tx.Commit(); err != nil {
		return Room{}, err
	}
	return v, nil
}

func (r *Repository) ByID(ctx context.Context, id int64) (Room, error) {
	return roomByID(ctx, r.db, id)
}

type roomQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func roomByID(ctx context.Context, db roomQueryer, id int64) (Room, error) {
	var v Room
	var started, ended sql.NullTime
	err := db.QueryRowContext(ctx, `
SELECT id, anchor_id, title, status, started_at, ended_at, created_at
FROM live_rooms
WHERE id = ? LIMIT 1`, id).
		Scan(&v.ID, &v.AnchorID, &v.Title, &v.Status, &started, &ended, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, err
	}
	if started.Valid {
		t := started.Time
		v.StartedAt = &t
	}
	if ended.Valid {
		t := ended.Time
		v.EndedAt = &t
	}
	return v, nil
}

func (r *Repository) List(ctx context.Context, status Status, limit int) ([]Room, error) {
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	query := `
SELECT id, anchor_id, title, status, started_at, ended_at, created_at
FROM live_rooms`
	args := make([]any, 0, 2)
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY COALESCE(started_at, created_at) DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]Room, 0, limit)
	for rows.Next() {
		var v Room
		var started, ended sql.NullTime
		if err := rows.Scan(&v.ID, &v.AnchorID, &v.Title, &v.Status, &started, &ended, &v.CreatedAt); err != nil {
			return nil, err
		}
		if started.Valid {
			t := started.Time
			v.StartedAt = &t
		}
		if ended.Valid {
			t := ended.Time
			v.EndedAt = &t
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) ChangeStatus(ctx context.Context, value Room, from, to Status) error {
	if r.integrationTopic == "" {
		return errors.New("room integration topic is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	query := `UPDATE live_rooms SET status=?, updated_at=NOW(3)`
	if to == StatusLiving {
		query += `, started_at=NOW(3), ended_at=NULL`
	}
	if to == StatusClosed {
		query += `, ended_at=NOW(3)`
	}
	query += ` WHERE id=? AND anchor_id=? AND status=?`
	res, err := tx.ExecContext(ctx, query, to, value.ID, value.AnchorID, from)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalidState
	}
	var startedAt, endedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT started_at, ended_at FROM live_rooms WHERE id=?`, value.ID).Scan(&startedAt, &endedAt); err != nil {
		return err
	}
	value.Status = to
	if startedAt.Valid {
		value.StartedAt = &startedAt.Time
	}
	if endedAt.Valid {
		value.EndedAt = &endedAt.Time
	}
	eventType := EventTypeStarted
	if to == StatusClosed {
		eventType = EventTypeStopped
	}
	eventID, payload, err := newIntegrationEvent(eventType, value, time.Now())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO outbox_events(event_id, aggregate_type, aggregate_id, event_type, topic, partition_key, payload, status, retry_count, next_retry_at, created_at)
VALUES (?, 'ROOM', ?, ?, ?, ?, ?, 0, 0, NOW(3), NOW(3))`, eventID, strconv.FormatInt(value.ID, 10), eventType, r.integrationTopic, strconv.FormatInt(value.ID, 10), payload); err != nil {
		return err
	}
	if err := r.enqueueRoom(ctx, tx, value); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Join(ctx context.Context, roomID, userID int64) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO live_sessions(room_id, user_id, joined_at, last_seen_at, status)
VALUES (?, ?, NOW(3), NOW(3), 1)
ON DUPLICATE KEY UPDATE last_seen_at=NOW(3), status=1`, roomID, userID)
	return err
}

func (r *Repository) IsBanned(ctx context.Context, roomID, userID int64) (bool, error) {
	var banned int
	err := r.db.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM room_bans WHERE room_id=? AND user_id=?)`, roomID, userID).Scan(&banned)
	return banned == 1, err
}

func (r *Repository) Ban(ctx context.Context, roomID, userID, createdBy int64, reason string) error {
	return r.changeGovernance(ctx, roomID, userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO room_bans(room_id, user_id, created_by, reason, created_at)
VALUES (?, ?, ?, ?, NOW(3))
ON DUPLICATE KEY UPDATE created_by=VALUES(created_by), reason=VALUES(reason), created_at=NOW(3)`, roomID, userID, createdBy, reason)
		return err
	})
}

func (r *Repository) Unban(ctx context.Context, roomID, userID int64) error {
	return r.changeGovernance(ctx, roomID, userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM room_bans WHERE room_id=? AND user_id=?`, roomID, userID)
		return err
	})
}

func (r *Repository) ListBans(ctx context.Context, roomID int64) ([]Ban, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT user_id, reason, created_at
FROM room_bans
WHERE room_id=? ORDER BY created_at DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]Ban, 0)
	for rows.Next() {
		var item Ban
		if err := rows.Scan(&item.UserID, &item.Reason, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) IsMuted(ctx context.Context, roomID, userID int64) (bool, error) {
	var muted int
	err := r.db.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM room_mutes
  WHERE room_id=? AND user_id=? AND (muted_until IS NULL OR muted_until > NOW(3))
)`, roomID, userID).Scan(&muted)
	return muted == 1, err
}

func (r *Repository) Mute(ctx context.Context, roomID, userID, createdBy int64, until *time.Time, reason string) error {
	return r.changeGovernance(ctx, roomID, userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO room_mutes(room_id, user_id, muted_until, created_by, reason, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, NOW(3), NOW(3))
ON DUPLICATE KEY UPDATE muted_until=VALUES(muted_until), created_by=VALUES(created_by), reason=VALUES(reason), updated_at=NOW(3)`,
			roomID, userID, until, createdBy, reason)
		return err
	})
}

func (r *Repository) Unmute(ctx context.Context, roomID, userID int64) error {
	return r.changeGovernance(ctx, roomID, userID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM room_mutes WHERE room_id=? AND user_id=?`, roomID, userID)
		return err
	})
}

func (r *Repository) ListMutes(ctx context.Context, roomID int64) ([]Mute, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT user_id, muted_until, reason, updated_at
FROM room_mutes
WHERE room_id=? AND (muted_until IS NULL OR muted_until > NOW(3))
ORDER BY updated_at DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]Mute, 0)
	for rows.Next() {
		var item Mute
		if err := rows.Scan(&item.UserID, &item.MutedUntil, &item.Reason, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) enqueueRoom(ctx context.Context, tx *sql.Tx, value Room) error {
	if r.roomTopic == "" {
		return nil
	}
	return projectionevent.Enqueue(ctx, tx, r.roomTopic, "ROOM", projectionevent.RoomAggregateID(value.ID), projectionevent.RoomUpserted, strconv.FormatInt(value.ID, 10), projectionevent.RoomPayload{
		RoomID: value.ID, AnchorID: value.AnchorID, Title: value.Title, Status: string(value.Status),
		StartedAt: value.StartedAt, EndedAt: value.EndedAt, CreatedAt: value.CreatedAt,
	})
}

func (r *Repository) changeGovernance(ctx context.Context, roomID, userID int64, change func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := change(tx); err != nil {
		return err
	}
	if r.governanceTopic != "" {
		var banned, muted int
		var mutedUntil sql.NullTime
		if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(SELECT 1 FROM room_bans WHERE room_id=? AND user_id=?),
       EXISTS(SELECT 1 FROM room_mutes WHERE room_id=? AND user_id=? AND (muted_until IS NULL OR muted_until>NOW(3))),
       (SELECT muted_until FROM room_mutes WHERE room_id=? AND user_id=? LIMIT 1)`,
			roomID, userID, roomID, userID, roomID, userID).Scan(&banned, &muted, &mutedUntil); err != nil {
			return err
		}
		var until *time.Time
		if mutedUntil.Valid {
			until = &mutedUntil.Time
		}
		if err := projectionevent.Enqueue(ctx, tx, r.governanceTopic, "ROOM_GOVERNANCE", projectionevent.GovernanceAggregateID(roomID, userID), projectionevent.GovernanceChanged, strconv.FormatInt(roomID, 10), projectionevent.GovernancePayload{
			RoomID: roomID, UserID: userID, Banned: banned == 1, Muted: muted == 1, MutedUntil: until,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
