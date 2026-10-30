package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/example/live-platform/internal/idgen"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/webui"
	"go.opentelemetry.io/otel/trace"
)

type Deps struct {
	Log             *slog.Logger
	Metrics         *observability.Metrics
	CommerceURL     string
	InteractionURL  string
	IdentityRoomURL string
	QueryURL        string
	FluxFeedURL     string
	InternalToken   string
	BridgeJWTSecret string
	BridgeTokenTTL  time.Duration
}

type Server struct {
	log     *slog.Logger
	metrics *observability.Metrics
	mux     *http.ServeMux
	bridge  *fluxFeedBridge
}

func New(d Deps) (*Server, error) {
	if d.Log == nil {
		return nil, errors.New("gateway logger is required")
	}
	s := &Server{log: d.Log, metrics: d.Metrics, mux: http.NewServeMux()}
	bridge, err := newFluxFeedBridge(d)
	if err != nil {
		return nil, err
	}
	s.bridge = bridge
	identity, err := s.proxy("identity-room", d.IdentityRoomURL, "IDENTITY_ROOM_UNAVAILABLE")
	if err != nil {
		return nil, err
	}
	interaction, err := s.proxy("interaction", d.InteractionURL, "INTERACTION_UNAVAILABLE")
	if err != nil {
		return nil, err
	}
	commerce, err := s.proxy("commerce", d.CommerceURL, "COMMERCE_UNAVAILABLE")
	if err != nil {
		return nil, err
	}
	query, err := s.proxy("query", d.QueryURL, "QUERY_UNAVAILABLE")
	if err != nil {
		return nil, err
	}
	fluxContent, err := s.fluxContentProxy(d.FluxFeedURL)
	if err != nil {
		return nil, err
	}
	fluxAuth, err := s.fluxAuthProxy(d.FluxFeedURL)
	if err != nil {
		return nil, err
	}
	s.routes(identity, interaction, commerce, query, fluxContent, fluxAuth)
	return s, nil
}

// fluxAuthProxy keeps FluxFeed as the owner of password and OAuth sessions.
// The Next BFF forwards its Set-Cookie headers to the browser so OAuth state
// remains bound to the public LumiFlow origin.
func (s *Server) fluxAuthProxy(rawURL string) (http.Handler, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("invalid fluxfeed upstream URL %q", rawURL)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		path := r.URL.Path
		director(r)
		switch {
		case strings.HasPrefix(path, "/api/v1/auth/"):
			r.URL.Path = "/api/auth/" + strings.TrimPrefix(path, "/api/v1/auth/")
		case strings.HasPrefix(path, "/api/v1/oauth/"):
			r.URL.Path = "/api/oauth/" + strings.TrimPrefix(path, "/api/v1/oauth/")
		}
		r.URL.RawPath = ""
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		s.log.ErrorContext(r.Context(), "upstream proxy failed", "upstream", "fluxfeed-auth", "error", err)
		writeError(w, http.StatusBadGateway, "FLUXFEED_UNAVAILABLE", "authentication service unavailable")
	}
	return proxy, nil
}

func (s *Server) proxy(name, rawURL, errorCode string) (http.Handler, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("invalid %s upstream URL %q", name, rawURL)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Header.Del("X-User-ID")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		s.log.ErrorContext(r.Context(), "upstream proxy failed", "upstream", name, "error", err)
		writeError(w, http.StatusBadGateway, errorCode, name+" service unavailable")
	}
	return proxy, nil
}

