package live

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/tidwall/gjson"
)

var ErrBackgroundImportConflict = errors.New("live: background import conflicts with stored history")

var backgroundImportRead = redis.NewScript(`
return {redis.call('HGETALL', KEYS[1]), redis.call('LRANGE', KEYS[2], 0, -1),
 redis.call('PTTL', KEYS[1]), redis.call('PTTL', KEYS[2]), redis.call('ZSCORE', KEYS[3], ARGV[1]) or false}
`)

var backgroundImportWrite = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then
  if ARGV[1] ~= 'repeat' or redis.call('HLEN', KEYS[1]) ~= tonumber(ARGV[2]) then return 0 end
  if redis.call('PTTL', KEYS[1]) ~= -1 or redis.call('ZSCORE', KEYS[3], redis.call('HGET', KEYS[1], 'id')) then return 0 end
  local pos = 3
  for i = 1, tonumber(ARGV[2]) do
    if redis.call('HGET', KEYS[1], ARGV[pos]) ~= ARGV[pos+1] then return 0 end
    pos = pos + 2
  end
  local count = tonumber(ARGV[pos])
  if redis.call('LLEN', KEYS[2]) ~= count then return 0 end
  if count > 0 and redis.call('PTTL', KEYS[2]) ~= -1 then return 0 end
  for i = 1, count do
    if redis.call('LINDEX', KEYS[2], i-1) ~= ARGV[pos+i] then return 0 end
  end
  return 1
end
if ARGV[1] ~= 'create' or redis.call('EXISTS', KEYS[2]) == 1 then return 0 end
local pendingType = redis.call('TYPE', KEYS[3]).ok
if pendingType ~= 'none' and pendingType ~= 'zset' then return 0 end
redis.call('HSET', KEYS[1], 'id', ARGV[2], 'user', ARGV[3], 'key', ARGV[4],
 'data', ARGV[5], 'status', ARGV[6], 'billed', '1', 'cancel', ARGV[7],
 'lease', '', 'lease_until', '0', 'sequence', ARGV[8], 'updated', ARGV[9], 'import_hash', ARGV[10])
