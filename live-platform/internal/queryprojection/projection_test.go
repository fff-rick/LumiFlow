package queryprojection

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/example/live-platform/internal/danmaku"
	"github.com/example/live-platform/internal/mq"
	"github.com/example/live-platform/internal/projectionevent"
)

type projectorFake struct {
	users    []User
	messages []Message
}

func (p *projectorFake) UpsertUser(_ context.Context, user User) error {
	p.users = append(p.users, user)
	return nil
}

func (p *projectorFake) InsertMessage(_ context.Context, message Message) error {
	p.messages = append(p.messages, message)
	return nil
}

func TestHandlerProjectsSupportedEvents(t *testing.T) {
	now := time.Date(2026, 11, 7, 8, 9, 10, 0, time.UTC)
	topics := Topics{Identity: "identity", Danmaku: "danmaku", Gift: "gift"}
	projector := &projectorFake{}
	handler := NewHandler(projector, topics)

	identityPayload, _ := json.Marshal(projectionevent.IdentityPayload{UserID: 7, Nickname: "viewer", Avatar: "/a.svg", Status: 1})
	identity := mq.Envelope{EventID: "i-1", EventType: projectionevent.IdentityUserUpserted, Version: 1, Producer: projectionevent.IdentityProducer, AggregateType: "IDENTITY", AggregateID: "7", AggregateVersion: 12, CreatedAt: now, Payload: identityPayload}
	danmakuPayload, _ := json.Marshal(danmaku.Event{MessageID: "d-1", RoomID: 9, UserID: 7, Nickname: "viewer", Content: "hello", CreatedAt: now})
	danmakuEvent := mq.Envelope{EventID: "d-1", EventType: mq.EventTypeDanmakuSent, RoomID: 9, CreatedAt: now, Payload: danmakuPayload}
	giftPayload, _ := json.Marshal(map[string]any{"order_no": "G1", "user_id": 7, "anchor_id": 8, "gift_id": 1, "gift_name": "Rose", "count": 2, "unit_price": 100, "total_amount": 200})
	giftEvent := mq.Envelope{EventID: "g-1", EventType: mq.EventTypeGiftSent, RoomID: 9, CreatedAt: now, Payload: giftPayload}

	for _, record := range []mq.Record{
		{Topic: topics.Identity, Value: mustJSON(t, identity)},
		{Topic: topics.Danmaku, Value: mustJSON(t, danmakuEvent)},
		{Topic: topics.Gift, Value: mustJSON(t, giftEvent)},
	} {
		if err := handler.Handle(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	if len(projector.users) != 1 || projector.users[0].AggregateVersion != 12 {
		t.Fatalf("users=%+v", projector.users)
	}
	if len(projector.messages) != 2 || projector.messages[0].MessageID != "d-1" || projector.messages[1].MessageID != "gift:G1" {
		t.Fatalf("messages=%+v", projector.messages)
	}
}

func TestHandlerRejectsTopicContractMismatch(t *testing.T) {
	now := time.Now().UTC()
	payload, _ := json.Marshal(danmaku.Event{MessageID: "d-1", RoomID: 9, UserID: 7, Nickname: "viewer", Content: "hello", CreatedAt: now})
	event := mq.Envelope{EventID: "d-1", EventType: mq.EventTypeDanmakuSent, RoomID: 9, CreatedAt: now, Payload: payload}
	err := NewHandler(&projectorFake{}, Topics{Identity: "identity", Danmaku: "danmaku", Gift: "gift"}).Handle(context.Background(), mq.Record{Topic: "gift", Value: mustJSON(t, event)})
	if !mq.IsPermanent(err) {
		t.Fatalf("err=%v want permanent", err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
