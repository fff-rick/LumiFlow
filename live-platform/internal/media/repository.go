package media

import (
	"context"
	"database/sql"
	"errors"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (r *Repository) SaveRecording(ctx context.Context, value Recording) (Recording, error) {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO live_recordings(room_id, provider, object_key, created_at)
VALUES (?, ?, ?, NOW(3))
ON DUPLICATE KEY UPDATE object_key=VALUES(object_key)`, value.RoomID, value.Provider, value.ObjectKey)
	if err != nil {
		return Recording{}, err
	}
	return scanRecording(r.db.QueryRowContext(ctx, `
SELECT id, room_id, provider, object_key, created_at
FROM live_recordings WHERE object_key=? LIMIT 1`, value.ObjectKey))
}

func (r *Repository) ListRecordings(ctx context.Context, roomID int64) ([]Recording, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, room_id, provider, object_key, created_at
FROM live_recordings WHERE room_id=? ORDER BY created_at DESC, id DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Recording, 0)
	for rows.Next() {
		var value Recording
		if err := rows.Scan(&value.ID, &value.RoomID, &value.Provider, &value.ObjectKey, &value.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func scanRecording(row *sql.Row) (Recording, error) {
	var value Recording
	err := row.Scan(&value.ID, &value.RoomID, &value.Provider, &value.ObjectKey, &value.CreatedAt)
	return value, err
}

type Repository struct{ db DB }

func NewRepository(db DB) *Repository { return &Repository{db: db} }

func (r *Repository) Save(ctx context.Context, value Session) (Session, error) {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO live_media_sessions(room_id, provider, app, stream_name, publish_token_hash, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, NOW(3), NOW(3))
ON DUPLICATE KEY UPDATE provider=VALUES(provider), app=VALUES(app), stream_name=VALUES(stream_name),
publish_token_hash=VALUES(publish_token_hash), status=VALUES(status), published_at=NULL,
disconnected_at=NULL, updated_at=NOW(3)`, value.RoomID, value.Provider, value.App, value.StreamName, value.PublishHash, value.Status)
	if err != nil {
		return Session{}, err
	}
	return r.ByRoom(ctx, value.RoomID)
}

func (r *Repository) ByRoom(ctx context.Context, roomID int64) (Session, error) {
	return scanSession(r.db.QueryRowContext(ctx, `
SELECT room_id, provider, app, stream_name, publish_token_hash, status,
       published_at, disconnected_at, created_at, updated_at
FROM live_media_sessions WHERE room_id=? LIMIT 1`, roomID))
}

func (r *Repository) ByStream(ctx context.Context, app, streamName string) (Session, error) {
	return scanSession(r.db.QueryRowContext(ctx, `
SELECT room_id, provider, app, stream_name, publish_token_hash, status,
       published_at, disconnected_at, created_at, updated_at
FROM live_media_sessions WHERE app=? AND stream_name=? LIMIT 1`, app, streamName))
}

func (r *Repository) SetStatus(ctx context.Context, roomID int64, status Status) error {
	query := `UPDATE live_media_sessions SET status=?, updated_at=NOW(3)`
	if status == StatusPublishing {
		query += `, published_at=NOW(3), disconnected_at=NULL`
	}
	if status == StatusDisconnected {
		query += `, disconnected_at=NOW(3)`
	}
	query += ` WHERE room_id=?`
	result, err := r.db.ExecContext(ctx, query, status, roomID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotConfigured
	}
	return nil
}

func scanSession(row *sql.Row) (Session, error) {
	var value Session
	err := row.Scan(&value.RoomID, &value.Provider, &value.App, &value.StreamName, &value.PublishHash,
		&value.Status, &value.PublishedAt, &value.DisconnectedAt, &value.CreatedAt, &value.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotConfigured
	}
	return value, err
}
