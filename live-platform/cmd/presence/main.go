package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/live-platform/internal/accessprojection"
	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/config"
	"github.com/example/live-platform/internal/httpapi"
	"github.com/example/live-platform/internal/identityclient"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/stats"
	"github.com/example/live-platform/internal/store/redisstore"
	cftoken "github.com/example/live-platform/internal/token"
	"github.com/example/live-platform/internal/viewer"
)

func main() {
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	shutdownTrace, err := observability.InitTracer(context.Background(), observability.TraceConfig{Enabled: cfg.Observability.OTelEnabled, ServiceName: "live-presence", Environment: cfg.Observability.Environment, Endpoint: cfg.Observability.OTelEndpoint, Insecure: cfg.Observability.OTelInsecure, SampleRatio: cfg.Observability.OTelSampleRatio})
	if err != nil {
		log.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTrace(ctx)
	}()

	metrics := observability.NewMetrics("live-presence")
	redis, err := redisstore.Open(redisstore.Config{URL: cfg.Redis.URL, Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, ActiveRoomShards: cfg.Engagement.ActiveRoomShards})
	if err != nil {
		log.Error("open redis", "error", err)
		os.Exit(1)
	}
	defer func() { _ = redis.Close() }()

	fallback := identityclient.NewSplit(cfg.Identity.BaseURL, cfg.Room.BaseURL, cfg.Auth.InternalToken)
	access := accessprojection.NewReader(accessprojection.NamespacePresence, redis, fallback, cfg.Projection.PresenceMode, cfg.Projection.MaxStaleness, metrics)
	presenceStore := redisstore.NewPresenceStore(redis, cfg.Engagement.ViewerTTL)
	h := httpapi.NewPresence(httpapi.Deps{
		Log: log, Redis: redis, Metrics: metrics, Profiles: access, Rooms: access,
		AppTokens: auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL),
		CFTokens:  cftoken.NewIssuer(cfg.Centrifugo.TokenSecret, cfg.Centrifugo.TokenTTL), CFSubTTL: cfg.Centrifugo.SubscriptionTokenTTL,
		Viewers: viewer.NewService(presenceStore, cfg.Engagement.ViewerTTL), Stats: stats.NewService(stats.NewRedisStore(redis)),
	})
	srv := &http.Server{Addr: cfg.Presence.HTTPAddr, Handler: h, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Info("presence started", "addr", cfg.Presence.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "error", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown", "error", err)
	}
}