// fluxContentProxy exposes FluxFeed as LUMI's video microservice. It
// deliberately preserves the FluxFeed access token: replacing it with the
// internal live token would make FluxFeed reject video requests.
func (s *Server) fluxContentProxy(rawURL string) (http.Handler, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("invalid fluxfeed upstream URL %q", rawURL)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		path := r.URL.Path
		director(r)
		switch {
		case path == "/api/v1/content-assets":
			// Upload stays behind LUMI's public API, but FluxFeed remains the
			// only owner of media-asset validation and storage metadata.
			r.URL.Path = "/api/uploads"
		case strings.HasPrefix(path, "/api/v1/content-assets/"):
			r.URL.Path = "/uploads/" + strings.TrimPrefix(path, "/api/v1/content-assets/")
		default:
			r.URL.Path = "/api" + strings.TrimPrefix(path, "/api/v1")
		}
		r.URL.RawPath = ""
		r.Header.Del("X-User-ID")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		s.log.ErrorContext(r.Context(), "upstream proxy failed", "upstream", "fluxfeed-content", "error", err)
		writeError(w, http.StatusBadGateway, "FLUXFEED_UNAVAILABLE", "video content service unavailable")
	}
	return proxy, nil
}

func (s *Server) routes(identity, interaction, commerce, query, fluxContent, fluxAuth http.Handler) {
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "milestone": "M8"})
	})
	s.mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
	})
	if s.metrics != nil {
		s.mux.Handle("GET /metrics", s.metrics.Handler())
	}

	s.mux.Handle("POST /api/v1/auth/register", s.authEndpoint(true))
	s.mux.Handle("POST /api/v1/auth/login", s.authEndpoint(false))
	s.mux.Handle("GET /api/v1/auth/capabilities", fluxAuth)
	s.mux.Handle("POST /api/v1/auth/oauth/sessions", fluxAuth)
	s.mux.Handle("GET /api/v1/oauth/{provider}", fluxAuth)
	s.mux.Handle("GET /api/v1/oauth/{provider}/callback", fluxAuth)
	s.mux.Handle("GET /api/v1/me", s.protected(identity))
	s.mux.Handle("POST /api/v1/realtime/token", s.protected(identity))
	s.mux.Handle("POST /api/v1/rooms", s.protected(identity))
	s.mux.Handle("GET /api/v1/rooms", identity)
	s.mux.Handle("GET /api/v1/rooms/{room_id}", identity)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/start", s.protected(identity))
	s.mux.Handle("POST /api/v1/rooms/{room_id}/stop", s.protected(identity))
	s.mux.Handle("POST /api/v1/rooms/{room_id}/mutes", s.protected(identity))
	s.mux.Handle("GET /api/v1/rooms/{room_id}/mutes", s.protected(identity))
	s.mux.Handle("DELETE /api/v1/rooms/{room_id}/mutes/{user_id}", s.protected(identity))
	s.mux.Handle("POST /api/v1/rooms/{room_id}/bans", s.protected(identity))
	s.mux.Handle("GET /api/v1/rooms/{room_id}/bans", s.protected(identity))
	s.mux.Handle("DELETE /api/v1/rooms/{room_id}/bans/{user_id}", s.protected(identity))

	s.mux.Handle("POST /api/v1/rooms/{room_id}/join", s.protected(interaction))
	s.mux.Handle("POST /api/v1/rooms/{room_id}/leave", s.protected(interaction))
	s.mux.Handle("POST /api/v1/rooms/{room_id}/danmaku", s.protected(interaction))
	s.mux.Handle("POST /api/v1/rooms/{room_id}/like", s.protected(interaction))
	s.mux.Handle("POST /api/v1/rooms/{room_id}/heartbeat", s.protected(interaction))

	s.mux.Handle("GET /api/v1/gifts", commerce)
	s.mux.Handle("GET /api/v1/wallet", s.protected(commerce))
	s.mux.Handle("GET /api/v1/wallet/transactions", s.protected(commerce))
	s.mux.Handle("POST /api/v1/wallet/dev-credit", s.protected(commerce))
	s.mux.Handle("POST /api/v1/rooms/{room_id}/gifts", s.protected(commerce))
	s.mux.Handle("GET /api/v1/gift-orders/{order_no}", s.protected(commerce))

	s.mux.Handle("GET /api/v1/rooms/{room_id}/messages", query)
	s.mux.Handle("GET /api/v1/rooms/{room_id}/top-viewers", query)
	s.mux.Handle("GET /api/v1/rooms/{room_id}/viewers", query)
	s.mux.Handle("GET /api/v1/rooms/{room_id}/stats", query)

	s.mux.Handle("POST /api/v1/content-assets", fluxContent)
	s.mux.Handle("GET /api/v1/feed-items", fluxContent)
	s.mux.Handle("PUT /api/v1/videos/{video_id}/like", fluxContent)
	s.mux.Handle("DELETE /api/v1/videos/{video_id}/like", fluxContent)
	s.mux.Handle("POST /api/v1/videos/{video_id}/comments", fluxContent)
	s.mux.Handle("GET /api/v1/videos/{video_id}/comments", fluxContent)
	s.mux.Handle("DELETE /api/v1/comments/{comment_id}", fluxContent)
	s.mux.Handle("GET /api/v1/content-assets/{asset...}", fluxContent)
	s.mux.Handle("POST /api/v1/videos", fluxContent)
	s.mux.Handle("GET /api/v1/videos/{video_id}", fluxContent)
	s.mux.Handle("DELETE /api/v1/videos/{video_id}", fluxContent)
	s.mux.Handle("GET /api/v1/users/me/videos", fluxContent)
	s.mux.Handle("GET /api/v1/users/me/following", fluxContent)
	s.mux.Handle("GET /api/v1/users/me/followers", fluxContent)
	s.mux.Handle("PUT /api/v1/users/me/following/{target_user_id}", fluxContent)
	s.mux.Handle("DELETE /api/v1/users/me/following/{target_user_id}", fluxContent)
	s.mux.Handle("GET /api/v1/users/{user_id}", fluxContent)
	s.mux.Handle("GET /api/v1/users/{user_id}/videos", fluxContent)

	for _, pattern := range []string{"GET /internal/", "POST /internal/", "PUT /internal/", "PATCH /internal/", "DELETE /internal/"} {
		s.mux.HandleFunc(pattern, http.NotFound)
	}
	s.mux.Handle("GET /", webui.Handler())
}

