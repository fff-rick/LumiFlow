package applicationeventbus

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TypeVideoPublished                    = "VideoPublished"
	TypeVideoExposed                      = "VideoExposed"
	TypeVideoViewed                       = "VideoViewed"
	TypeVideoLiked                        = "VideoLiked"
	TypeVideoUnliked                      = "VideoUnliked"
	TypeVideoCollected                    = "VideoCollected"
	TypeVideoUncollected                  = "VideoUncollected"
	TypeVideoCommented                    = "VideoCommented"
	TypeUserFollowed                      = "UserFollowed"
	TypeUserUnfollowed                    = "UserUnfollowed"
	TypeContentReviewRequested            = "ContentReviewRequested"
	TypeContentVisibilityChanged          = "ContentVisibilityChanged"
	TypeIdentityProfileChanged            = "user.profile_changed"
	TypeIdentityDisabled                  = "user.disabled"
	TypeIntegrationVideoPublished         = "video.published"
	TypeIntegrationVideoVisibilityChanged = "video.visibility_changed"
	TypeIntegrationRoomStarted            = "room.started"
	TypeIntegrationRoomStopped            = "room.stopped"
	TypeIntegrationRoomOwnerChanged       = "room.owner_changed"
)

var ErrInvalidEvent = errors.New("invalid event")

// Event 是跨消息中间件的统一信封，Payload 保留各业务事件的完整字段。
type Event struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	UserID      int64           `json:"user_id,omitempty"`
	VideoID     int64           `json:"video_id,omitempty"`
	RoomID      int64           `json:"room_id,omitempty"`
	Timestamp   int64           `json:"timestamp"`
	Payload     json.RawMessage `json:"payload"`
	identity    *IdentityEnvelope
	integration json.RawMessage
	room        *RoomEnvelope
}

// IdentityEnvelope is the versioned contract shared with LUMI. It intentionally
// keeps identity events separate from FluxFeed's legacy feed-event envelope.
type IdentityEnvelope struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	Version    int             `json:"version"`
	OccurredAt time.Time       `json:"occurred_at"`
	Producer   string          `json:"producer"`
	Subject    IdentitySubject `json:"subject"`
	Payload    json.RawMessage `json:"payload"`
}

type IdentitySubject struct {
	FluxFeedUserID int64 `json:"fluxfeed_user_id"`
}

