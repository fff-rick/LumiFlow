package accessprojection

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/mq"
	"github.com/example/live-platform/internal/projectionevent"
	"github.com/example/live-platform/internal/room"
	"github.com/example/live-platform/internal/store/redisstore"
)

type memoryStore struct {
	values map[string]redisstore.ProjectionRecord
}

func (s *memoryStore) ApplyProjection(_ context.Context, key string, value redisstore.ProjectionRecord) (bool, error) {
	if current, ok := s.values[key]; ok && current.Version >= value.Version {
		return false, nil
	}
	s.values[key] = value
	return true, nil
}
func (s *memoryStore) RefreshProjection(_ context.Context, key string, expectedVersion int64, projectedAt time.Time, payload []byte) (bool, error) {
	current, ok := s.values[key]
	if !ok || current.Version != expectedVersion {
		return false, nil
	}
	current.ProjectedAt = projectedAt
	current.Payload = append([]byte(nil), payload...)
	s.values[key] = current
	return true, nil
}
func (s *memoryStore) LoadProjection(_ context.Context, key string) (redisstore.ProjectionRecord, bool, error) {
	value, ok := s.values[key]
	return value, ok, nil
}

func TestHandlerIgnoresDuplicateAndOutOfOrderEvents(t *testing.T) {
	store := &memoryStore{values: map[string]redisstore.ProjectionRecord{}}
	metrics := &fakeProjectionMetrics{}
	topics := Topics{Identity: "live.identity.v1", Room: "live.room.v1", Governance: "live.governance.v1"}
	handler := NewHandler(NamespaceInteraction, store, topics, metrics)
	newer := projectionRecord(t, topics.Room, projectionevent.RoomUpserted, "9", 2, projectionevent.RoomPayload{RoomID: 9, AnchorID: 3, Title: "new", Status: "LIVING", CreatedAt: time.Now()})
	older := projectionRecord(t, topics.Room, projectionevent.RoomUpserted, "9", 1, projectionevent.RoomPayload{RoomID: 9, AnchorID: 3, Title: "old", Status: "PREPARING", CreatedAt: time.Now()})
	if err := handler.Handle(context.Background(), newer); err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), older); err != nil {
		t.Fatal(err)
	}
	stored := store.values[roomKey(NamespaceInteraction, 9)]
	var payload projectionevent.RoomPayload
	if err := json.Unmarshal(stored.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if stored.Version != 2 || payload.Title != "new" {
		t.Fatalf("projection regressed: version=%d payload=%+v", stored.Version, payload)
	}
	if got := metrics.applies; len(got) != 2 || got[0] != "room:applied" || got[1] != "room:out_of_order" {
		t.Fatalf("apply metrics=%v", got)
	}
}

func TestReaderUsesFreshLocalProjection(t *testing.T) {
	store := &memoryStore{values: map[string]redisstore.ProjectionRecord{}}
	now := time.Now().UTC()
	putProjection(t, store, roomKey(NamespaceCommerce, 7), now, projectionevent.RoomPayload{RoomID: 7, AnchorID: 4, Title: "room", Status: "LIVING", CreatedAt: now})
	putProjection(t, store, governanceKey(NamespaceCommerce, 7, 8), now, projectionevent.GovernancePayload{RoomID: 7, UserID: 8, Banned: true})
	putProjection(t, store, identityKey(NamespaceCommerce, 8), now, projectionevent.IdentityPayload{UserID: 8, Nickname: "viewer", Status: 1})
	fallback := &fakeFallback{}
	reader := NewReader(NamespaceCommerce, store, fallback, ModePrefer, time.Minute, nil)
	access, err := reader.GetRoomAccess(context.Background(), 7, 8)
	if err != nil || !access.Banned || access.Room.Status != room.StatusLiving {
		t.Fatalf("access=%+v err=%v", access, err)
	}
	user, err := reader.User(context.Background(), 8)
	if err != nil || user.Nickname != "viewer" || fallback.calls != 0 {
		t.Fatalf("user=%+v calls=%d err=%v", user, fallback.calls, err)
	}
}

