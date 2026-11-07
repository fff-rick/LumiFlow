package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

func (r *Repository) SaveRecording(ctx context.Context, value Recording) (Recording, error) {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO live_recordings(room_id, provider, object_key, storage_status, created_at, updated_at)
VALUES (?, ?, ?, ?, NOW(3), NOW(3))
ON DUPLICATE KEY UPDATE object_key=VALUES(object_key)`, value.RoomID, value.Provider, value.ObjectKey, value.StorageStatus)
	if err != nil {
		return Recording{}, err
	}
	return scanRecording(r.db.QueryRowContext(ctx, `
SELECT id, room_id, provider, object_key, storage_status, size_bytes, archive_attempts,
       archive_started_at, archived_at, created_at
FROM live_recordings WHERE object_key=? LIMIT 1`, value.ObjectKey))
}

func (r *Repository) ListRecordings(ctx context.Context, roomID int64) ([]Recording, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, room_id, provider, object_key, storage_status, size_bytes, archive_attempts,
       archive_started_at, archived_at, created_at
FROM live_recordings
WHERE room_id=? AND storage_status IN ('LOCAL', 'ARCHIVED')
ORDER BY created_at DESC, id DESC`, roomID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]Recording, 0)
	for rows.Next() {
		var value Recording
		if err := scanRecordingValues(rows.Scan, &value); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}

func scanRecording(row *sql.Row) (Recording, error) {
	var value Recording
	err := scanRecordingValues(row.Scan, &value)
	return value, err
}

func scanRecordingValues(scan func(...any) error, value *Recording) error {
	return scan(&value.ID, &value.RoomID, &value.Provider, &value.ObjectKey, &value.StorageStatus,
		&value.SizeBytes, &value.ArchiveAttempts, &value.ArchiveStarted, &value.ArchivedAt, &value.CreatedAt)
}

func (r *Repository) ClaimRecording(ctx context.Context, lease time.Duration) (Recording, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Recording{}, err
	}
	defer func() { _ = tx.Rollback() }()
	staleBefore := time.Now().UTC().Add(-lease)
	row := tx.QueryRowContext(ctx, `
SELECT id, room_id, provider, object_key, storage_status, size_bytes, archive_attempts,
       archive_started_at, archived_at, created_at
FROM live_recordings
WHERE (storage_status IN ('PENDING', 'FAILED') AND (next_attempt_at IS NULL OR next_attempt_at <= NOW(3)))
   OR (storage_status='UPLOADING' AND archive_started_at < ?)
ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`, staleBefore)
	var value Recording
	if err := scanRecordingValues(row.Scan, &value); err != nil {
		return Recording{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE live_recordings
SET storage_status='UPLOADING', archive_attempts=archive_attempts+1,
    archive_started_at=NOW(3), last_error=NULL, updated_at=NOW(3)
WHERE id=?`, value.ID); err != nil {
		return Recording{}, err
	}
	if err := tx.Commit(); err != nil {
		return Recording{}, err
	}
	value.StorageStatus = RecordingUploading
	value.ArchiveAttempts++
	return value, nil
}

func (r *Repository) MarkRecordingArchived(ctx context.Context, id, size int64) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE live_recordings
SET provider='s3', storage_status='ARCHIVED', size_bytes=?, archived_at=NOW(3),
    archive_started_at=NULL, next_attempt_at=NULL, last_error=NULL, updated_at=NOW(3)
WHERE id=? AND storage_status='UPLOADING'`, size, id)
	if err != nil {
		return err
	}
	return requireChanged(result, id)
}

func (r *Repository) MarkRecordingFailed(ctx context.Context, id int64, retryAt time.Time, cause string) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE live_recordings
SET storage_status='FAILED', archive_started_at=NULL, next_attempt_at=?, last_error=?, updated_at=NOW(3)
WHERE id=? AND storage_status='UPLOADING'`, retryAt, cause, id)
	if err != nil {
		return err
	}
	return requireChanged(result, id)
}

func requireChanged(result sql.Result, id int64) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("recording %d changed rows=%d", id, changed)
	}
	return nil
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
