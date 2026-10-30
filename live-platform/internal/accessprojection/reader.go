package accessprojection

import (
	"context"
	"encoding/json"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/projectionevent"
	"github.com/example/live-platform/internal/room"
	"github.com/example/live-platform/internal/store/redisstore"
)

const (
	ModeShadow = "shadow"
	ModePrefer = "prefer"
)

type Fallback interface {
	Create(context.Context, int64, string) (room.Room, error)
	Get(context.Context, int64) (room.Room, error)
	List(context.Context, room.Status, int) ([]room.Room, error)
	Start(context.Context, int64, int64) (room.Room, error)
	Stop(context.Context, int64, int64) (room.Room, error)
	Join(context.Context, int64, int64) (room.Room, error)
	GetRoomAccess(context.Context, int64, int64) (room.RoomAccess, error)
	IsMuted(context.Context, int64, int64) (bool, error)
	IsBanned(context.Context, int64, int64) (bool, error)
	Mute(context.Context, int64, int64, int64, time.Duration, string) error
	Unmute(context.Context, int64, int64, int64) error
	ListMutes(context.Context, int64, int64) ([]room.Mute, error)
	Ban(context.Context, int64, int64, int64, string) error
	Unban(context.Context, int64, int64, int64) error
	ListBans(context.Context, int64, int64) ([]room.Ban, error)
	User(context.Context, int64) (auth.User, error)
}

type Reader struct {
	namespace    string
	store        Store
	fallback     Fallback
	mode         string
	maxStaleness time.Duration
	metrics      Metrics
	now          func() time.Time
}

func NewReader(namespace string, store Store, fallback Fallback, mode string, maxStaleness time.Duration, metrics Metrics) *Reader {
	return &Reader{namespace: namespace, store: store, fallback: fallback, mode: mode, maxStaleness: maxStaleness, metrics: metrics, now: time.Now}
}

func (r *Reader) Get(ctx context.Context, roomID int64) (room.Room, error) {
	projected, record, exists, err := r.projectedRoom(ctx, roomID)
	r.observeRead("room", record, exists, err)
	if r.mode == ModeShadow {
		value, fallbackErr := r.fallback.Get(ctx, roomID)
		r.observeFallback("room", fallbackErr)
		if fallbackErr == nil {
			r.observeComparison("room", comparableResult(err, exists, roomEquivalent(projected, value)))
		}
		return value, fallbackErr
	}
	if err == nil && exists && r.fresh(record) {
		return projected, nil
	}
	value, fallbackErr := r.fallback.Get(ctx, roomID)
	r.observeFallback("room", fallbackErr)
	if fallbackErr != nil {
		r.failClosed("room")
	}
	return value, fallbackErr
}

func (r *Reader) GetRoomAccess(ctx context.Context, roomID, userID int64) (room.RoomAccess, error) {
	projectedRoom, roomRecord, roomExists, err := r.projectedRoom(ctx, roomID)
	r.observeRead("room", roomRecord, roomExists, err)
	governance, governanceRecord, governanceExists, governanceErr := r.projectedGovernance(ctx, roomID, userID)
	r.observeRead("governance", governanceRecord, governanceExists, governanceErr)
	if r.mode == ModeShadow {
		value, fallbackErr := r.fallback.GetRoomAccess(ctx, roomID, userID)
		r.observeFallback("room_access", fallbackErr)
		if fallbackErr == nil {
			equal := roomEquivalent(projectedRoom, value.Room) && governance.Banned == value.Banned && governance.Muted == value.Muted
			r.observeComparison("room_access", comparableResult(firstError(err, governanceErr), roomExists && governanceExists, equal))
		}
		return value, fallbackErr
	}
	if err == nil && governanceErr == nil && roomExists && governanceExists && r.fresh(roomRecord) && r.fresh(governanceRecord) {
		return room.RoomAccess{Room: projectedRoom, Banned: governance.Banned, Muted: governance.Muted}, nil
	}
	value, fallbackErr := r.fallback.GetRoomAccess(ctx, roomID, userID)
	r.observeFallback("room_access", fallbackErr)
	if fallbackErr != nil {
		r.failClosed("room_access")
	}
	return value, fallbackErr
}

func (r *Reader) IsBanned(ctx context.Context, roomID, userID int64) (bool, error) {
	access, err := r.GetRoomAccess(ctx, roomID, userID)
	return access.Banned, err
}

func (r *Reader) IsMuted(ctx context.Context, roomID, userID int64) (bool, error) {
	access, err := r.GetRoomAccess(ctx, roomID, userID)
	return access.Muted, err
}

func (r *Reader) User(ctx context.Context, userID int64) (auth.User, error) {
	projected, record, exists, err := r.projectedUser(ctx, userID)
	r.observeRead("identity", record, exists, err)
	if r.mode == ModeShadow {
		value, fallbackErr := r.fallback.User(ctx, userID)
		r.observeFallback("identity", fallbackErr)
		if fallbackErr == nil {
			equal := projected.ID == value.ID && projected.Nickname == value.Nickname && projected.Avatar == value.Avatar && projected.Status == value.Status
			r.observeComparison("identity", comparableResult(err, exists, equal))
		}
		return value, fallbackErr
	}
	if err == nil && exists && r.fresh(record) {
		return projected, nil
	}
	value, fallbackErr := r.fallback.User(ctx, userID)
	r.observeFallback("identity", fallbackErr)
	if fallbackErr != nil {
		r.failClosed("identity")
	}
	return value, fallbackErr
}

