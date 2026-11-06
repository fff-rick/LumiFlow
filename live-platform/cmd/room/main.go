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
	"github.com/example/live-platform/internal/media"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/realtime"
	"github.com/example/live-platform/internal/room"
	"github.com/example/live-platform/internal/stats"
	"github.com/example/live-platform/internal/store/mysqlstore"
	"github.com/example/live-platform/internal/store/redisstore"
)

func main() {
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	closeTrace, err := observability.InitTracer(context.Background(), observability.TraceConfig{Enabled: cfg.Observability.OTelEnabled, ServiceName: "live-room", Environment: cfg.Observability.Environment, Endpoint: cfg.Observability.OTelEndpoint, Insecure: cfg.Observability.OTelInsecure, SampleRatio: cfg.Observability.OTelSampleRatio})
	if err != nil {
		log.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer closeRoomTracing(log, closeTrace)

	metrics := observability.NewMetrics("live-room")
	pool := mysqlstore.Config{MaxOpenConns: cfg.MySQL.MaxOpenConns, MaxIdleConns: cfg.MySQL.MaxIdleConns, ConnMaxLifetime: cfg.MySQL.ConnMaxLifetime}
	db, err := mysqlstore.Open(cfg.Databases.RoomDSN, pool)
	if err != nil {
		log.Error("open room mysql", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("close room mysql", "error", err)
		}
	}()
	metrics.RegisterDBPool("room-mysql", db.Stats)

	redis, err := redisstore.Open(redisstore.Config{URL: cfg.Redis.URL, Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB, ActiveRoomShards: cfg.Engagement.ActiveRoomShards})
	if err != nil {
		log.Error("open redis", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := redis.Close(); err != nil {
			log.Error("close redis", "error", err)
		}
	}()

	identity := identityclient.NewInternal(cfg.Identity.BaseURL, cfg.Auth.InternalToken)
	// shortcut: projection misses use one-user HTTP fallback; add batching when fallback volume is measurable.
	users := accessprojection.NewReader(accessprojection.NamespaceRoom, redis, identity, cfg.Projection.RoomMode, cfg.Projection.MaxStaleness, metrics)
	roomService := room.NewService(room.NewRepository(db, cfg.Kafka.RoomIntegrationTopic, cfg.Kafka.RoomDomainTopic, cfg.Kafka.GovernanceTopic), users)
	mediaService := media.NewService(media.NewRepository(db), roomService, media.Config{
		App: cfg.Media.App, RTMPServerURL: cfg.Media.RTMPServerURL, WHEPBaseURL: cfg.Media.WHEPBaseURL, HLSBaseURL: cfg.Media.HLSBaseURL,
		RecordingBaseURL: cfg.Media.RecordingBaseURL, ArchiveBaseURL: cfg.Media.RecordingArchiveBaseURL, ArchiveEnabled: cfg.Media.RecordingArchiveEnabled,
	})
	tokens := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	handler := httpapi.NewRoom(httpapi.Deps{
		Log: log, MySQL: db, Redis: redis, Centrifugo: realtime.NewCentrifugo(cfg.Centrifugo.APIURL, cfg.Centrifugo.APIKey, metrics), Metrics: metrics,
		Governance: redisstore.NewPresenceStore(redis, cfg.Engagement.ViewerTTL), AppTokens: tokens, InternalToken: cfg.Auth.InternalToken, MediaCallbackToken: cfg.Media.CallbackToken, Rooms: roomService, Media: mediaService, Stats: stats.NewService(stats.NewRedisStore(redis)),
	})
	runRoom(log, cfg.Room.HTTPAddr, handler)
}

func runRoom(log *slog.Logger, addr string, handler http.Handler) {
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Info("room started", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("room server", "error", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("room shutdown", "error", err)
	}
}

func closeRoomTracing(log *slog.Logger, closeTrace func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := closeTrace(ctx); err != nil {
		log.Warn("shutdown tracing", "error", err)
	}
}
