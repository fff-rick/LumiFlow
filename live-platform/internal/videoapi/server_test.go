package videoapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/live-platform/internal/auth"
)

type memoryRepository struct {
	items map[int64]Video
	keys  map[string]int64
	next  int64
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{items: map[int64]Video{}, keys: map[string]int64{}, next: 1}
}
func (m *memoryRepository) Create(_ context.Context, v Video, key string) (Video, bool, error) {
	if key != "" {
		if id, ok := m.keys[key]; ok {
			return m.items[id], false, nil
		}
	}
	now := time.Now().UTC()
	v.ID = m.next
	m.next++
	v.CreatedAt = now
	v.UpdatedAt = now
	v.PublishedAt = &now
	m.items[v.ID] = v
	if key != "" {
		m.keys[key] = v.ID
	}
	return v, true, nil
}
func (m *memoryRepository) Get(_ context.Context, id int64) (Video, error) {
	v, ok := m.items[id]
	if !ok || v.Status != 2 {
		return Video{}, ErrNotFound
	}
	return v, nil
}
func (m *memoryRepository) Delete(_ context.Context, id, author int64) error {
	v, ok := m.items[id]
	if !ok || v.Status == 4 {
		return ErrNotFound
	}
	if v.AuthorID != author {
		return ErrForbidden
	}
	v.Status = 4
	m.items[id] = v
	return nil
}
func (m *memoryRepository) ListPublicByAuthor(_ context.Context, author int64, _, _ int) ([]Video, error) {
	out := []Video{}
	for _, v := range m.items {
		if v.AuthorID == author && v.Status == 2 {
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *memoryRepository) ListMine(_ context.Context, author int64, _, _ int) ([]Video, error) {
	return m.ListPublicByAuthor(context.Background(), author, 0, 0)
}

func TestVideoLifecycleUsesLivePlatformJWTAndIdempotency(t *testing.T) {
	tokens := auth.NewTokenManager("test-secret", time.Hour)
	token, _, err := tokens.Issue(7)
	if err != nil {
		t.Fatal(err)
	}
	api := New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Tokens: tokens, Videos: newMemoryRepository()})
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	body := `{"title":"FluxFeed clip","description":"imported","media_url":"https://cdn.example/video.mp4","cover_url":"https://cdn.example/cover.jpg"}`
	for i, want := range []int{http.StatusCreated, http.StatusOK} {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/videos", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", "publish-1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != want {
			t.Fatalf("attempt %d: status=%d", i, res.StatusCode)
		}
		_ = res.Body.Close()
	}
	res, err := http.Get(server.URL + "/api/v1/videos/1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d", res.StatusCode)
	}
	var got Video
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.AuthorID != 7 || got.MediaURL != "https://cdn.example/video.mp4" {
		t.Fatalf("unexpected video: %+v", got)
	}
}

func TestVideoRejectsInvalidMediaURL(t *testing.T) {
	api := New(Deps{Tokens: auth.NewTokenManager("test-secret", time.Hour), Videos: newMemoryRepository()})
	token, _, _ := api.tokens.Issue(1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/videos", strings.NewReader(`{"title":"clip","media_url":"file:///etc/passwd","cover_url":"https://cdn.example/cover.jpg"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

var _ Repository = (*memoryRepository)(nil)
