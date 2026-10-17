package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

const (
	EmailVerificationPurpose      = "v"
	PasswordResetPurpose          = "r"
	WalletTransferPasswordPurpose = "wallet-transfer-password:"
	verificationMaxAttempts       = 5
	verificationMapMaxSize        = 10000
)

type verificationValue struct {
	code     string
	time     time.Time
	attempts int
}

var verificationMutex sync.Mutex
var verificationMap = make(map[string]verificationValue)
var VerificationValidMinutes = 10

// GenerateVerificationCode returns a UUID-derived verification token or its prefix.
func GenerateVerificationCode(length int) string {
	code := strings.ReplaceAll(uuid.New().String(), "-", "")
	if length <= 0 || length > len(code) {
		return code
	}
	return code[:length]
}

func verificationStorageKey(key, purpose string) string {
	return fmt.Sprintf("new-api:verification:v2:%x", sha256.Sum256([]byte(purpose+"\x00"+strings.ToLower(strings.TrimSpace(key)))))
}

func verificationCodeHash(code string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(code)))
}

// RegisterVerificationCodeWithKey replaces a purpose-scoped, single-use code.
// Redis errors fail closed rather than creating process-local alternatives.
func RegisterVerificationCodeWithKey(key, code, purpose string) error {
	if strings.TrimSpace(key) == "" || code == "" {
		return errors.New("invalid verification code")
	}
	storageKey := verificationStorageKey(key, purpose)
	ttl := time.Duration(VerificationValidMinutes) * time.Minute
	if platformcache.RedisEnabled {
		if platformcache.RDB == nil {
			return errors.New("verification store unavailable")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := platformcache.RDB.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(ctx, storageKey, "code", verificationCodeHash(code), "attempts", 0)
			pipe.Expire(ctx, storageKey, ttl)
			return nil
		})
		return err
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	removeExpiredVerificationPairs()
	if _, exists := verificationMap[storageKey]; !exists && len(verificationMap) >= verificationMapMaxSize {
		return errors.New("verification store unavailable")
	}
	verificationMap[storageKey] = verificationValue{code: verificationCodeHash(code), time: time.Now()}
	return nil
}

var consumeVerificationCode = redis.NewScript(`
local stored = redis.call('HGET', KEYS[1], 'code')
if not stored then return 0 end
if stored == ARGV[1] then redis.call('DEL', KEYS[1]); return 1 end
local attempts = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
if attempts >= tonumber(ARGV[2]) then redis.call('DEL', KEYS[1]) end
return 0
`)

// VerifyCodeWithKey consumes a matching code atomically. Each issued code permits
// at most five failed attempts; callers must not verify a successful code twice.
func VerifyCodeWithKey(key, code, purpose string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	storageKey := verificationStorageKey(key, purpose)
	hash := verificationCodeHash(code)
	if platformcache.RedisEnabled {
		if platformcache.RDB == nil {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		result, err := consumeVerificationCode.Run(ctx, platformcache.RDB, []string{storageKey}, hash, verificationMaxAttempts).Int()
		if err != nil {
			platformobservability.SysLog("verification code store unavailable")
		}
		return err == nil && result == 1
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, ok := verificationMap[storageKey]
	if !ok {
		return false
	}
	if time.Since(value.time) >= time.Duration(VerificationValidMinutes)*time.Minute {
		delete(verificationMap, storageKey)
		return false
	}
	if code != "" && subtle.ConstantTimeCompare([]byte(hash), []byte(value.code)) == 1 {
		delete(verificationMap, storageKey)
		return true
	}
	value.attempts++
	if value.attempts >= verificationMaxAttempts {
		delete(verificationMap, storageKey)
	} else {
		verificationMap[storageKey] = value
	}
	return false
}

func DeleteVerificationKey(key, purpose string) {
	storageKey := verificationStorageKey(key, purpose)
	if platformcache.RedisEnabled {
		if platformcache.RDB == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := platformcache.RDB.Del(ctx, storageKey).Err(); err != nil {
			platformobservability.SysLog("verification code deletion unavailable")
		}
		return
	}
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, storageKey)
}

func removeExpiredVerificationPairs() {
	for key, value := range verificationMap {
		if time.Since(value.time) >= time.Duration(VerificationValidMinutes)*time.Minute {
			delete(verificationMap, key)
		}
	}
}
