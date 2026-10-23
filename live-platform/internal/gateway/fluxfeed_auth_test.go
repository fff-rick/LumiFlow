package gateway

import (
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

func TestFluxFeedBridgeOwnsLoginAndExchangesProtectedRequests(t *testing.T) {
	flux := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sessions":
			if r.Method != http.MethodPost {
				t.Fatalf("method=%s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(fluxFeedSession{AccessToken: "flux-token", TokenType: "Bearer", ExpiresInSeconds: 900})
		case "/api/users/me":
			if r.Header.Get("Authorization") != "Bearer flux-token" {
				t.Fatalf("unexpected FluxFeed authorization %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(fluxFeedProfile{ID: 9, Account: "viewer@example.com", Nickname: "viewer", AvatarURL: "https://cdn.example/avatar.png"})
		default:
			t.Fatalf("unexpected FluxFeed path %s", r.URL.Path)
		}
	}))
	defer flux.Close()

	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/users/fluxfeed-sync" || r.Header.Get("X-Internal-Token") != "bridge-secret" {
			t.Fatalf("unexpected identity sync request path=%s token=%q", r.URL.Path, r.Header.Get("X-Internal-Token"))
		}
		_ = json.NewEncoder(w).Encode(auth.User{ID: 77, Username: "fluxfeed:9", Nickname: "viewer"})
	}))
	defer identity.Close()

	liveTokens := auth.NewTokenManager("live-secret", time.Hour)
	protected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := liveTokens.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil || claims.UserID != 77 {
			t.Fatalf("live token claims=%+v err=%v", claims, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer protected.Close()

	api, err := New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), IdentityRoomURL: identity.URL, InteractionURL: protected.URL, CommerceURL: protected.URL, QueryURL: protected.URL, FluxFeedURL: flux.URL, InternalToken: "bridge-secret", BridgeJWTSecret: "live-secret", BridgeTokenTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewServer(api.Handler())
	defer edge.Close()

	login, err := http.Post(edge.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"account":"viewer@example.com","password":"password123"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d", login.StatusCode)
	}
	var session bridgeLoginResponse
	if err := json.NewDecoder(login.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.AccessToken != "flux-token" || session.User.ID != 77 {
		t.Fatalf("unexpected session %+v", session)
	}

	req, err := http.NewRequest(http.MethodPost, edge.URL+"/api/v1/rooms/1/join", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer flux-token")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("protected request status=%d", response.StatusCode)
	}
}

func TestFluxFeedVideoRoutesKeepAccessTokenAndRewritePath(t *testing.T) {
	requests := make(chan string, 7)
	flux := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authorization := r.Header.Get("Authorization"); authorization != "Bearer flux-token" {
			t.Fatalf("authorization=%q", authorization)
		}
		requests <- r.Method + " " + r.URL.RequestURI()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer flux.Close()

	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()

	api, err := New(Deps{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), IdentityRoomURL: upstream.URL,
		InteractionURL: upstream.URL, CommerceURL: upstream.URL, QueryURL: upstream.URL,
		FluxFeedURL: flux.URL, InternalToken: "bridge-secret", BridgeJWTSecret: "live-secret", BridgeTokenTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		method, path, want string
	}{
		{http.MethodPost, "/api/v1/content-assets", "POST /api/uploads"},
		{http.MethodGet, "/api/v1/feed-items?scene=timeline&limit=12", "GET /api/feed-items?scene=timeline&limit=12"},
		{http.MethodPost, "/api/v1/videos", "POST /api/videos"},
		{http.MethodGet, "/api/v1/videos/12", "GET /api/videos/12"},
		{http.MethodDelete, "/api/v1/videos/12", "DELETE /api/videos/12"},
		{http.MethodGet, "/api/v1/users/me/videos", "GET /api/users/me/videos"},
		{http.MethodGet, "/api/v1/users/9/videos", "GET /api/users/9/videos"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer flux-token")
			response := httptest.NewRecorder()
			api.Handler().ServeHTTP(response, req)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if got := <-requests; got != tc.want {
				t.Fatalf("upstream request=%q want %q", got, tc.want)
			}
		})
	}
}

func TestGatewayRequiresFluxFeed(t *testing.T) {
	_, err := New(Deps{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		IdentityRoomURL: "http://identity-room:8083", InteractionURL: "http://interaction:8082",
		CommerceURL: "http://commerce:8081", QueryURL: "http://query:8084",
		InternalToken: "bridge-secret", BridgeJWTSecret: "live-secret", BridgeTokenTTL: time.Hour,
	})
	if err == nil || !strings.Contains(err.Error(), "FLUXFEED_BASE_URL") {
		t.Fatalf("expected missing FluxFeed configuration error, got %v", err)
	}
}
