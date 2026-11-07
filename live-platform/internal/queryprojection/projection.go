package queryprojection

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/live-platform/internal/danmaku"
	"github.com/example/live-platform/internal/mq"
	"github.com/example/live-platform/internal/projectionevent"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type User struct {
	ID               int64
	Nickname         string
	Avatar           string
	Status           int
	AggregateVersion int64
	ProjectedAt      time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Message struct {
	Type      string
	MessageID string
	RoomID    int64
	UserID    int64
	Nickname  string
	Content   string
	GiftName  string
	GiftCount int64
	CreatedAt time.Time
}

type Projector interface {
	UpsertUser(context.Context, User) error
	InsertMessage(context.Context, Message) error
}

type Repository struct{ db DB }

func NewRepository(db DB) *Repository { return &Repository{db: db} }

func (r *Repository) UpsertUser(ctx context.Context, user User) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO user_display_projection(user_id, nickname, avatar, status, aggregate_version, projected_at, created_at, updated_at)
VALUES(?,?,?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE
nickname=IF(VALUES(aggregate_version)>aggregate_version OR VALUES(aggregate_version)=0 AND aggregate_version=0,VALUES(nickname),nickname),
avatar=IF(VALUES(aggregate_version)>aggregate_version OR VALUES(aggregate_version)=0 AND aggregate_version=0,VALUES(avatar),avatar),
status=IF(VALUES(aggregate_version)>aggregate_version OR VALUES(aggregate_version)=0 AND aggregate_version=0,VALUES(status),status),
projected_at=IF(VALUES(aggregate_version)>aggregate_version OR VALUES(aggregate_version)=0 AND aggregate_version=0,VALUES(projected_at),projected_at),
updated_at=IF(VALUES(aggregate_version)>aggregate_version OR VALUES(aggregate_version)=0 AND aggregate_version=0,VALUES(updated_at),updated_at),
aggregate_version=GREATEST(aggregate_version,VALUES(aggregate_version))`,
		user.ID, user.Nickname, user.Avatar, user.Status, user.AggregateVersion, user.ProjectedAt, user.CreatedAt, user.UpdatedAt)
	return err
}

func (r *Repository) InsertMessage(ctx context.Context, message Message) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO room_message_feed(type, message_id, room_id, user_id, nickname, content, gift_name, gift_count, created_at)
VALUES(?,?,?,?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE message_id=message_id`,
		message.Type, message.MessageID, message.RoomID, message.UserID, message.Nickname,
		message.Content, message.GiftName, message.GiftCount, message.CreatedAt)
	return err
}

type Topics struct {
	Identity string
	Danmaku  string
	Gift     string
}

type Handler struct {
	projector Projector
	topics    Topics
}

func NewHandler(projector Projector, topics Topics) *Handler {
	return &Handler{projector: projector, topics: topics}
}

func (h *Handler) Handle(ctx context.Context, record mq.Record) error {
	var envelope mq.Envelope
	if err := json.Unmarshal(record.Value, &envelope); err != nil {
		return mq.Permanent(fmt.Errorf("decode query projection envelope: %w", err))
	}
	switch record.Topic {
	case h.topics.Identity:
		return h.handleIdentity(ctx, envelope)
	case h.topics.Danmaku:
		return h.handleDanmaku(ctx, envelope)
	case h.topics.Gift:
		return h.handleGift(ctx, envelope)
	default:
		return mq.Permanent(fmt.Errorf("unsupported query projection topic %q", record.Topic))
	}
}

func (h *Handler) handleIdentity(ctx context.Context, envelope mq.Envelope) error {
	if envelope.EventID == "" || envelope.EventType != projectionevent.IdentityUserUpserted || envelope.Version != 1 ||
		envelope.AggregateType != "IDENTITY" || !projectionevent.ValidProducer(envelope.AggregateType, envelope.Producer) ||
		envelope.AggregateVersion <= 0 || envelope.CreatedAt.IsZero() {
		return mq.Permanent(errors.New("invalid query identity projection envelope"))
	}
	var payload projectionevent.IdentityPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.UserID <= 0 || strings.TrimSpace(payload.Nickname) == "" ||
		(payload.Status != 1 && payload.Status != 2) || envelope.AggregateID != projectionevent.UserAggregateID(payload.UserID) {
		return mq.Permanent(errors.New("invalid query identity projection payload"))
	}
	return h.projector.UpsertUser(ctx, User{
		ID: payload.UserID, Nickname: payload.Nickname, Avatar: payload.Avatar, Status: payload.Status,
		AggregateVersion: envelope.AggregateVersion, ProjectedAt: envelope.CreatedAt,
		CreatedAt: envelope.CreatedAt, UpdatedAt: envelope.CreatedAt,
	})
}

func (h *Handler) handleDanmaku(ctx context.Context, envelope mq.Envelope) error {
	if err := envelope.ValidateRoomEnvelope(); err != nil || envelope.EventType != mq.EventTypeDanmakuSent {
		return mq.Permanent(errors.New("invalid query danmaku projection envelope"))
	}
	var payload danmaku.Event
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.MessageID == "" || payload.MessageID != envelope.EventID ||
		payload.RoomID <= 0 || payload.RoomID != envelope.RoomID || payload.UserID <= 0 || strings.TrimSpace(payload.Nickname) == "" ||
		strings.TrimSpace(payload.Content) == "" || payload.CreatedAt.IsZero() {
		return mq.Permanent(errors.New("invalid query danmaku projection payload"))
	}
	return h.projector.InsertMessage(ctx, Message{
		Type: "danmaku", MessageID: payload.MessageID, RoomID: payload.RoomID, UserID: payload.UserID,
		Nickname: payload.Nickname, Content: payload.Content, CreatedAt: payload.CreatedAt,
	})
}

type giftPayload struct {
	OrderNo     string `json:"order_no"`
	UserID      int64  `json:"user_id"`
	AnchorID    int64  `json:"anchor_id"`
	GiftID      int64  `json:"gift_id"`
	GiftName    string `json:"gift_name"`
	Count       int64  `json:"count"`
	UnitPrice   int64  `json:"unit_price"`
	TotalAmount int64  `json:"total_amount"`
}

func (h *Handler) handleGift(ctx context.Context, envelope mq.Envelope) error {
	if err := envelope.ValidateRoomEnvelope(); err != nil || envelope.EventType != mq.EventTypeGiftSent {
		return mq.Permanent(errors.New("invalid query gift projection envelope"))
	}
	var payload giftPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil || strings.TrimSpace(payload.OrderNo) == "" || payload.UserID <= 0 ||
		payload.AnchorID <= 0 || payload.GiftID <= 0 || strings.TrimSpace(payload.GiftName) == "" || payload.Count <= 0 ||
		payload.UnitPrice < 0 || payload.TotalAmount < 0 {
		return mq.Permanent(errors.New("invalid query gift projection payload"))
	}
	return h.projector.InsertMessage(ctx, Message{
		Type: "gift", MessageID: "gift:" + strings.TrimSpace(payload.OrderNo), RoomID: envelope.RoomID,
		UserID: payload.UserID, GiftName: payload.GiftName, GiftCount: payload.Count, CreatedAt: envelope.CreatedAt,
	})
}

var _ Projector = (*Repository)(nil)
