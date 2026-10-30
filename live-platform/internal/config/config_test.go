package config

import "testing"

func TestMySQLPoolDefaults(t *testing.T) {
	t.Setenv("MYSQL_MAX_OPEN_CONNS", "")
	t.Setenv("MYSQL_MAX_IDLE_CONNS", "")
	t.Setenv("MYSQL_CONN_MAX_LIFETIME", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MySQL.MaxOpenConns != 40 || cfg.MySQL.MaxIdleConns != 20 {
		t.Fatalf("unexpected pool defaults: open=%d idle=%d", cfg.MySQL.MaxOpenConns, cfg.MySQL.MaxIdleConns)
	}
	if got := cfg.MySQL.ConnMaxLifetime.String(); got != "30m0s" {
		t.Fatalf("unexpected conn lifetime: %s", got)
	}
}

func TestMySQLPoolOverrides(t *testing.T) {
	t.Setenv("MYSQL_MAX_OPEN_CONNS", "80")
	t.Setenv("MYSQL_MAX_IDLE_CONNS", "40")
	t.Setenv("MYSQL_CONN_MAX_LIFETIME", "10m")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MySQL.MaxOpenConns != 80 || cfg.MySQL.MaxIdleConns != 40 {
		t.Fatalf("unexpected pool overrides: open=%d idle=%d", cfg.MySQL.MaxOpenConns, cfg.MySQL.MaxIdleConns)
	}
	if got := cfg.MySQL.ConnMaxLifetime.String(); got != "10m0s" {
		t.Fatalf("unexpected conn lifetime: %s", got)
	}
}

func TestMySQLIdleCannotExceedOpen(t *testing.T) {
	t.Setenv("MYSQL_MAX_OPEN_CONNS", "20")
	t.Setenv("MYSQL_MAX_IDLE_CONNS", "21")
	if _, err := Load(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestWorkerRoleDefaults(t *testing.T) {
	t.Setenv("WORKER_ROLES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"stats", "like-snapshot", "outbox", "identity-outbox", "room-outbox", "gift-consumer", "danmaku-consumer", "identity-projection", "video-projection", "interaction-projection", "commerce-projection"}
	if len(cfg.Worker.Roles) != len(want) {
		t.Fatalf("unexpected worker role count: %v", cfg.Worker.Roles)
	}
	for i := range want {
		if cfg.Worker.Roles[i] != want[i] {
			t.Fatalf("unexpected worker roles: %v", cfg.Worker.Roles)
		}
	}
}

func TestWorkerRoleValidation(t *testing.T) {
	t.Setenv("WORKER_ROLES", "stats,unknown")
	if _, err := Load(); err == nil {
		t.Fatal("expected unsupported worker role error")
	}
}

func TestKafkaSASLRequiresCredentials(t *testing.T) {
	t.Setenv("KAFKA_SASL_MECHANISM", "scram-sha-256")
	t.Setenv("KAFKA_SASL_USERNAME", "")
	t.Setenv("KAFKA_SASL_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected SASL credential validation error")
	}
}

func TestStatsOnlyWorkerDoesNotRequireKafkaCredentials(t *testing.T) {
	t.Setenv("WORKER_ROLES", "stats")
	t.Setenv("KAFKA_SASL_MECHANISM", "scram-sha-512")
	t.Setenv("KAFKA_SASL_USERNAME", "")
	t.Setenv("KAFKA_SASL_PASSWORD", "")
	if _, err := Load(); err != nil {
		t.Fatalf("stats-only worker should not require Kafka credentials: %v", err)
	}
}

func TestVideoProjectionWorkerRequiresKafkaCredentials(t *testing.T) {
	t.Setenv("WORKER_ROLES", "video-projection")
	t.Setenv("KAFKA_SASL_MECHANISM", "scram-sha-512")
	t.Setenv("KAFKA_SASL_USERNAME", "")
	t.Setenv("KAFKA_SASL_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected video projection worker to require Kafka credentials")
	}
}

func TestRoomOutboxWorkerRequiresKafkaCredentials(t *testing.T) {
	t.Setenv("WORKER_ROLES", "room-outbox")
	t.Setenv("KAFKA_SASL_MECHANISM", "scram-sha-512")
	t.Setenv("KAFKA_SASL_USERNAME", "")
	t.Setenv("KAFKA_SASL_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected room outbox worker to require Kafka credentials")
	}
}

func TestProjectionModeDefaultsToShadowAndAcceptsPrefer(t *testing.T) {
	t.Setenv("PROJECTION_MODE", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load default: %v", err)
	}
	if cfg.Projection.Mode != "shadow" {
		t.Fatalf("mode=%q want shadow", cfg.Projection.Mode)
	}

	t.Setenv("PROJECTION_MODE", "prefer")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load prefer: %v", err)
	}
	if cfg.Projection.Mode != "prefer" {
		t.Fatalf("mode=%q want prefer", cfg.Projection.Mode)
	}
}

func TestProjectionModeRejectsUnknownValue(t *testing.T) {
	t.Setenv("PROJECTION_MODE", "both")
	if _, err := Load(); err == nil {
		t.Fatal("expected projection mode validation error")
	}
}

func TestGatewayConfigDoesNotRequireBusinessDependencies(t *testing.T) {
	t.Setenv("MYSQL_MAX_OPEN_CONNS", "invalid")
	t.Setenv("KAFKA_BROKERS", "")
	t.Setenv("KAFKA_SASL_MECHANISM", "invalid")
	t.Setenv("COMMERCE_BASE_URL", "http://commerce:8081")
	t.Setenv("INTERACTION_BASE_URL", "http://interaction:8082")
	t.Setenv("IDENTITY_ROOM_BASE_URL", "http://identity-room:8083")
	t.Setenv("QUERY_BASE_URL", "http://query:8084")
	t.Setenv("FLUXFEED_BASE_URL", "http://fluxfeed:8080")
	cfg, err := LoadGateway()
	if err != nil {
		t.Fatalf("LoadGateway: %v", err)
	}
	if cfg.Query.BaseURL != "http://query:8084" {
		t.Fatalf("unexpected query target %q", cfg.Query.BaseURL)
	}
	if cfg.Auth.FluxFeedBaseURL != "http://fluxfeed:8080" {
		t.Fatalf("unexpected FluxFeed target %q", cfg.Auth.FluxFeedBaseURL)
	}
}
