package accessprojection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/example/live-platform/internal/mq"
	"github.com/example/live-platform/internal/projectionevent"
	"github.com/example/live-platform/internal/store/redisstore"
)

const (
	NamespaceInteraction = "interaction"
	NamespaceCommerce    = "commerce"
)

type Topics struct {
	Identity   string
	Room       string
	Governance string
}

type Store interface {
	ApplyProjection(context.Context, string, redisstore.ProjectionRecord) (bool, error)
	RefreshProjection(context.Context, string, int64, time.Time, []byte) (bool, error)
	LoadProjection(context.Context, string) (redisstore.ProjectionRecord, bool, error)
}

type Metrics interface {
	ProjectionRead(namespace, mode, entity, result string, age time.Duration)
	ProjectionFallback(namespace, mode, entity, result string)
	ProjectionCompared(namespace, entity, result string)
	ProjectionApplied(namespace, entity, result string)
	ProjectionFailedClosed(namespace, mode, entity string)
}

type Handler struct {
	namespace string
	store     Store
	topics    Topics
	metrics   Metrics
}

func NewHandler(namespace string, store Store, topics Topics, metrics Metrics) *Handler {
	return &Handler{namespace: namespace, store: store, topics: topics, metrics: metrics}
}

func (h *Handler) Handle(ctx context.Context, record mq.Record) error {
	var event mq.Envelope
	if err := json.Unmarshal(record.Value, &event); err != nil {
		return mq.Permanent(fmt.Errorf("decode access projection: %w", err))
	}
	if event.EventID == "" || event.Version != 1 || event.Producer != projectionevent.Producer || event.AggregateVersion <= 0 || event.CreatedAt.IsZero() || len(event.Payload) == 0 {
		return mq.Permanent(errors.New("invalid access projection envelope"))
	}
	key, err := h.key(record.Topic, event)
	if err != nil {
		return mq.Permanent(err)
	}
	applied, err := h.store.ApplyProjection(ctx, key, redisstore.ProjectionRecord{
		Version: event.AggregateVersion, ProjectedAt: time.Now().UTC(), Topic: record.Topic,
		Partition: record.Partition, Offset: record.Offset, Payload: event.Payload,
	})
	if h.metrics != nil {
		result := "applied"
		if err != nil {
			result = "error"
		} else if !applied {
			result = "out_of_order"
		}
		h.metrics.ProjectionApplied(h.namespace, projectionEntity(event.EventType), result)
	}
	return err
}

func projectionEntity(eventType string) string {
	switch eventType {
	case projectionevent.IdentityUserUpserted:
		return "identity"
	case projectionevent.RoomUpserted:
		return "room"
	case projectionevent.GovernanceChanged:
		return "governance"
	default:
		return "unknown"
	}
}

func (h *Handler) key(topic string, event mq.Envelope) (string, error) {
	switch event.EventType {
	case projectionevent.IdentityUserUpserted:
		var payload projectionevent.IdentityPayload
		if topic != h.topics.Identity || event.AggregateType != "IDENTITY" || json.Unmarshal(event.Payload, &payload) != nil || payload.UserID <= 0 || payload.Nickname == "" || (payload.Status != 1 && payload.Status != 2) || event.AggregateID != projectionevent.UserAggregateID(payload.UserID) {
			return "", errors.New("invalid identity projection event")
		}
		return identityKey(h.namespace, payload.UserID), nil
	case projectionevent.RoomUpserted:
		var payload projectionevent.RoomPayload
		if topic != h.topics.Room || event.AggregateType != "ROOM" || json.Unmarshal(event.Payload, &payload) != nil || payload.RoomID <= 0 || payload.AnchorID <= 0 || payload.Title == "" || !validRoomStatus(payload.Status) || event.AggregateID != projectionevent.RoomAggregateID(payload.RoomID) {
			return "", errors.New("invalid room projection event")
		}
		return roomKey(h.namespace, payload.RoomID), nil
	case projectionevent.GovernanceChanged:
		var payload projectionevent.GovernancePayload
		if topic != h.topics.Governance || event.AggregateType != "ROOM_GOVERNANCE" || json.Unmarshal(event.Payload, &payload) != nil || payload.RoomID <= 0 || payload.UserID <= 0 || event.AggregateID != projectionevent.GovernanceAggregateID(payload.RoomID, payload.UserID) {
			return "", errors.New("invalid governance projection event")
		}
		return governanceKey(h.namespace, payload.RoomID, payload.UserID), nil
	default:
		return "", fmt.Errorf("unsupported access projection event type %q", event.EventType)
	}
}

func validRoomStatus(status string) bool {
	return status == "PREPARING" || status == "LIVING" || status == "CLOSED"
}

func identityKey(namespace string, userID int64) string {
	return "live:" + namespace + ":projection:identity:" + strconv.FormatInt(userID, 10)
}

func roomKey(namespace string, roomID int64) string {
	return "live:" + namespace + ":projection:room:" + strconv.FormatInt(roomID, 10)
}

func governanceKey(namespace string, roomID, userID int64) string {
	return "live:" + namespace + ":projection:governance:" + strconv.FormatInt(roomID, 10) + ":" + strconv.FormatInt(userID, 10)
}
