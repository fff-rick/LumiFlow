package room

import (
	"encoding/json"
	"testing"
	"time"
)

func TestIntegrationEventUsesVersionedEnvelope(t *testing.T) {
	startedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	id, raw, err := newIntegrationEvent(EventTypeStarted, Room{ID: 3, AnchorID: 4, AnchorNickname: "anchor", Title: "live", Status: StatusLiving, StartedAt: &startedAt}, startedAt)
	if err != nil {
		t.Fatal(err)
	}
	var event integrationEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if id == "" || event.EventID != id || event.Version != 1 || event.Producer != "lumi-live" || event.Subject.RoomID != 3 || event.Payload.Status != StatusLiving {
		t.Fatalf("unexpected room event: %s", raw)
	}
}
