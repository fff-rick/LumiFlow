package room

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/example/live-platform/internal/idgen"
)

const (
	EventTypeStarted      = "room.started"
	EventTypeStopped      = "room.stopped"
	EventTypeOwnerChanged = "room.owner_changed"
)

type integrationEvent struct {
	EventID    string             `json:"event_id"`
	EventType  string             `json:"event_type"`
	Version    int                `json:"version"`
	OccurredAt time.Time          `json:"occurred_at"`
	Producer   string             `json:"producer"`
	Subject    integrationSubject `json:"subject"`
	Payload    integrationPayload `json:"payload"`
}

type integrationSubject struct {
	RoomID       int64 `json:"room_id"`
	LumiAnchorID int64 `json:"lumi_anchor_id"`
}

type integrationPayload struct {
	Title          string     `json:"title"`
	AnchorNickname string     `json:"anchor_nickname,omitempty"`
	Status         Status     `json:"status"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
}

func newIntegrationEvent(eventType string, value Room, occurredAt time.Time) (string, []byte, error) {
	if (eventType != EventTypeStarted && eventType != EventTypeStopped && eventType != EventTypeOwnerChanged) || value.ID <= 0 || value.AnchorID <= 0 || strings.TrimSpace(value.Title) == "" {
		return "", nil, errors.New("invalid integration.room.v1 event")
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	id := idgen.New()
	event := integrationEvent{
		EventID: id, EventType: eventType, Version: 1, OccurredAt: occurredAt.UTC(), Producer: "lumi-live",
		Subject: integrationSubject{RoomID: value.ID, LumiAnchorID: value.AnchorID},
		Payload: integrationPayload{Title: value.Title, AnchorNickname: value.AnchorNickname, Status: value.Status, StartedAt: value.StartedAt, EndedAt: value.EndedAt},
	}
	payload, err := json.Marshal(event)
	return id, payload, err
}