func (s *Server) authEndpoint(register bool) http.Handler {
	if register {
		return http.HandlerFunc(s.register)
	}
	return http.HandlerFunc(s.login)
}

type registerRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

type loginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if err := decodeAuthJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	response, err := s.bridge.register(r.Context(), request.Account, request.Password, request.Nickname)
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := decodeAuthJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	response, err := s.bridge.login(r.Context(), request.Account, request.Password)
	if err != nil {
		s.writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) protected(next http.Handler) http.Handler {
	if s.bridge == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token")
			return
		}
		liveToken, err := s.bridge.liveToken(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			s.writeAuthError(w, err)
			return
		}
		r.Header.Set("Authorization", "Bearer "+liveToken)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) writeAuthError(w http.ResponseWriter, err error) {
	if errors.Is(err, errFluxFeedUnauthorized) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid FluxFeed access token")
		return
	}
	var upstream *fluxFeedHTTPError
	if errors.As(err, &upstream) && upstream.status >= 400 && upstream.status < 500 {
		writeError(w, upstream.status, "AUTH_FAILED", upstream.message)
		return
	}
	s.log.Error("FluxFeed authentication bridge failed", "error", err)
	writeError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication service unavailable")
}

func decodeAuthJSON(r *http.Request, output any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<10+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return errors.New("invalid JSON request")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	base := requestID(s.log, recoverer(s.log, s.mux))
	if s.metrics != nil {
		base = s.metrics.HTTPMiddleware("live-api", base)
	}
	return observability.TraceHTTP(base)
}

func requestID(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = idgen.New()
		}
		r.Header.Set("X-Request-ID", id)
		w.Header().Set("X-Request-ID", id)
		started := time.Now()
		next.ServeHTTP(w, r)
		attrs := []any{"request_id", id, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds()}
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			attrs = append(attrs, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
		}
		log.InfoContext(r.Context(), "http request", attrs...)
	})
}

func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Error("panic recovered", "panic", v, "path", r.URL.Path, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, code int, errorCode, message string) {
	writeJSON(w, code, map[string]any{"code": errorCode, "message": message})
}
