package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/room"
	"github.com/example/live-platform/internal/store/mysqlstore"
)

type identityRebuilder interface {
	RebuildProjectionBatch(context.Context, int64, int) (int64, int, error)
}

type roomRebuilder interface {
	RebuildRoomProjectionBatch(context.Context, int64, int) (int64, int, error)
	RebuildGovernanceProjectionBatch(context.Context, int64, int64, int) (int64, int64, int, error)
}

func main() {
	// shortcut: one operator job coordinates both schemas; split it when rebuild permissions need separate delegation.
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	identityDSN := strings.TrimSpace(os.Getenv("IDENTITY_MYSQL_DSN"))
	roomDSN := strings.TrimSpace(os.Getenv("ROOM_MYSQL_DSN"))
	if identityDSN == "" || roomDSN == "" {
		log.Error("IDENTITY_MYSQL_DSN and ROOM_MYSQL_DSN are required")
		os.Exit(1)
	}
	batch, err := strconv.Atoi(env("PROJECTION_REBUILD_BATCH_SIZE", "200"))
	if err != nil || batch <= 0 || batch > 1000 {
		log.Error("PROJECTION_REBUILD_BATCH_SIZE must be between 1 and 1000")
		os.Exit(1)
	}
	pool := mysqlstore.Config{MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: 5 * time.Minute}
	identityDB, err := mysqlstore.Open(identityDSN, pool)
	if err != nil {
		log.Error("open identity mysql", "error", err)
		os.Exit(1)
	}
	defer func() { _ = identityDB.Close() }()
	roomDB, err := mysqlstore.Open(roomDSN, pool)
	if err != nil {
		log.Error("open room mysql", "error", err)
		os.Exit(1)
	}
	defer func() { _ = roomDB.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := identityDB.Ping(ctx); err != nil {
		log.Error("ping identity mysql", "error", err)
		os.Exit(1)
	}
	if err := roomDB.Ping(ctx); err != nil {
		log.Error("ping room mysql", "error", err)
		os.Exit(1)
	}
	counts, err := rebuild(ctx,
		auth.NewRepository(identityDB, env("KAFKA_IDENTITY_DOMAIN_TOPIC", "live.identity.v1")),
		room.NewRepository(roomDB, env("KAFKA_ROOM_INTEGRATION_TOPIC", "integration.room.v1"), env("KAFKA_ROOM_DOMAIN_TOPIC", "live.room.v1"), env("KAFKA_GOVERNANCE_TOPIC", "live.governance.v1")),
		batch,
	)
	if err != nil {
		log.Error("rebuild projections", "error", err)
		os.Exit(1)
	}
	log.Info("projection rebuild enqueued", "identities", counts.identities, "rooms", counts.rooms, "governance", counts.governance)
}

type rebuildCounts struct{ identities, rooms, governance int }

func rebuild(ctx context.Context, identities identityRebuilder, rooms roomRebuilder, batch int) (rebuildCounts, error) {
	var counts rebuildCounts
	for after := int64(0); ; {
		next, count, err := identities.RebuildProjectionBatch(ctx, after, batch)
		if err != nil {
			return counts, fmt.Errorf("identity batch after %d: %w", after, err)
		}
		counts.identities += count
		if count == 0 {
			break
		}
		if next <= after {
			return counts, errors.New("identity projection cursor did not advance")
		}
		after = next
	}
	for after := int64(0); ; {
		next, count, err := rooms.RebuildRoomProjectionBatch(ctx, after, batch)
		if err != nil {
			return counts, fmt.Errorf("room batch after %d: %w", after, err)
		}
		counts.rooms += count
		if count == 0 {
			break
		}
		if next <= after {
			return counts, errors.New("room projection cursor did not advance")
		}
		after = next
	}
	for afterRoom, afterUser := int64(0), int64(0); ; {
		nextRoom, nextUser, count, err := rooms.RebuildGovernanceProjectionBatch(ctx, afterRoom, afterUser, batch)
		if err != nil {
			return counts, fmt.Errorf("governance batch after %d:%d: %w", afterRoom, afterUser, err)
		}
		counts.governance += count
		if count == 0 {
			break
		}
		if nextRoom < afterRoom || nextRoom == afterRoom && nextUser <= afterUser {
			return counts, errors.New("governance projection cursor did not advance")
		}
		afterRoom, afterUser = nextRoom, nextUser
	}
	return counts, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
