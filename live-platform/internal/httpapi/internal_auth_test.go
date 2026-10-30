package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInternalRoutesRequireServiceToken(t *testing.T) {
	h := NewIdentityRoom(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), InternalToken: "secret"})
	for _, test := range []struct{ method, path string }{
		{http.MethodPost, "/internal/v1/users/fluxfeed-sync"},
		{http.MethodGet, "/internal/v1/rooms/1/access/2"},
		{http.MethodGet, "/internal/v1/rooms/1"},
		{http.MethodGet, "/internal/v1/users/2"},
		{http.MethodPost, "/internal/v1/rooms/1/join/2"},
	} {
		req := httptest.NewRequest(test.method, test.path, nil)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d want %d", test.method, test.path, res.Code, http.StatusUnauthorized)
		}
	}
}

func TestRequireInternalAcceptsServiceToken(t *testing.T) {
	s := &Server{internalToken: "secret"}
	h := s.requireInternal(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/users/1", nil)
	req.Header.Set("X-Internal-Token", "secret")
	res := httptest.NewRecorder()
	h(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("status=%d want %d", res.Code, http.StatusNoContent)
	}
}
