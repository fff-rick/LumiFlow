package mq

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type contextKey string

func TestDetachedAsyncContextSurvivesRequestCancellation(t *testing.T) {
	parent := context.WithValue(context.Background(), contextKey("trace-value"), "preserved")
	requestCtx, cancel := context.WithCancel(parent)
	detached := detachedAsyncContext(requestCtx)
	cancel()

	if err := detached.Err(); err != nil {
		t.Fatalf("detached context should not inherit request cancellation: %v", err)
	}
	if got := detached.Value(contextKey("trace-value")); got != "preserved" {
		t.Fatalf("context values must be preserved, got %v", got)
	}
}

type handlerFunc func(context.Context, Record) error

func (f handlerFunc) Handle(ctx context.Context, record Record) error { return f(ctx, record) }

func TestConsumerDeadLettersAfterFiniteTransientRetries(t *testing.T) {
	writer := &recordingDeadLetterWriter{}
	consumer := &Consumer{group: "group", deadLetters: writer, maxRetries: 2, retryBackoff: time.Nanosecond, retryMaxBackoff: time.Nanosecond}
	calls := 0
	result, cause, fatal := consumer.processRecord(context.Background(), handlerFunc(func(context.Context, Record) error {
		calls++
		return errors.New("database unavailable")
	}), Record{Topic: "topic", Partition: 1, Offset: 2, Value: []byte("value")})
	if fatal != nil || result != "dead_lettered" || calls != 3 || writer.calls != 1 {
		t.Fatalf("result=%q calls=%d dlq=%d cause=%v fatal=%v", result, calls, writer.calls, cause, fatal)
	}
	if !strings.Contains(writer.dead.Error, "after 2 retries") {
		t.Fatalf("DLQ error=%q", writer.dead.Error)
	}
}

func TestConsumerRetryDelayIsExponentialAndCapped(t *testing.T) {
	for retry, want := range []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 500 * time.Millisecond} {
		if got := consumerRetryDelay(100*time.Millisecond, 500*time.Millisecond, retry); got != want {
			t.Fatalf("retry %d delay=%s want %s", retry, got, want)
		}
	}
}

func TestConsumerReturnsDLQFailure(t *testing.T) {
	writer := &recordingDeadLetterWriter{err: errors.New("kafka unavailable")}
	consumer := &Consumer{group: "group", deadLetters: writer}
	result, _, fatal := consumer.processRecord(context.Background(), handlerFunc(func(context.Context, Record) error {
		return Permanent(errors.New("invalid payload"))
	}), Record{Topic: "topic", Value: []byte("value")})
	if result != "dlq_error" || fatal == nil || writer.calls != 1 {
		t.Fatalf("result=%q fatal=%v dlq=%d", result, fatal, writer.calls)
	}
}

func TestKafkaProduceErrorReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{context.Canceled, "context_canceled"},
		{context.DeadlineExceeded, "deadline_exceeded"},
		{kgo.ErrMaxBuffered, "max_buffered"},
		{kgo.ErrClientClosed, "client_closed"},
		{errors.New("broker failure"), "broker_or_other"},
	}
	for _, tc := range cases {
		if got := kafkaProduceErrorReason(tc.err); got != tc.want {
			t.Fatalf("reason(%v)=%q want %q", tc.err, got, tc.want)
		}
	}
}