func TestReaderFailsClosedWhenProjectionIsStaleAndFallbackFails(t *testing.T) {
	store := &memoryStore{values: map[string]redisstore.ProjectionRecord{}}
	putProjection(t, store, roomKey(NamespaceInteraction, 7), time.Now().Add(-2*time.Minute), projectionevent.RoomPayload{RoomID: 7, AnchorID: 4, Title: "room", Status: "LIVING", CreatedAt: time.Now()})
	want := errors.New("identity-room unavailable")
	metrics := &fakeProjectionMetrics{}
	reader := NewReader(NamespaceInteraction, store, &fakeFallback{err: want}, ModePrefer, time.Minute, metrics)
	if _, err := reader.Get(context.Background(), 7); !errors.Is(err, want) {
		t.Fatalf("err=%v want=%v", err, want)
	}
	if metrics.failClosed != 1 {
		t.Fatalf("fail-closed metrics=%d want 1", metrics.failClosed)
	}
}

func TestReaderRefreshesStaleProjectionWithoutChangingVersion(t *testing.T) {
	store := &memoryStore{values: map[string]redisstore.ProjectionRecord{}}
	old := time.Now().Add(-2 * time.Minute)
	putProjection(t, store, roomKey(NamespaceInteraction, 7), old, projectionevent.RoomPayload{RoomID: 7, AnchorID: 4, Title: "old", Status: "LIVING", CreatedAt: old})
	current := store.values[roomKey(NamespaceInteraction, 7)]
	current.Version = 9
	store.values[roomKey(NamespaceInteraction, 7)] = current
	fallback := &fakeFallback{room: room.Room{ID: 7, AnchorID: 4, Title: "new", Status: room.StatusClosed}}
	reader := NewReader(NamespaceInteraction, store, fallback, ModePrefer, time.Minute, nil)
	value, err := reader.Get(context.Background(), 7)
	if err != nil || value.Title != "new" {
		t.Fatalf("room=%+v err=%v", value, err)
	}
	stored := store.values[roomKey(NamespaceInteraction, 7)]
	var payload projectionevent.RoomPayload
	if err := json.Unmarshal(stored.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if stored.Version != 9 || payload.Title != "new" || !stored.ProjectedAt.After(old) {
		t.Fatalf("stored=%+v payload=%+v", stored, payload)
	}
}

func TestReaderExpiresTimedMuteLocally(t *testing.T) {
	store := &memoryStore{values: map[string]redisstore.ProjectionRecord{}}
	now := time.Now().UTC()
	until := now.Add(-time.Second)
	putProjection(t, store, roomKey(NamespaceInteraction, 7), now, projectionevent.RoomPayload{RoomID: 7, AnchorID: 4, Title: "room", Status: "LIVING", CreatedAt: now})
	putProjection(t, store, governanceKey(NamespaceInteraction, 7, 8), now, projectionevent.GovernancePayload{RoomID: 7, UserID: 8, Muted: true, MutedUntil: &until})
	reader := NewReader(NamespaceInteraction, store, &fakeFallback{}, ModePrefer, time.Minute, nil)
	access, err := reader.GetRoomAccess(context.Background(), 7, 8)
	if err != nil || access.Muted {
		t.Fatalf("access=%+v err=%v", access, err)
	}
}

func TestReaderShadowModeReturnsAuthorityAndReportsMismatch(t *testing.T) {
	store := &memoryStore{values: map[string]redisstore.ProjectionRecord{}}
	now := time.Now().UTC()
	putProjection(t, store, roomKey(NamespaceInteraction, 7), now, projectionevent.RoomPayload{RoomID: 7, AnchorID: 4, Title: "projected", Status: "LIVING", CreatedAt: now})
	fallback := &fakeFallback{room: room.Room{ID: 7, AnchorID: 4, Title: "authority", Status: room.StatusLiving, CreatedAt: now}}
	metrics := &fakeProjectionMetrics{}
	reader := NewReader(NamespaceInteraction, store, fallback, ModeShadow, time.Minute, metrics)

	value, err := reader.Get(context.Background(), 7)
	if err != nil || value.Title != "authority" || fallback.calls != 1 {
		t.Fatalf("room=%+v calls=%d err=%v", value, fallback.calls, err)
	}
	if len(metrics.comparisons) != 1 || metrics.comparisons[0] != "room:mismatch" {
		t.Fatalf("comparison metrics=%v", metrics.comparisons)
	}
	var payload projectionevent.RoomPayload
	if err := json.Unmarshal(store.values[roomKey(NamespaceInteraction, 7)].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Title != "projected" {
		t.Fatalf("shadow mode rewrote projection: %+v", payload)
	}
}

func projectionRecord(t *testing.T, topic, eventType, aggregateID string, version int64, payload any) mq.Record {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	aggregateType := "ROOM"
	if eventType == projectionevent.IdentityUserUpserted {
		aggregateType = "IDENTITY"
	} else if eventType == projectionevent.GovernanceChanged {
		aggregateType = "ROOM_GOVERNANCE"
	}
	envelope, err := json.Marshal(mq.Envelope{EventID: "event", EventType: eventType, Version: 1, Producer: projectionevent.Producer, AggregateType: aggregateType, AggregateID: aggregateID, AggregateVersion: version, CreatedAt: time.Now().UTC(), Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	return mq.Record{Topic: topic, Partition: 1, Offset: version, Value: envelope}
}

func putProjection(t *testing.T, store *memoryStore, key string, projectedAt time.Time, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	store.values[key] = redisstore.ProjectionRecord{Version: 1, ProjectedAt: projectedAt, Payload: raw}
}

type fakeFallback struct {
	err   error
	room  room.Room
	calls int
}

func (f *fakeFallback) called() error { f.calls++; return f.err }
func (f *fakeFallback) Create(context.Context, int64, string) (room.Room, error) {
	return room.Room{}, f.called()
}
func (f *fakeFallback) Get(context.Context, int64) (room.Room, error) {
	err := f.called()
	return f.room, err
}
func (f *fakeFallback) List(context.Context, room.Status, int) ([]room.Room, error) {
	return nil, f.called()
}
func (f *fakeFallback) Start(context.Context, int64, int64) (room.Room, error) {
	return room.Room{}, f.called()
}
func (f *fakeFallback) Stop(context.Context, int64, int64) (room.Room, error) {
	return room.Room{}, f.called()
}
func (f *fakeFallback) Join(context.Context, int64, int64) (room.Room, error) {
	return room.Room{}, f.called()
}
func (f *fakeFallback) GetRoomAccess(context.Context, int64, int64) (room.RoomAccess, error) {
	return room.RoomAccess{}, f.called()
}
func (f *fakeFallback) IsMuted(context.Context, int64, int64) (bool, error) {
	return false, f.called()
}
func (f *fakeFallback) IsBanned(context.Context, int64, int64) (bool, error) {
	return false, f.called()
}
func (f *fakeFallback) Mute(context.Context, int64, int64, int64, time.Duration, string) error {
	return f.called()
}
func (f *fakeFallback) Unmute(context.Context, int64, int64, int64) error { return f.called() }
func (f *fakeFallback) ListMutes(context.Context, int64, int64) ([]room.Mute, error) {
	return nil, f.called()
}
func (f *fakeFallback) Ban(context.Context, int64, int64, int64, string) error {
	return f.called()
}
func (f *fakeFallback) Unban(context.Context, int64, int64, int64) error { return f.called() }
func (f *fakeFallback) ListBans(context.Context, int64, int64) ([]room.Ban, error) {
	return nil, f.called()
}
func (f *fakeFallback) User(context.Context, int64) (auth.User, error) {
	return auth.User{}, f.called()
}

type fakeProjectionMetrics struct {
	applies     []string
	comparisons []string
	failClosed  int
}

func (*fakeProjectionMetrics) ProjectionRead(string, string, string, string, time.Duration) {}
func (*fakeProjectionMetrics) ProjectionFallback(string, string, string, string)            {}
func (m *fakeProjectionMetrics) ProjectionCompared(_ string, entity, result string) {
	m.comparisons = append(m.comparisons, entity+":"+result)
}
func (m *fakeProjectionMetrics) ProjectionApplied(_ string, entity, result string) {
	m.applies = append(m.applies, entity+":"+result)
}
func (m *fakeProjectionMetrics) ProjectionFailedClosed(string, string, string) {
	m.failClosed++
}
