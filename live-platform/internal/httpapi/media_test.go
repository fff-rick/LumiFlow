package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/live-platform/internal/media"
)

type fakeMedia struct{ callbacks int }

func (*fakeMedia) IssueCredentials(context.Context, int64, int64) (media.Credentials, error) {
	return media.Credentials{}, nil
}
func (*fakeMedia) Playback(context.Context, int64) (media.Playback, error) {
	return media.Playback{}, nil
}
func (*fakeMedia) RequirePublishing(context.Context, int64) error { return nil }
func (f *fakeMedia) HandleSRSCallback(context.Context, media.SRSCallback) error {
	f.callbacks++
	return nil
}

func TestSRSCallbackRequiresToken(t *testing.T) {
	service := &fakeMedia{}
	handler := NewIdentityRoom(Deps{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), MediaCallbackToken: "secret", Media: service,
	})

	request := httptest.NewRequest(http.MethodPost, "/internal/v1/media/srs/callback?token=wrong", strings.NewReader(`{"action":"on_publish","app":"live","stream":"room-1"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || service.callbacks != 0 {
		t.Fatalf("status=%d callbacks=%d", response.Code, service.callbacks)
	}

	request = httptest.NewRequest(http.MethodPost, "/internal/v1/media/srs/callback?token=secret", strings.NewReader(`{"action":"on_publish","app":"live","stream":"room-1"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"code\":0}\n" || service.callbacks != 1 {
		t.Fatalf("status=%d body=%q callbacks=%d", response.Code, response.Body.String(), service.callbacks)
	}
}
