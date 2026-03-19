package storage

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// PresenceInfo holds the online flag and last-seen timestamp for a user.
type PresenceInfo struct {
	Online   bool
	LastSeen *time.Time
}

// PresenceStore defines the interface for presence data operations.
type PresenceStore interface {
	SetPresence(ctx context.Context, userID string, ttl time.Duration) error
	GetPresence(ctx context.Context, userIDs []string) (map[string]PresenceInfo, error)
	IncrementRateLimit(ctx context.Context, key string, window time.Duration) (int64, error)
	SetIfNotExists(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Ping(ctx context.Context) error
}

const (
	presenceKeyPrefix = "presence:"
	lastSeenKeyPrefix = "last_seen:"
	lastSeenTTL       = 30 * 24 * time.Hour // 30 days
)

// RedisStore implements PresenceStore using Redis.
type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) SetPresence(ctx context.Context, userID string, ttl time.Duration) error {
	now := time.Now().UTC().Format(time.RFC3339)
	pipe := s.client.Pipeline()
	pipe.Set(ctx, presenceKeyPrefix+userID, now, ttl)
	pipe.Set(ctx, lastSeenKeyPrefix+userID, now, lastSeenTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisStore) GetPresence(ctx context.Context, userIDs []string) (map[string]PresenceInfo, error) {
	if len(userIDs) == 0 {
		return map[string]PresenceInfo{}, nil
	}

	// Build keys: first all presence: keys, then all last_seen: keys
	keys := make([]string, 0, len(userIDs)*2)
	for _, id := range userIDs {
		keys = append(keys, presenceKeyPrefix+id)
	}
	for _, id := range userIDs {
		keys = append(keys, lastSeenKeyPrefix+id)
	}

	vals, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	result := make(map[string]PresenceInfo, len(userIDs))
	for i, id := range userIDs {
		presenceVal := vals[i]
		lastSeenVal := vals[len(userIDs)+i]

		if presenceVal != nil {
			// Online: presence key exists
			result[id] = PresenceInfo{Online: true}
		} else if lastSeenVal != nil {
			// Offline but we have a last_seen record
			if str, ok := lastSeenVal.(string); ok {
				if t, err := time.Parse(time.RFC3339, str); err == nil {
					result[id] = PresenceInfo{Online: false, LastSeen: &t}
					continue
				}
			}
			result[id] = PresenceInfo{Online: false}
		} else {
			// Never seen
			result[id] = PresenceInfo{Online: false}
		}
	}
	return result, nil
}

func (s *RedisStore) IncrementRateLimit(ctx context.Context, key string, window time.Duration) (int64, error) {
	pipe := s.client.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

func (s *RedisStore) SetIfNotExists(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, "1", ttl).Result()
}

func (s *RedisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}
