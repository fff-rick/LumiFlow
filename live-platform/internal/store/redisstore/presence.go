package redisstore

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type PresenceStore struct {
	store *Store
	ttl   time.Duration
}

func NewPresenceStore(store *Store, ttl time.Duration) *PresenceStore {
	return &PresenceStore{store: store, ttl: ttl}
}

func presenceViewersKey(roomID int64) string { return "live:presence:" + roomTag(roomID) + ":viewers" }
func presenceViewerJoinKey(roomID int64) string {
	return "live:presence:" + roomTag(roomID) + ":viewer-join"
}
func presenceViewerGiftKey(roomID int64) string {
	return "live:presence:" + roomTag(roomID) + ":viewer-gift"
}

// shortcut: consumers share this Presence-owned aggregate key; publish viewer-count events when Redis is physically isolated.
func presenceViewerCountKey(roomID int64) string {
	return "live:presence:" + roomTag(roomID) + ":viewer-count"
}
func presenceMigrationKey(roomID int64) string {
	return "live:presence:" + roomTag(roomID) + ":migrated"
}
func presenceGiftEventKey(roomID int64, eventID string) string {
	return "live:presence:" + roomTag(roomID) + ":gift-event:" + eventID
}

var migratePresenceScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[7]) == 1 then return 0 end
local active = redis.call('ZRANGEBYSCORE', KEYS[1], ARGV[1], '+inf', 'WITHSCORES')
for i=1,#active,2 do
  local user = active[i]
  redis.call('ZADD', KEYS[4], active[i+1], user)
  local joined = redis.call('ZSCORE', KEYS[2], user)
  if joined then redis.call('ZADD', KEYS[5], 'NX', joined, user) end
  local gift = redis.call('ZSCORE', KEYS[3], user)
  if gift then redis.call('ZADD', KEYS[6], 'NX', gift, user) end
end
redis.call('SET', KEYS[7], '1', 'PX', ARGV[2])
redis.call('SET', KEYS[8], #active / 2, 'PX', ARGV[3])
return #active / 2
`)

func (s *PresenceStore) migrate(ctx context.Context, roomID int64) error {
	return migratePresenceScript.Run(ctx, s.store.client, []string{
		roomViewersKey(roomID), roomViewerJoinKey(roomID), roomViewerGiftKey(roomID),
		presenceViewersKey(roomID), presenceViewerJoinKey(roomID), presenceViewerGiftKey(roomID), presenceMigrationKey(roomID), presenceViewerCountKey(roomID),
	}, time.Now().UnixMilli(), (10 * time.Minute).Milliseconds(), (2 * s.ttl).Milliseconds()).Err()
}

var touchPresenceScript = redis.NewScript(`
redis.call('ZADD', KEYS[1], ARGV[3], ARGV[2])
redis.call('ZADD', KEYS[2], 'NX', ARGV[1], ARGV[2])
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
for _, user in ipairs(expired) do redis.call('ZREM', KEYS[2], user) end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
local count = redis.call('ZCARD', KEYS[1])
redis.call('SET', KEYS[3], count, 'PX', ARGV[4])
return count
`)

func (s *PresenceStore) TouchViewer(ctx context.Context, roomID, userID int64, ttl time.Duration) (int64, error) {
	if err := s.migrate(ctx, roomID); err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	expires := now + ttl.Milliseconds()
	if err := s.store.client.ZAdd(ctx, activeStatsRoomsKey(s.store.activeRoomShard(roomID)), redis.Z{Score: float64(now), Member: roomID}).Err(); err != nil {
		return 0, err
	}
	return touchPresenceScript.Run(ctx, s.store.client, []string{presenceViewersKey(roomID), presenceViewerJoinKey(roomID), presenceViewerCountKey(roomID)}, now, userID, expires, (2 * ttl).Milliseconds()).Int64()
}

var removePresenceScript = redis.NewScript(`
redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
local count = redis.call('ZCARD', KEYS[1])
redis.call('SET', KEYS[3], count, 'PX', ARGV[2])
return count
`)

func (s *PresenceStore) RemoveViewer(ctx context.Context, roomID, userID int64) (int64, error) {
	if err := s.migrate(ctx, roomID); err != nil {
		return 0, err
	}
	return removePresenceScript.Run(ctx, s.store.client, []string{presenceViewersKey(roomID), presenceViewerJoinKey(roomID), presenceViewerCountKey(roomID)}, userID, (2 * s.ttl).Milliseconds()).Int64()
}

func (s *PresenceStore) EnforceLikeBan(ctx context.Context, roomID, userID int64, banned bool) error {
	return s.store.EnforceLikeBan(ctx, roomID, userID, banned)
}

func (s *PresenceStore) TopViewers(ctx context.Context, roomID, limit int64) ([]int64, error) {
	if err := s.migrate(ctx, roomID); err != nil {
		return nil, err
	}
	vals, err := topViewersScript.Run(ctx, s.store.client, []string{presenceViewersKey(roomID), presenceViewerGiftKey(roomID), presenceViewerJoinKey(roomID)}, time.Now().UnixMilli(), limit).StringSlice()
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(vals)/2)
	for i := 0; i+1 < len(vals); i += 2 {
		id, err := parseInt(vals[i])
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *PresenceStore) OnlineViewers(ctx context.Context, roomID, limit int64) ([]int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return s.TopViewers(ctx, roomID, limit)
}

func (s *PresenceStore) AddViewerGiftValue(ctx context.Context, roomID, userID, value int64) error {
	if err := s.migrate(ctx, roomID); err != nil {
		return err
	}
	return s.store.client.ZIncrBy(ctx, presenceViewerGiftKey(roomID), float64(value), formatInt(userID)).Err()
}

var applyPresenceGiftScript = redis.NewScript(`
if redis.call('SET', KEYS[1], '1', 'NX', 'EX', 604800) then
  redis.call('ZINCRBY', KEYS[2], ARGV[1], ARGV[2])
  return 1
end
return 0
`)

func (s *PresenceStore) ApplyGiftValue(ctx context.Context, eventID string, roomID, userID, value int64) error {
	if err := s.migrate(ctx, roomID); err != nil {
		return err
	}
	return applyPresenceGiftScript.Run(ctx, s.store.client, []string{presenceGiftEventKey(roomID, eventID), presenceViewerGiftKey(roomID)}, value, userID).Err()
}