type VideoEnvelope struct {
	EventID    string       `json:"event_id"`
	EventType  string       `json:"event_type"`
	Version    int          `json:"version"`
	OccurredAt time.Time    `json:"occurred_at"`
	Producer   string       `json:"producer"`
	Subject    VideoSubject `json:"subject"`
	Payload    VideoPayload `json:"payload"`
}
type VideoSubject struct {
	FluxFeedUserID int64 `json:"fluxfeed_user_id"`
	FluxVideoID    int64 `json:"flux_video_id"`
}
type VideoPayload struct {
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	MediaURL    string    `json:"media_url,omitempty"`
	CoverURL    string    `json:"cover_url,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	Visible     bool      `json:"visible"`
}

type RoomEnvelope struct {
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	Version    int             `json:"version"`
	OccurredAt time.Time       `json:"occurred_at"`
	Producer   string          `json:"producer"`
	Subject    RoomSubject     `json:"subject"`
	Payload    json.RawMessage `json:"payload"`
}

type RoomSubject struct {
	RoomID       int64 `json:"room_id"`
	LumiAnchorID int64 `json:"lumi_anchor_id"`
}

type IdentityPayload struct {
	Account   string `json:"account"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

type Bus interface {
	Publish(ctx context.Context, topic string, event *Event) error
	Subscribe(ctx context.Context, topic string, group string, handler func(context.Context, *Event) error) error
	Close() error
}

// ConsumerDeduplicator 记录消费组已经成功处理的事件，避免 Kafka 重投时重复执行业务逻辑。
type ConsumerDeduplicator interface {
	IsProcessed(ctx context.Context, group string, eventID string) (bool, error)
	MarkProcessed(ctx context.Context, group string, eventID string) error
}

func New(id string, eventType string, userID int64, videoID int64, occurredAt time.Time, payload any) (*Event, error) {
	id = strings.TrimSpace(id)
	eventType = strings.TrimSpace(eventType)
	if id == "" || eventType == "" {
		return nil, ErrInvalidEvent
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	return &Event{
		ID:        id,
		Type:      eventType,
		UserID:    userID,
		VideoID:   videoID,
		Timestamp: occurredAt.UTC().UnixMilli(),
		Payload:   content,
	}, nil
}

func (e *Event) Decode(target any) error {
	if e == nil || len(e.Payload) == 0 || target == nil {
		return ErrInvalidEvent
	}
	return json.Unmarshal(e.Payload, target)
}

// NewIdentityEvent creates the v1 integration contract while retaining Event
// as the common Outbox transport type.
func NewIdentityEvent(eventType string, userID int64, occurredAt time.Time, payload IdentityPayload) (*Event, error) {
	if (eventType != TypeIdentityProfileChanged && eventType != TypeIdentityDisabled) || userID <= 0 || strings.TrimSpace(payload.Nickname) == "" {
		return nil, ErrInvalidEvent
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	id, err := eventID()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	envelope := &IdentityEnvelope{EventID: id, EventType: eventType, Version: 1, OccurredAt: occurredAt.UTC(), Producer: "fluxfeed", Subject: IdentitySubject{FluxFeedUserID: userID}, Payload: body}
	return &Event{ID: id, Type: eventType, UserID: userID, Timestamp: occurredAt.UTC().UnixMilli(), Payload: body, identity: envelope}, nil
}

func NewIntegrationVideoEvent(eventType string, userID, videoID int64, occurredAt time.Time, payload VideoPayload) (*Event, error) {
	if (eventType != TypeIntegrationVideoPublished && eventType != TypeIntegrationVideoVisibilityChanged) || userID <= 0 || videoID <= 0 || (eventType == TypeIntegrationVideoPublished && (strings.TrimSpace(payload.Title) == "" || strings.TrimSpace(payload.MediaURL) == "")) {
		return nil, ErrInvalidEvent
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	id, err := eventID()
	if err != nil {
		return nil, err
	}
	envelope := VideoEnvelope{EventID: id, EventType: eventType, Version: 1, OccurredAt: occurredAt.UTC(), Producer: "fluxfeed", Subject: VideoSubject{FluxFeedUserID: userID, FluxVideoID: videoID}, Payload: payload}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return &Event{ID: id, Type: eventType, UserID: userID, VideoID: videoID, Timestamp: occurredAt.UTC().UnixMilli(), integration: raw}, nil
}

func (e Event) MarshalJSON() ([]byte, error) {
	if e.identity != nil {
		return json.Marshal(e.identity)
	}
	if e.integration != nil {
		return e.integration, nil
	}
	if e.room != nil {
		return json.Marshal(e.room)
	}
	type wire Event
	return json.Marshal(wire(e))
}

func (e *Event) UnmarshalJSON(data []byte) error {
	var identity IdentityEnvelope
	if err := json.Unmarshal(data, &identity); err != nil {
		return err
	}
	if identity.EventType == TypeIdentityProfileChanged || identity.EventType == TypeIdentityDisabled {
		e.ID, e.Type, e.UserID = identity.EventID, identity.EventType, identity.Subject.FluxFeedUserID
		e.Timestamp, e.Payload, e.identity = identity.OccurredAt.UnixMilli(), identity.Payload, &identity
		return nil
	}
	var video VideoEnvelope
	if err := json.Unmarshal(data, &video); err != nil {
		return err
	}
	if video.EventType == TypeIntegrationVideoPublished || video.EventType == TypeIntegrationVideoVisibilityChanged {
		e.ID, e.Type, e.UserID, e.VideoID = video.EventID, video.EventType, video.Subject.FluxFeedUserID, video.Subject.FluxVideoID
		e.Timestamp, e.integration = video.OccurredAt.UnixMilli(), append(e.integration[:0], data...)
		return nil
	}
	var room RoomEnvelope
	if err := json.Unmarshal(data, &room); err != nil {
		return err
	}
	if room.EventType == TypeIntegrationRoomStarted || room.EventType == TypeIntegrationRoomStopped || room.EventType == TypeIntegrationRoomOwnerChanged {
		if room.EventID == "" || room.Version != 1 || room.Producer != "lumi-live" || room.OccurredAt.IsZero() || room.Subject.RoomID <= 0 || room.Subject.LumiAnchorID <= 0 || len(room.Payload) == 0 {
			return ErrInvalidEvent
		}
		e.ID, e.Type, e.UserID, e.RoomID = room.EventID, room.EventType, room.Subject.LumiAnchorID, room.Subject.RoomID
		e.Timestamp, e.Payload, e.room = room.OccurredAt.UnixMilli(), room.Payload, &room
		return nil
	}
	type wire Event
	var legacy wire
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	*e = Event(legacy)
	return nil
}

func eventID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate integration event id: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
}
