package applicationeventbus

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestIdentityEventUsesVersionedEnvelope(t *testing.T) {
	event, err := NewIdentityEvent(TypeIdentityProfileChanged, 42, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), IdentityPayload{Account: "a@example.test", Nickname: "A"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		Version   int    `json:"version"`
		Producer  string `json:"producer"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.EventID == "" || wire.EventType != TypeIdentityProfileChanged || wire.Version != 1 || wire.Producer != "fluxfeed" {
		t.Fatalf("unexpected integration envelope: %s", raw)
	}
	var decoded Event
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != event.ID || decoded.Type != event.Type || decoded.UserID != 42 {
		t.Fatalf("outbox round trip lost event metadata: %+v", decoded)
	}
}

func TestVideoEventUsesVersionedEnvelope(t *testing.T) {
	publishedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	event, err := NewIntegrationVideoEvent(TypeIntegrationVideoPublished, 42, 84, publishedAt, VideoPayload{Title: "video", MediaURL: "https://media", PublishedAt: publishedAt, Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var wire VideoEnvelope
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.EventID == "" || wire.EventType != TypeIntegrationVideoPublished || wire.Version != 1 || wire.Producer != "fluxfeed" || wire.Subject.FluxVideoID != 84 {
		t.Fatalf("unexpected integration envelope: %s", raw)
	}
	var decoded Event
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != event.ID || decoded.Type != event.Type || decoded.UserID != 42 || decoded.VideoID != 84 {
		t.Fatalf("outbox round trip lost event metadata: %+v", decoded)
	}
}

func TestRoomEventDecodesVersionedEnvelope(t *testing.T) {
	raw := []byte(`{"event_id":"room-event-1","event_type":"room.started","version":1,"occurred_at":"2026-01-02T03:04:05Z","producer":"lumi-live","subject":{"room_id":84,"lumi_anchor_id":42},"payload":{"title":"live","status":"LIVING","started_at":"2026-01-02T03:04:05Z"}}`)
	var event Event
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if event.ID != "room-event-1" || event.Type != TypeIntegrationRoomStarted || event.RoomID != 84 || event.UserID != 42 {
		t.Fatalf("room envelope metadata lost: %+v", event)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(raw) {
		t.Fatalf("room envelope round trip changed: %s", encoded)
	}
}

func TestRoomEventRejectsWrongProducer(t *testing.T) {
	raw := []byte(`{"event_id":"room-event-1","event_type":"room.started","version":1,"occurred_at":"2026-01-02T03:04:05Z","producer":"other","subject":{"room_id":84,"lumi_anchor_id":42},"payload":{"title":"live","status":"LIVING"}}`)
	var event Event
	if err := json.Unmarshal(raw, &event); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("expected invalid event, got %v", err)
	}
}
