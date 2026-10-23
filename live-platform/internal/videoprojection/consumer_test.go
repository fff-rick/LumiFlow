package videoprojection

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/example/live-platform/internal/mq"
)

type dedupFake struct {
	begin int
	done  bool
}

func (d *dedupFake) Begin(context.Context, string, string, time.Duration) (bool, error) {
	d.begin++
	return false, nil
}
func (d *dedupFake) Done(context.Context, string, string) error        { d.done = true; return nil }
func (d *dedupFake) Fail(context.Context, string, string, error) error { return nil }

type dbFake struct{ calls int }

func (d *dbFake) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	d.calls++
	return nil, nil
}
func TestPublishedEventProjectsVideo(t *testing.T) {
	db, dedup := &dbFake{}, &dedupFake{}
	record := mq.Record{Value: []byte(`{"event_id":"evt-1","event_type":"video.published","version":1,"occurred_at":"2026-01-02T03:04:05Z","producer":"fluxfeed","subject":{"fluxfeed_user_id":2,"flux_video_id":3},"payload":{"title":"video","media_url":"https://media","published_at":"2026-01-02T03:04:05Z","visible":true}}`)}
	if err := NewHandler("group", dedup, db, time.Minute).Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if db.calls != 1 || !dedup.done {
		t.Fatalf("projection calls=%d done=%v", db.calls, dedup.done)
	}
}

func TestMalformedPublishedEventDoesNotClaimDedupLease(t *testing.T) {
	db, dedup := &dbFake{}, &dedupFake{}
	record := mq.Record{Value: []byte(`{"event_id":"evt-1","event_type":"video.published","version":1,"occurred_at":"2026-01-02T03:04:05Z","producer":"fluxfeed","subject":{"fluxfeed_user_id":2,"flux_video_id":3},"payload":{"title":"video","visible":true}}`)}
	err := NewHandler("group", dedup, db, time.Minute).Handle(context.Background(), record)
	if !mq.IsPermanent(err) || dedup.begin != 0 || db.calls != 0 {
		t.Fatalf("err=%v begin=%d projection calls=%d", err, dedup.begin, db.calls)
	}
}
