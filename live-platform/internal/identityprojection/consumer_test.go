package identityprojection

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/example/live-platform/internal/mq"
)

type dedupStub struct{ begun, done int }

func (d *dedupStub) Begin(context.Context, string, string, time.Duration) (bool, error) {
	d.begun++
	return false, nil
}
func (d *dedupStub) Done(context.Context, string, string) error        { d.done++; return nil }
func (d *dedupStub) Fail(context.Context, string, string, error) error { return nil }

type projectorStub struct{ user Projection }

func (p *projectorStub) SyncFluxFeed(_ context.Context, user Projection) error {
	p.user = user
	return nil
}

func TestDisabledEventProjectsDisabledUser(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"event_id": "evt-1", "event_type": "user.disabled", "version": 1, "occurred_at": time.Now().UTC(), "producer": "fluxfeed",
		"subject": map[string]any{"fluxfeed_user_id": 42}, "payload": map[string]any{"account": "a@example.test", "nickname": "A", "avatar_url": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	dedup, projector := &dedupStub{}, &projectorStub{}
	if err := NewHandler("lumi-identity-projection-v1", dedup, projector, time.Minute).Handle(context.Background(), mq.Record{Value: payload}); err != nil {
		t.Fatal(err)
	}
	if projector.user.Enabled || projector.user.ID != 42 {
		t.Fatalf("unexpected projection: %+v", projector.user)
	}
	if dedup.begun != 1 || dedup.done != 1 {
		t.Fatalf("unexpected dedup calls: %+v", dedup)
	}
}

func TestMalformedEventIsPermanent(t *testing.T) {
	err := NewHandler("group", &dedupStub{}, &projectorStub{}, time.Minute).Handle(context.Background(), mq.Record{Value: []byte(`{"event_id":""}`)})
	if !mq.IsPermanent(err) {
		t.Fatalf("expected permanent error, got %v", err)
	}
}
