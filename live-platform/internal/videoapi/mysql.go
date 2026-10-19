package videoapi

import (
	"context"
	"database/sql"
	"errors"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type MySQLRepository struct{ db DB }

func NewMySQLRepository(db DB) *MySQLRepository { return &MySQLRepository{db: db} }

const videoColumns = "id, author_id, title, description, media_url, cover_url, status, published_at, created_at, updated_at"

func (r *MySQLRepository) Create(ctx context.Context, video Video, key string) (Video, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Video{}, false, err
	}
	defer tx.Rollback()
	var id int64
	var result sql.Result
	if key == "" {
		result, err = tx.ExecContext(ctx, "INSERT INTO video.videos(author_id,title,description,media_url,cover_url,status,published_at) VALUES(?,?,?,?,?,2,NOW(3))", video.AuthorID, video.Title, video.Description, video.MediaURL, video.CoverURL)
	} else {
		result, err = tx.ExecContext(ctx, "INSERT INTO video.videos(author_id,title,description,media_url,cover_url,status,published_at,idempotency_key) VALUES(?,?,?,?,?,2,NOW(3),?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)", video.AuthorID, video.Title, video.Description, video.MediaURL, video.CoverURL, key)
	}
	if err != nil {
		return Video{}, false, err
	}
	id, err = result.LastInsertId()
	if err != nil {
		return Video{}, false, err
	}
	created := true
	if key != "" {
		affected, err := result.RowsAffected()
		if err != nil {
			return Video{}, false, err
		}
		created = affected == 1
	}
	if created {
		if _, err := tx.ExecContext(ctx, "INSERT INTO video.video_stats(video_id) VALUES(?)", id); err != nil {
			return Video{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Video{}, false, err
	}
	got, err := r.Get(ctx, id)
	return got, created, err
}

func (r *MySQLRepository) Get(ctx context.Context, id int64) (Video, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+videoColumns+", s.like_count, s.comment_count, s.favorite_count FROM video.videos v JOIN video.video_stats s ON s.video_id=v.id WHERE v.id=? AND v.status=2", id)
	return scanVideo(row)
}

func (r *MySQLRepository) Delete(ctx context.Context, id, authorID int64) error {
	var owner int64
	err := r.db.QueryRowContext(ctx, "SELECT author_id FROM video.videos WHERE id=? AND status<>4", id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != authorID {
		return ErrForbidden
	}
	_, err = r.db.ExecContext(ctx, "UPDATE video.videos SET status=4, updated_at=NOW(3) WHERE id=?", id)
	return err
}

func (r *MySQLRepository) ListPublicByAuthor(ctx context.Context, authorID int64, limit, offset int) ([]Video, error) {
	return r.list(ctx, "WHERE v.author_id=? AND v.status=2", authorID, limit, offset)
}
func (r *MySQLRepository) ListMine(ctx context.Context, authorID int64, limit, offset int) ([]Video, error) {
	return r.list(ctx, "WHERE v.author_id=? AND v.status<>4", authorID, limit, offset)
}
func (r *MySQLRepository) list(ctx context.Context, where string, authorID int64, limit, offset int) ([]Video, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+videoColumns+", s.like_count, s.comment_count, s.favorite_count FROM video.videos v JOIN video.video_stats s ON s.video_id=v.id "+where+" ORDER BY v.published_at DESC, v.id DESC LIMIT ? OFFSET ?", authorID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Video, 0)
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanVideo(s scanner) (Video, error) {
	var v Video
	var published sql.NullTime
	err := s.Scan(&v.ID, &v.AuthorID, &v.Title, &v.Description, &v.MediaURL, &v.CoverURL, &v.Status, &published, &v.CreatedAt, &v.UpdatedAt, &v.LikeCount, &v.CommentCount, &v.FavoriteCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Video{}, ErrNotFound
		}
		return Video{}, err
	}
	if published.Valid {
		t := published.Time
		v.PublishedAt = &t
	}
	v.StatusName = "published"
	return v, nil
}

var _ Repository = (*MySQLRepository)(nil)
