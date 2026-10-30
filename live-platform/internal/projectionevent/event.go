package projectionevent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/example/live-platform/internal/idgen"
	"github.com/example/live-platform/internal/mq"
)

const (
	Producer             = "live-identity-room"
	IdentityUserUpserted = "identity.user.upserted"
	RoomUpserted         = "room.upserted"
	GovernanceChanged    = "room.governance.changed"
)

type IdentityPayload struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
	Status   int    `json:"status"`
}

type RoomPayload struct {
	RoomID    int64      `json:"room_id"`
	AnchorID  int64      `json:"anchor_id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type GovernancePayload struct {
	RoomID     int64      `json:"room_id"`
	UserID     int64      `json:"user_id"`
	Banned     bool       `json:"banned"`
	Muted      bool       `json:"muted"`
	MutedUntil *time.Time `json:"muted_until,omitempty"`
}

type Tx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Enqueue uses the outbox row id as a monotonic aggregate version. Gaps are
// harmless; consumers only require newer changes to have larger versions.
func Enqueue(ctx context.Context, tx Tx, topic, aggregateType, aggregateID, eventType, partitionKey string, payload any) error {
	if strings.TrimSpace(topic) == "" {
		return errors.New("projection topic is required")
	}
	eventID := idgen.New()
	result, err := tx.ExecContext(ctx, `
INSERT INTO outbox_events(event_id, aggregate_type, aggregate_id, event_type, topic, partition_key, payload, status, retry_count, next_retry_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, JSON_OBJECT(), 0, 0, NOW(3), NOW(3))`, eventID, aggregateType, aggregateID, eventType, topic, partitionKey)
	if err != nil {
		return err
	}
	version, err := result.LastInsertId()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(mq.Envelope{
		EventID: eventID, EventType: eventType, Version: 1, Producer: Producer,
		AggregateType: aggregateType, AggregateID: aggregateID, AggregateVersion: version,
		CreatedAt: time.Now().UTC(), Payload: raw,
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE outbox_events SET payload=? WHERE id=?`, envelope, version)
	return err
}

func UserAggregateID(userID int64) string { return strconv.FormatInt(userID, 10) }
func RoomAggregateID(roomID int64) string { return strconv.FormatInt(roomID, 10) }
func GovernanceAggregateID(roomID, userID int64) string {
	return strconv.FormatInt(roomID, 10) + ":" + strconv.FormatInt(userID, 10)
}
