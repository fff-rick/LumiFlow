// Package videoapi owns the short-video catalogue imported from FluxFeed.
// Media bytes stay in object storage; this service owns only video metadata.
package videoapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/observability"
)

var (
	ErrNotFound  = errors.New("video not found")
	ErrForbidden = errors.New("video does not belong to user")
)

type Video struct {
	ID            int64      `json:"id"`
	AuthorID      int64      `json:"author_id"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	MediaURL      string     `json:"media_url"`
	CoverURL      string     `json:"cover_url"`
	Status        int        `json:"status"`
	StatusName    string     `json:"status_name"`
	LikeCount     int64      `json:"like_count"`
	CommentCount  int64      `json:"comment_count"`
	FavoriteCount int64      `json:"favorite_count"`
	PublishedAt   *time.Time `json:"published_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Repository interface {
	Create(context.Context, Video, string) (Video, bool, error)
	Get(context.Context, int64) (Video, error)
	Delete(context.Context, int64, int64) error
	ListPublicByAuthor(context.Context, int64, int, int) ([]Video, error)
	ListMine(context.Context, int64, int, int) ([]Video, error)
}

type Pinger interface{ Ping(context.Context) error }

type Deps struct {
	Log     *slog.Logger
	Tokens  *auth.TokenManager
	Videos  Repository
	MySQL   Pinger
	Metrics *observability.Metrics
}

type Server struct {
	log     *slog.Logger
	tokens  *auth.TokenManager
	videos  Repository
	mysql   Pinger
	metrics *observability.Metrics
	mux     *http.ServeMux
}

func New(d Deps) *Server {
	s := &Server{log: d.Log, tokens: d.Tokens, videos: d.Videos, mysql: d.MySQL, metrics: d.Metrics, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "live-video"})
	})
	s.mux.HandleFunc("GET /ready", s.ready)
	if s.metrics != nil {
		s.mux.Handle("GET /metrics", s.metrics.Handler())
	}
	s.mux.HandleFunc("POST /api/v1/videos", s.requireAuth(s.create))
	s.mux.HandleFunc("GET /api/v1/videos/{video_id}", s.get)
	s.mux.HandleFunc("DELETE /api/v1/videos/{video_id}", s.requireAuth(s.delete))
	s.mux.HandleFunc("GET /api/v1/users/me/videos", s.requireAuth(s.listMine))
	s.mux.HandleFunc("GET /api/v1/users/{user_id}/videos", s.listByAuthor)
	return s
}

func (s *Server) Handler() http.Handler {
	base := recoverer(s.log, s.mux)
	if s.metrics != nil {
		base = s.metrics.HTTPMiddleware("live-video", base)
	}
	return observability.TraceHTTP(base)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if s.mysql != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
		defer cancel()
		if s.mysql.Ping(ctx) != nil {
			writeError(w, http.StatusServiceUnavailable, "NOT_READY", "mysql unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "service": "live-video"})
}

type createRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	MediaURL    string `json:"media_url"`
	CoverURL    string `json:"cover_url"`
}

func (s *Server) create(w http.ResponseWriter, r *http.Request, userID int64) {
	var in createRequest
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	video := Video{AuthorID: userID, Title: strings.TrimSpace(in.Title), Description: strings.TrimSpace(in.Description), MediaURL: strings.TrimSpace(in.MediaURL), CoverURL: strings.TrimSpace(in.CoverURL), Status: 2, StatusName: "published"}
	if err := validate(video); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_VIDEO", err.Error())
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) > 128 {
		writeError(w, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be at most 128 characters")
		return
	}
	result, created, err := s.videos.Create(r.Context(), video, key)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if created {
		writeJSON(w, http.StatusCreated, result)
	} else {
		writeJSON(w, http.StatusOK, result)
	}
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "video_id")
	if !ok {
		return
	}
	video, err := s.videos.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "VIDEO_NOT_FOUND", "video not found")
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, video)
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request, userID int64) {
	id, ok := pathID(w, r, "video_id")
	if !ok {
		return
	}
	err := s.videos.Delete(r.Context(), id, userID)
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "VIDEO_NOT_FOUND", "video not found")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "video does not belong to user")
	case err != nil:
		s.internalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) listMine(w http.ResponseWriter, r *http.Request, userID int64) {
	s.list(w, r, userID, true)
}
func (s *Server) listByAuthor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "user_id")
	if ok {
		s.list(w, r, id, false)
	}
}
func (s *Server) list(w http.ResponseWriter, r *http.Request, authorID int64, mine bool) {
	limit, offset, err := pagination(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	var videos []Video
	if mine {
		videos, err = s.videos.ListMine(r.Context(), authorID, limit, offset)
	} else {
		videos, err = s.videos.ListPublicByAuthor(r.Context(), authorID, limit, offset)
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": videos, "limit": limit, "offset": offset})
}

type authedHandler func(http.ResponseWriter, *http.Request, int64)

func (s *Server) requireAuth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(h, "Bearer ") {
			writeError(w, 401, "UNAUTHORIZED", "missing bearer token")
			return
		}
		if s.tokens == nil {
			writeError(w, 500, "INTERNAL_ERROR", "token verifier unavailable")
			return
		}
		claims, err := s.tokens.Verify(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
		if err != nil {
			writeError(w, 401, "UNAUTHORIZED", err.Error())
			return
		}
		next(w, r, claims.UserID)
	}
}

func validate(v Video) error {
	if v.Title == "" || len(v.Title) > 128 {
		return errors.New("title must contain 1 to 128 characters")
	}
	if len(v.Description) > 512 {
		return errors.New("description must be at most 512 characters")
	}
	if !validURL(v.MediaURL) || !validURL(v.CoverURL) {
		return errors.New("media_url and cover_url must be absolute http or https URLs")
	}
	return nil
}
func validURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(raw) <= 512
}
func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 400, "BAD_REQUEST", name+" must be a positive integer")
		return 0, false
	}
	return id, true
}
func pagination(r *http.Request) (int, int, error) {
	limit, offset := 20, 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return 0, 0, errors.New("limit must be between 1 and 100")
		}
		limit = n
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, errors.New("offset must be non-negative")
		}
		offset = n
	}
	return limit, offset, nil
}
func decodeJSON(r *http.Request, dst any) error {
	body := io.NopCloser(io.LimitReader(r.Body, 16<<10+1))
	defer body.Close()
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return errors.New("invalid JSON request")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
func (s *Server) internalError(w http.ResponseWriter, err error) {
	if s.log != nil {
		s.log.Error("video service failure", "error", err)
	}
	writeError(w, 500, "INTERNAL_ERROR", "video service failure")
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				if log != nil {
					log.Error("video service panic")
				}
				writeError(w, 500, "INTERNAL_ERROR", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
