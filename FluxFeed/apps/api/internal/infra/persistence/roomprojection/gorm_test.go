package roomprojection

import (
	applicationeventbus "FluxFeed/internal/application/eventbus"
	"context"
	"encoding/json"
	"testing"
)

type storeFake struct{ value Model }

func (s *storeFake) Apply(_ context.Context, value Model) error { s.value = value; return nil }

func TestHandlerProjectsStartedRoom(t *testing.T) {
	raw := []byte(`{"event_id":"room-event-1","event_type":"room.started","version":1,"occurred_at":"2026-01-02T03:04:05Z","producer":"lumi-live","subject":{"room_id":84,"lumi_anchor_id":42},"payload":{"title":"live","anchor_nickname":"anchor","status":"LIVING","started_at":"2026-01-02T03:04:05Z"}}`)
	var event applicationeventbus.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	store := &storeFake{}
	if err := NewHandler(store).Handle(context.Background(), &event); err != nil {
		t.Fatal(err)
	}
	if store.value.RoomID != 84 || store.value.LumiAnchorID != 42 || store.value.Status != "LIVING" || store.value.StartedAt == nil {
		t.Fatalf("unexpected projection: %+v", store.value)
	}
}