func (r *Reader) projectedUser(ctx context.Context, userID int64) (auth.User, redisstore.ProjectionRecord, bool, error) {
	record, exists, err := r.store.LoadProjection(ctx, identityKey(r.namespace, userID))
	if err != nil || !exists {
		return auth.User{}, record, exists, err
	}
	var payload projectionevent.IdentityPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return auth.User{}, record, true, err
	}
	return auth.User{ID: payload.UserID, Nickname: payload.Nickname, Avatar: payload.Avatar, Status: payload.Status}, record, true, nil
}

func (r *Reader) projectedRoom(ctx context.Context, roomID int64) (room.Room, redisstore.ProjectionRecord, bool, error) {
	record, exists, err := r.store.LoadProjection(ctx, roomKey(r.namespace, roomID))
	if err != nil || !exists {
		return room.Room{}, record, exists, err
	}
	var payload projectionevent.RoomPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return room.Room{}, record, true, err
	}
	return room.Room{ID: payload.RoomID, AnchorID: payload.AnchorID, Title: payload.Title, Status: room.Status(payload.Status), StartedAt: payload.StartedAt, EndedAt: payload.EndedAt, CreatedAt: payload.CreatedAt}, record, true, nil
}

func (r *Reader) projectedGovernance(ctx context.Context, roomID, userID int64) (projectionevent.GovernancePayload, redisstore.ProjectionRecord, bool, error) {
	record, exists, err := r.store.LoadProjection(ctx, governanceKey(r.namespace, roomID, userID))
	if err != nil || !exists {
		return projectionevent.GovernancePayload{}, record, exists, err
	}
	var payload projectionevent.GovernancePayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return projectionevent.GovernancePayload{}, record, true, err
	}
	if payload.Muted && payload.MutedUntil != nil && !payload.MutedUntil.After(r.now()) {
		payload.Muted = false
	}
	return payload, record, true, nil
}

func (r *Reader) fresh(record redisstore.ProjectionRecord) bool {
	return r.maxStaleness > 0 && r.now().Sub(record.ProjectedAt) <= r.maxStaleness
}

func (r *Reader) observeRead(entity string, record redisstore.ProjectionRecord, exists bool, err error) {
	if r.metrics == nil {
		return
	}
	result := "miss"
	age := time.Duration(-1)
	if err != nil {
		result = "error"
	} else if exists {
		age = r.now().Sub(record.ProjectedAt)
		if age < 0 {
			age = 0
		}
		result = "hit"
		if !r.fresh(record) {
			result = "stale"
		}
	}
	r.metrics.ProjectionRead(r.namespace, r.mode, entity, result, age)
}

func (r *Reader) observeFallback(entity string, err error) {
	if r.metrics == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "error"
	}
	r.metrics.ProjectionFallback(r.namespace, r.mode, entity, result)
}

func (r *Reader) observeComparison(entity, result string) {
	if r.metrics != nil {
		r.metrics.ProjectionCompared(r.namespace, entity, result)
	}
}

func (r *Reader) failClosed(entity string) {
	if r.metrics != nil {
		r.metrics.ProjectionFailedClosed(r.namespace, r.mode, entity)
	}
}

func comparableResult(err error, exists, equal bool) string {
	if err != nil {
		return "error"
	}
	if !exists {
		return "missing"
	}
	if equal {
		return "match"
	}
	return "mismatch"
}

func firstError(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func roomEquivalent(a, b room.Room) bool {
	return a.ID == b.ID && a.AnchorID == b.AnchorID && a.Title == b.Title && a.Status == b.Status && timePtrEqual(a.StartedAt, b.StartedAt) && timePtrEqual(a.EndedAt, b.EndedAt) && a.CreatedAt.Equal(b.CreatedAt)
}

func timePtrEqual(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

func (r *Reader) Create(ctx context.Context, anchorID int64, title string) (room.Room, error) {
	return r.fallback.Create(ctx, anchorID, title)
}
func (r *Reader) List(ctx context.Context, status room.Status, limit int) ([]room.Room, error) {
	return r.fallback.List(ctx, status, limit)
}
func (r *Reader) Start(ctx context.Context, id, actorID int64) (room.Room, error) {
	return r.fallback.Start(ctx, id, actorID)
}
func (r *Reader) Stop(ctx context.Context, id, actorID int64) (room.Room, error) {
	return r.fallback.Stop(ctx, id, actorID)
}
func (r *Reader) Join(ctx context.Context, id, userID int64) (room.Room, error) {
	return r.fallback.Join(ctx, id, userID)
}
func (r *Reader) Mute(ctx context.Context, roomID, actorID, targetID int64, duration time.Duration, reason string) error {
	return r.fallback.Mute(ctx, roomID, actorID, targetID, duration, reason)
}
func (r *Reader) Unmute(ctx context.Context, roomID, actorID, targetID int64) error {
	return r.fallback.Unmute(ctx, roomID, actorID, targetID)
}
func (r *Reader) ListMutes(ctx context.Context, roomID, actorID int64) ([]room.Mute, error) {
	return r.fallback.ListMutes(ctx, roomID, actorID)
}
func (r *Reader) Ban(ctx context.Context, roomID, actorID, targetID int64, reason string) error {
	return r.fallback.Ban(ctx, roomID, actorID, targetID, reason)
}
func (r *Reader) Unban(ctx context.Context, roomID, actorID, targetID int64) error {
	return r.fallback.Unban(ctx, roomID, actorID, targetID)
}
func (r *Reader) ListBans(ctx context.Context, roomID, actorID int64) ([]room.Ban, error) {
	return r.fallback.ListBans(ctx, roomID, actorID)
}
