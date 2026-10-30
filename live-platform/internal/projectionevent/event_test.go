package projectionevent

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/example/live-platform/internal/mq"
)

type fakeTx struct {
	calls int
	raw   []byte
}

func (t *fakeTx) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	t.calls++
	if t.calls == 2 {
		t.raw = append([]byte(nil), args[0].([]byte)...)
	}
	return fakeResult(42), nil
}

type fakeResult int64

func (r fakeResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r fakeResult) RowsAffected() (int64, error) { return 1, nil }

func TestEnqueueUsesOutboxIDAsAggregateVersion(t *testing.T) {
	tx := &fakeTx{}
	if err := Enqueue(context.Background(), tx, "live.room.v1", "ROOM", "7", RoomUpserted, "7", RoomPayload{RoomID: 7, AnchorID: 2, Title: "room", Status: "LIVING"}); err != nil {
		t.Fatal(err)
	}
	var event mq.Envelope
	if err := json.Unmarshal(tx.raw, &event); err != nil {
		t.Fatal(err)
	}
	if tx.calls != 2 || event.AggregateVersion != 42 || event.AggregateID != "7" || event.Producer != Producer {
		t.Fatalf("calls=%d event=%+v", tx.calls, event)
	}
}
