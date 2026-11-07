package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/example/live-platform/internal/media"
	"github.com/example/live-platform/internal/store/mysqlstore"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := loadConfig()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	db, err := mysqlstore.Open(config.roomDSN, mysqlstore.Config{MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: 30 * time.Minute})
	if err != nil {
		log.Error("open room database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	objects, err := media.NewS3ObjectStore(config.s3)
	if err != nil {
		log.Error("create S3 client", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := objects.EnsureBucket(ctx); err != nil {
		log.Error("ensure recording bucket", "error", err)
		os.Exit(1)
	}
	archiver := media.NewArchiver(log, media.NewRepository(db), objects, config.archiver)
	log.Info("recording archiver started", "bucket", config.s3.Bucket, "bytes_per_second", config.s3.BytesPerSecond)
	if err := archiver.Run(ctx); err != nil && err != context.Canceled {
		log.Error("recording archiver stopped", "error", err)
		os.Exit(1)
	}
}

type appConfig struct {
	roomDSN  string
	s3       media.S3Config
	archiver media.ArchiverConfig
}

func loadConfig() (appConfig, error) {
	secure, err := strconv.ParseBool(env("RECORDING_S3_SECURE", "false"))
	if err != nil {
		return appConfig{}, fmt.Errorf("RECORDING_S3_SECURE: %w", err)
	}
	publicRead, err := strconv.ParseBool(env("RECORDING_S3_PUBLIC_READ", "false"))
	if err != nil {
		return appConfig{}, fmt.Errorf("RECORDING_S3_PUBLIC_READ: %w", err)
	}
	poll, err := positiveDuration("RECORDING_ARCHIVE_POLL_INTERVAL", "2s")
	if err != nil {
		return appConfig{}, err
	}
	lease, err := positiveDuration("RECORDING_ARCHIVE_LEASE", "10m")
	if err != nil {
		return appConfig{}, err
	}
	bytesPerSecond, err := strconv.ParseInt(env("RECORDING_ARCHIVE_BYTES_PER_SECOND", "2500000"), 10, 64)
	if err != nil || bytesPerSecond <= 0 {
		return appConfig{}, fmt.Errorf("RECORDING_ARCHIVE_BYTES_PER_SECOND must be positive")
	}
	config := appConfig{
		roomDSN: required("ROOM_MYSQL_DSN"),
		s3: media.S3Config{
			Endpoint: required("RECORDING_S3_ENDPOINT"), AccessKey: required("RECORDING_S3_ACCESS_KEY"),
			SecretKey: required("RECORDING_S3_SECRET_KEY"), Bucket: env("RECORDING_S3_BUCKET", "lumiflow-recordings"),
			Region: env("RECORDING_S3_REGION", "us-east-1"), Secure: secure, PublicRead: publicRead,
			BytesPerSecond: bytesPerSecond,
		},
		archiver: media.ArchiverConfig{
			RecordingDir: env("RECORDING_ARCHIVE_DIR", "/recordings"), PollInterval: poll,
			Lease: lease,
		},
	}
	if config.roomDSN == "" || config.s3.Endpoint == "" || config.s3.AccessKey == "" || config.s3.SecretKey == "" {
		return appConfig{}, fmt.Errorf("ROOM_MYSQL_DSN and RECORDING_S3 endpoint credentials are required")
	}
	return config, nil
}

func positiveDuration(key, fallback string) (time.Duration, error) {
	value, err := time.ParseDuration(env(key, fallback))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return value, nil
}

func required(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func env(key, fallback string) string {
	if value := required(key); value != "" {
		return value
	}
	return fallback
}
