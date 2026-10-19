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

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/config"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/store/mysqlstore"
	"github.com/example/live-platform/internal/videoapi"
)

func main() {
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	store, err := mysqlstore.Open(cfg.Databases.VideoDSN, mysqlstore.Config{MaxOpenConns: cfg.MySQL.MaxOpenConns, MaxIdleConns: cfg.MySQL.MaxIdleConns, ConnMaxLifetime: cfg.MySQL.ConnMaxLifetime})
	if err != nil {
		log.Error("open video database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	handler := videoapi.New(videoapi.Deps{Log: log, Tokens: auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL), Videos: videoapi.NewMySQLRepository(store), MySQL: store, Metrics: observability.NewMetrics("live-video")})
	srv := &http.Server{Addr: cfg.Video.HTTPAddr, Handler: handler.Handler(), ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() { log.Info("video service started", "addr", cfg.Video.HTTPAddr); errCh <- srv.ListenAndServe() }()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "error", err)
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Error("shutdown", "error", err)
	}
}
