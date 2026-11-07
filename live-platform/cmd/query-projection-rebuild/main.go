package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/example/live-platform/internal/queryprojection"
	"github.com/example/live-platform/internal/queryrebuild"
	"github.com/example/live-platform/internal/store/mysqlstore"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	dsns := map[string]string{
		"identity": strings.TrimSpace(os.Getenv("IDENTITY_MYSQL_DSN")),
		"archive":  strings.TrimSpace(os.Getenv("ARCHIVE_MYSQL_DSN")),
		"commerce": strings.TrimSpace(os.Getenv("COMMERCE_MYSQL_DSN")),
		"query":    strings.TrimSpace(os.Getenv("QUERY_WRITER_MYSQL_DSN")),
	}
	for name, dsn := range dsns {
		if dsn == "" {
			log.Error("database DSN is required", "database", name)
			os.Exit(1)
		}
	}
	batch, err := strconv.Atoi(env("QUERY_PROJECTION_REBUILD_BATCH_SIZE", "200"))
	if err != nil || batch <= 0 || batch > 1000 {
		log.Error("QUERY_PROJECTION_REBUILD_BATCH_SIZE must be between 1 and 1000")
		os.Exit(1)
	}

	pool := mysqlstore.Config{MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: 5 * time.Minute}
	stores := make(map[string]*mysqlstore.Store, len(dsns))
	for name, dsn := range dsns {
		store, openErr := mysqlstore.Open(dsn, pool)
		if openErr != nil {
			log.Error("open mysql", "database", name, "error", openErr)
			os.Exit(1)
		}
		stores[name] = store
		defer func() { _ = store.Close() }()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	for name, store := range stores {
		if err := store.Ping(ctx); err != nil {
			log.Error("ping mysql", "database", name, "error", err)
			os.Exit(1)
		}
	}
	counts, err := queryrebuild.Rebuild(ctx, stores["identity"], stores["archive"], stores["commerce"], queryprojection.NewRepository(stores["query"]), batch)
	if err != nil {
		log.Error("rebuild query projections", "error", err)
		os.Exit(1)
	}
	log.Info("query projections rebuilt", "users", counts.Users, "danmaku", counts.Danmaku, "gifts", counts.Gifts)
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
