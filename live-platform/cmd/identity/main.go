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
	"github.com/example/live-platform/internal/httpapi"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/store/mysqlstore"
	cftoken "github.com/example/live-platform/internal/token"
)

func main() {
	log := slog.New(observability.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	closeTrace, err := observability.InitTracer(context.Background(), observability.TraceConfig{Enabled: cfg.Observability.OTelEnabled, ServiceName: "live-identity", Environment: cfg.Observability.Environment, Endpoint: cfg.Observability.OTelEndpoint, Insecure: cfg.Observability.OTelInsecure, SampleRatio: cfg.Observability.OTelSampleRatio})
	if err != nil {
		log.Error("init tracing", "error", err)
		os.Exit(1)
	}
	defer closeTracing(log, closeTrace)

	metrics := observability.NewMetrics("live-identity")
	pool := mysqlstore.Config{MaxOpenConns: cfg.MySQL.MaxOpenConns, MaxIdleConns: cfg.MySQL.MaxIdleConns, ConnMaxLifetime: cfg.MySQL.ConnMaxLifetime}
	db, err := mysqlstore.Open(cfg.Databases.IdentityDSN, pool)
	if err != nil {
		log.Error("open identity mysql", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("close identity mysql", "error", err)
		}
	}()
	metrics.RegisterDBPool("identity-mysql", db.Stats)

	tokens := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	authService := auth.NewService(auth.NewRepository(db, cfg.Kafka.IdentityDomainTopic), tokens)
	handler := httpapi.NewIdentity(httpapi.Deps{
		Log: log, MySQL: db, Metrics: metrics, Auth: authService, AppTokens: tokens,
		InternalToken: cfg.Auth.InternalToken, CFTokens: cftoken.NewIssuer(cfg.Centrifugo.TokenSecret, cfg.Centrifugo.TokenTTL), CFSubTTL: cfg.Centrifugo.SubscriptionTokenTTL,
	})
	run(log, "identity", cfg.Identity.HTTPAddr, handler)
}

func run(log *slog.Logger, name, addr string, handler http.Handler) {
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		log.Info(name+" started", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error(name+" server", "error", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error(name+" shutdown", "error", err)
	}
}

func closeTracing(log *slog.Logger, closeTrace func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := closeTrace(ctx); err != nil {
		log.Warn("shutdown tracing", "error", err)
	}
}
