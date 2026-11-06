package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type readyPinger struct{ called bool }

func (p *readyPinger) Ping(context.Context) error {
	p.called = true
	return nil
}

func TestInteractionReadyWithoutMySQL(t *testing.T) {
	redis := &readyPinger{}
	handler := NewInteraction(Deps{
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Redis: redis,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if response.Code != http.StatusOK || !redis.called {
		t.Fatalf("status=%d redis_checked=%t body=%q", response.Code, redis.called, response.Body.String())
	}
}
