package live

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisRepository shares response routing locators between gateway replicas.
// IDs are hashed to keep untrusted values out of the Redis key structure.
type RedisRepository struct {
	client redis.UniversalClient
	prefix string
}

func NewRedisRepository(client redis.UniversalClient, prefix string) (*RedisRepository, error) {
	if client == nil {
		return nil, errors.New("live: Redis client is required")
	}
	if prefix == "" {
		prefix = "codego:v3:live:"
	}
	return &RedisRepository{client: client, prefix: prefix}, nil
}

func (r *RedisRepository) key(id string, userID, keyID int64) string {
	hash := sha256.Sum256([]byte(id))
	owner, _ := json.Marshal([]int64{userID, keyID})
	return r.prefix + "response:" + string(owner) + ":" + hex.EncodeToString(hash[:])
}

func (r *RedisRepository) Put(ctx context.Context, item Locator, ttl time.Duration) error {
	if item.ID == "" || item.UserID <= 0 || item.KeyID <= 0 || item.ChannelID <= 0 || item.CredentialID <= 0 || ttl <= 0 {
		return errors.New("live: invalid locator")
	}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.key(item.ID, item.UserID, item.KeyID), data, ttl).Err()
}

func (r *RedisRepository) Get(ctx context.Context, id string, userID, keyID int64) (Locator, error) {
	data, err := r.client.Get(ctx, r.key(id, userID, keyID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Locator{}, ErrNotFound
	}
	if err != nil {
		return Locator{}, err
	}
	var item Locator
	if err = json.Unmarshal(data, &item); err != nil {
		return Locator{}, err
	}
	if item.ID != id || item.UserID != userID || item.KeyID != keyID {
		return Locator{}, ErrNotFound
	}
	return item, nil
}

func (r *RedisRepository) Delete(ctx context.Context, id string, userID, keyID int64) error {
	return r.client.Del(ctx, r.key(id, userID, keyID)).Err()
}