for i = 11, #ARGV do redis.call('RPUSH', KEYS[2], ARGV[i]) end
redis.call('ZREM', KEYS[3], ARGV[2])
return 1
`)

// ImportTerminalJob atomically installs previously settled history. Source v2
// history has no expiry, so imported results remain persistent. This is an
// offline asset operation, never part of a PostgreSQL transaction or billing.
func (r *RedisBackgroundRepository) ImportTerminalJob(ctx context.Context, job BackgroundJob, events []BackgroundEvent) error {
	digest, err := backgroundImportDigest(job, events)
	if err != nil {
		return err
	}
	fields, stored, err := r.readBackgroundImport(ctx, job.ID)
	if err != nil {
		return err
	}
	var args []any
	if len(fields) != 0 {
		if err := r.matchBackgroundImport(job, events, digest, fields, stored); err != nil {
			return err
		}
		args = []any{"repeat", len(fields)}
		for name, value := range fields {
			args = append(args, name, value)
		}
		args = append(args, len(stored))
		for _, value := range stored {
			args = append(args, value)
		}
	} else {
		sealed, err := r.seal(job.ID, "job", job)
		if err != nil {
			return err
		}
		args = []any{"create", job.ID, job.UserID, job.KeyID, sealed, job.Status,
			backgroundFlag(job.CancelRequested), len(events) - 1, job.UpdatedAt.UnixMilli(), digest}
		for _, event := range events {
			value, err := r.seal(job.ID, "event", event)
			if err != nil {
				return err
			}
			args = append(args, value)
		}
	}
	key, eventKey, pending := r.keys(job.ID)
	result, err := backgroundImportWrite.Run(ctx, r.client, []string{key, eventKey, pending}, args...).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		// Another copier may have installed identical facts after our read.
		return r.VerifyTerminalJob(ctx, job, events)
	}
	return nil
}

// VerifyTerminalJob checks pre-copied offline assets without changing Redis.
func (r *RedisBackgroundRepository) VerifyTerminalJob(ctx context.Context, job BackgroundJob, events []BackgroundEvent) error {
	digest, err := backgroundImportDigest(job, events)
	if err != nil {
		return err
	}
	fields, stored, err := r.readBackgroundImport(ctx, job.ID)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return ErrNotFound
	}
	return r.matchBackgroundImport(job, events, digest, fields, stored)
}

func backgroundImportDigest(job BackgroundJob, events []BackgroundEvent) (string, error) {
	terminal := job.Status == "completed" || job.Status == "failed" || job.Status == "cancelled"
	if job.ID == "" || job.UserID <= 0 || job.KeyID <= 0 || job.Model == "" || !terminal || !job.Billed ||
		job.CreatedAt.IsZero() || job.UpdatedAt.Before(job.CreatedAt) || job.LeaseID != "" || !job.LeaseUntil.IsZero() ||
		len(job.Body) != 0 || len(job.Reservation) != 0 || len(job.PricingHeaders) != 0 || job.Error != "" {
		return "", errors.New("live: import requires settled terminal history without execution secrets or leases")
	}
	snapshot := gjson.ParseBytes(job.Snapshot)
	if !gjson.ValidBytes(job.Snapshot) || !snapshot.IsObject() || snapshot.Get("id").Str != job.ID || snapshot.Get("status").Str != job.Status {
		return "", errors.New("live: invalid imported background snapshot")
	}
	for i, event := range events {
		payload := gjson.ParseBytes(event.Payload)
		if event.Sequence != int64(i) || event.Type == "" || strings.ContainsAny(event.Type, "\r\n") ||
			!gjson.ValidBytes(event.Payload) || !payload.IsObject() || payload.Get("type").Str != event.Type ||
			payload.Get("sequence_number").Raw != strconv.FormatInt(event.Sequence, 10) {
			return "", errors.New("live: invalid imported background event or noncontiguous sequence")
		}
		if id := payload.Get("response.id"); id.Exists() && id.Str != job.ID {
			return "", errors.New("live: imported event belongs to another response")
		}
	}
	if events == nil {
		events = []BackgroundEvent{}
	}
	data, err := json.Marshal(struct {
		Job    BackgroundJob
		Events []BackgroundEvent
	}{job, events})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (r *RedisBackgroundRepository) readBackgroundImport(ctx context.Context, id string) (map[string]string, []string, error) {
	key, eventKey, pending := r.keys(id)
	values, err := backgroundImportRead.Run(ctx, r.client, []string{key, eventKey, pending}, id).Slice()
	if err != nil {
		return nil, nil, err
	}
	if len(values) != 5 {
		return nil, nil, ErrBackgroundImportConflict
	}
	hash, hashOK := values[0].([]any)
	list, listOK := values[1].([]any)
	if !hashOK || !listOK || len(hash)%2 != 0 {
		return nil, nil, ErrBackgroundImportConflict
	}
	if len(hash) > 0 && (values[2] != int64(-1) || (len(list) > 0 && values[3] != int64(-1)) || values[4] != nil) {
		return nil, nil, ErrBackgroundImportConflict
	}
	fields := make(map[string]string, len(hash)/2)
	for i := 0; i < len(hash); i += 2 {
		name, nameOK := hash[i].(string)
		value, valueOK := hash[i+1].(string)
		if !nameOK || !valueOK {
			return nil, nil, ErrBackgroundImportConflict
		}
		fields[name] = value
	}
	stored := make([]string, len(list))
	for i, item := range list {
		value, ok := item.(string)
		if !ok {
			return nil, nil, ErrBackgroundImportConflict
		}
		stored[i] = value
	}
	return fields, stored, nil
}

func (r *RedisBackgroundRepository) matchBackgroundImport(job BackgroundJob, events []BackgroundEvent, digest string, fields map[string]string, stored []string) error {
	if fields["import_hash"] != digest || len(stored) != len(events) || fields["id"] != job.ID ||
		fields["user"] != strconv.FormatInt(job.UserID, 10) || fields["key"] != strconv.FormatInt(job.KeyID, 10) ||
		fields["status"] != job.Status || fields["billed"] != "1" || fields["cancel"] != backgroundFlag(job.CancelRequested) ||
		fields["updated"] != strconv.FormatInt(job.UpdatedAt.UnixMilli(), 10) || fields["sequence"] != strconv.Itoa(len(events)-1) ||
		fields["lease"] != "" || fields["lease_until"] != "0" {
		return ErrBackgroundImportConflict
	}
	var actualJob BackgroundJob
	if err := r.open(job.ID, "job", []byte(fields["data"]), &actualJob); err != nil {
		return err
	}
	// RawMessage's zero value is encoded as JSON null by the shared job seal.
	if string(actualJob.Reservation) == "null" {
		actualJob.Reservation = nil
	}
	actualEvents := make([]BackgroundEvent, len(stored))
	for i, value := range stored {
		if err := r.open(job.ID, "event", []byte(value), &actualEvents[i]); err != nil {
			return err
		}
	}
	actualDigest, err := backgroundImportDigest(actualJob, actualEvents)
	if err != nil || actualDigest != digest {
		return ErrBackgroundImportConflict
	}
	return nil
}
