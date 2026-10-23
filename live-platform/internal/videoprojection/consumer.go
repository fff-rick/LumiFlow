package videoprojection

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/example/live-platform/internal/eventdedup"
	"github.com/example/live-platform/internal/mq"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
type Deduper interface {
	Begin(context.Context, string, string, time.Duration) (bool, error)
	Done(context.Context, string, string) error
	Fail(context.Context, string, string, error) error
}
type Handler struct {
	group string
	dedup Deduper
	db    DB
	lease time.Duration
}
type event struct {
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	Version    int       `json:"version"`
	Producer   string    `json:"producer"`
	OccurredAt time.Time `json:"occurred_at"`
	Subject    struct {
		UserID  int64 `json:"fluxfeed_user_id"`
		VideoID int64 `json:"flux_video_id"`
	} `json:"subject"`
	Payload struct {
		Title       string    `json:"title"`
		Description string    `json:"description"`
		MediaURL    string    `json:"media_url"`
		CoverURL    string    `json:"cover_url"`
		PublishedAt time.Time `json:"published_at"`
		Visible     bool      `json:"visible"`
	} `json:"payload"`
}

func NewHandler(group string, dedup Deduper, db DB, lease time.Duration) *Handler {
	return &Handler{group: group, dedup: dedup, db: db, lease: lease}
}
func (h *Handler) Handle(ctx context.Context, record mq.Record) error {
	var e event
	if err := json.Unmarshal(record.Value, &e); err != nil {
		return mq.Permanent(err)
	}
	if e.EventID == "" || e.Version != 1 || e.Producer != "fluxfeed" || e.OccurredAt.IsZero() || e.Subject.UserID <= 0 || e.Subject.VideoID <= 0 || (e.EventType != "video.published" && e.EventType != "video.visibility_changed") {
		return mq.Permanent(errors.New("invalid integration.video.v1 envelope"))
	}
	if e.EventType == "video.published" && (strings.TrimSpace(e.Payload.Title) == "" || strings.TrimSpace(e.Payload.MediaURL) == "" || e.Payload.PublishedAt.IsZero()) {
		return mq.Permanent(errors.New("invalid published video payload"))
	}
	done, err := h.dedup.Begin(ctx, h.group, e.EventID, h.lease)
	if err != nil {
		return err
	}
	if done {
		return nil
	}
	if e.EventType == "video.published" {
		_, err = h.db.ExecContext(ctx, `INSERT INTO fluxfeed_video_projection(flux_video_id,fluxfeed_user_id,title,description,media_url,cover_url,visible,published_at,updated_at) VALUES(?,?,?,?,?,?,?,?,NOW(3)) ON DUPLICATE KEY UPDATE title=VALUES(title),description=VALUES(description),media_url=VALUES(media_url),cover_url=VALUES(cover_url),visible=VALUES(visible),published_at=VALUES(published_at),updated_at=NOW(3)`, e.Subject.VideoID, e.Subject.UserID, e.Payload.Title, e.Payload.Description, e.Payload.MediaURL, e.Payload.CoverURL, e.Payload.Visible, e.Payload.PublishedAt)
	} else {
		_, err = h.db.ExecContext(ctx, `UPDATE fluxfeed_video_projection SET visible=?,updated_at=NOW(3) WHERE flux_video_id=?`, e.Payload.Visible, e.Subject.VideoID)
	}
	if err != nil {
		_ = h.dedup.Fail(ctx, h.group, e.EventID, err)
		return err
	}
	if err = h.dedup.Done(ctx, h.group, e.EventID); err != nil {
		return err
	}
	return nil
}

var _ Deduper = (*eventdedup.Store)(nil)
