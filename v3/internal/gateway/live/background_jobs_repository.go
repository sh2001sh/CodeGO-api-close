package live

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrBackgroundLeaseConflict = errors.New("live: background job lease conflict")

const backgroundHistoryTTL = 7 * 24 * time.Hour

// RedisBackgroundRepository keeps unfinished holds indefinitely and retains
// completed jobs and their events for seven days. Only routing identifiers,
// ownership, scheduling and lease metadata are stored without encryption.
type RedisBackgroundRepository struct {
	client redis.UniversalClient
	prefix string
	aead   cipher.AEAD
}

var _ BackgroundJobRepository = (*RedisBackgroundRepository)(nil)

func NewRedisBackgroundRepository(client redis.UniversalClient, prefix string, key []byte) (*RedisBackgroundRepository, error) {
	if client == nil {
		return nil, errors.New("live: Redis client is required")
	}
	if len(key) != 32 {
		return nil, errors.New("live: background encryption key must contain exactly 32 bytes")
	}
	if prefix == "" {
		prefix = "codego:v3:live:background"
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Put the hash tag first, so even a caller-supplied prefix containing braces
	// cannot send the atomic multi-key scripts to different cluster slots.
	hash := sha256.Sum256([]byte(prefix))
	return &RedisBackgroundRepository{client: client, prefix: "{codego-bg:" + hex.EncodeToString(hash[:]) + "}:", aead: aead}, nil
}

func (r *RedisBackgroundRepository) keys(id string) (string, string, string) {
	hash := sha256.Sum256([]byte(id))
	job := r.prefix + "job:" + hex.EncodeToString(hash[:])
	return job, job + ":events", r.prefix + "pending"
}

func (r *RedisBackgroundRepository) seal(id, kind string, value any) ([]byte, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	sealed := make([]byte, 1+r.aead.NonceSize())
	sealed[0] = 1
	if _, err := rand.Read(sealed[1:]); err != nil {
		return nil, err
	}
	return r.aead.Seal(sealed, sealed[1:], plain, []byte(r.prefix+kind+":"+id)), nil
}

func (r *RedisBackgroundRepository) open(id, kind string, sealed []byte, value any) error {
	n := 1 + r.aead.NonceSize()
	if len(sealed) < n+r.aead.Overhead() || sealed[0] != 1 {
		return errors.New("live: invalid encrypted background data")
	}
	plain, err := r.aead.Open(nil, sealed[1:n], sealed[n:], []byte(r.prefix+kind+":"+id))
	if err != nil {
		return errors.New("live: background data authentication failed")
	}
	if err := json.Unmarshal(plain, value); err != nil {
		return fmt.Errorf("live: invalid background data: %w", err)
	}
	return nil
}

// Mutation replies contain one result code followed by an atomic HGETALL.
func backgroundReply(result any, err error) (map[string]string, error) {
	if err != nil {
		return nil, err
	}
	values, ok := result.([]any)
	if !ok || len(values) == 0 {
		return nil, errors.New("live: invalid background repository reply")
	}
	code, ok := values[0].(int64)
	if !ok {
		return nil, errors.New("live: invalid background repository result")
	}
	switch code {
	case 0:
		return nil, ErrNotFound
	case -1:
		return nil, ErrBackgroundLeaseConflict
	case -2:
		return nil, errors.New("live: background job already exists")
	case 1:
	default:
		return nil, errors.New("live: invalid background repository result")
	}
	if (len(values)-1)%2 != 0 {
		return nil, errors.New("live: invalid background repository fields")
	}
	fields := make(map[string]string, (len(values)-1)/2)
	for i := 1; i < len(values); i += 2 {
		key, keyOK := values[i].(string)
		value, valueOK := values[i+1].(string)
		if !keyOK || !valueOK {
			return nil, errors.New("live: invalid background repository field")
		}
		fields[key] = value
	}
	return fields, nil
}

func (r *RedisBackgroundRepository) decode(id string, fields map[string]string) (BackgroundJob, error) {
	if len(fields) == 0 {
		return BackgroundJob{}, ErrNotFound
	}
	var job BackgroundJob
	if err := r.open(id, "job", []byte(fields["data"]), &job); err != nil {
		return BackgroundJob{}, err
	}
	if job.ID != id || fields["id"] != id || fields["user"] != strconv.FormatInt(job.UserID, 10) || fields["key"] != strconv.FormatInt(job.KeyID, 10) {
		return BackgroundJob{}, errors.New("live: background job identity mismatch")
	}
	leaseUntil, err := strconv.ParseInt(fields["lease_until"], 10, 64)
	if err != nil {
		return BackgroundJob{}, errors.New("live: invalid background lease expiry")
	}
	updated, err := strconv.ParseInt(fields["updated"], 10, 64)
	if err != nil {
		return BackgroundJob{}, errors.New("live: invalid background update time")
	}
	job.LeaseID = fields["lease"]
	job.LeaseUntil = time.Time{}
	if leaseUntil > 0 {
		job.LeaseUntil = time.UnixMilli(leaseUntil).UTC()
	}
	job.CancelRequested = fields["cancel"] == "1"
	job.Billed = fields["billed"] == "1"
	if fields["import_hash"] == "" {
		job.UpdatedAt = time.UnixMilli(updated).UTC()
	} else if job.UpdatedAt.UnixMilli() != updated {
		return BackgroundJob{}, errors.New("live: imported background update time mismatch")
	}
	return job, nil
}
