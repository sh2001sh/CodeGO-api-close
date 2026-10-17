package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/stretchr/testify/require"
)

func resetIdentityProofMemory(t *testing.T) {
	t.Helper()
	oldRedis, oldRDB := platformcache.RedisEnabled, platformcache.RDB
	platformcache.RedisEnabled = false
	verificationMutex.Lock()
	oldCodes, oldReplays := verificationMap, telegramReplays
	verificationMap, telegramReplays = make(map[string]verificationValue), make(map[string]time.Time)
	verificationMutex.Unlock()
	t.Cleanup(func() {
		platformcache.RedisEnabled, platformcache.RDB = oldRedis, oldRDB
		verificationMutex.Lock()
		verificationMap, telegramReplays = oldCodes, oldReplays
		verificationMutex.Unlock()
	})
}

func TestVerificationCodesConsumeAtomicallyAndScopePurpose(t *testing.T) {
	resetIdentityProofMemory(t)
	require.NoError(t, RegisterVerificationCodeWithKey(" User@Example.com ", "123456", EmailVerificationPurpose))
	require.NoError(t, RegisterVerificationCodeWithKey("user@example.com", "123456", PasswordResetPurpose))
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if VerifyCodeWithKey("USER@example.com", "123456", EmailVerificationPurpose) {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, accepted.Load())
	require.False(t, VerifyCodeWithKey("user@example.com", "123456", EmailVerificationPurpose))
	require.True(t, VerifyCodeWithKey("user@example.com", "123456", PasswordResetPurpose))
}

func TestVerificationCodesFailureExpiryAndReplacement(t *testing.T) {
	resetIdentityProofMemory(t)
	const email = "attempt@example.com"
	require.NoError(t, RegisterVerificationCodeWithKey(email, "123456", EmailVerificationPurpose))
	for i := 0; i < verificationMaxAttempts; i++ {
		require.False(t, VerifyCodeWithKey(email, "bad", EmailVerificationPurpose))
	}
	require.False(t, VerifyCodeWithKey(email, "123456", EmailVerificationPurpose), "exhausted code remained usable")
	require.NoError(t, RegisterVerificationCodeWithKey(email, "old", EmailVerificationPurpose))
	require.NoError(t, RegisterVerificationCodeWithKey(email, "new", EmailVerificationPurpose))
	require.False(t, VerifyCodeWithKey(email, "old", EmailVerificationPurpose))
	require.True(t, VerifyCodeWithKey(email, "new", EmailVerificationPurpose))
	require.NoError(t, RegisterVerificationCodeWithKey(email, "expired", EmailVerificationPurpose))
	key := verificationStorageKey(email, EmailVerificationPurpose)
	verificationMutex.Lock()
	v := verificationMap[key]
	v.time = time.Now().Add(-time.Duration(VerificationValidMinutes)*time.Minute - time.Second)
	verificationMap[key] = v
	verificationMutex.Unlock()
	require.False(t, VerifyCodeWithKey(email, "expired", EmailVerificationPurpose))
	require.Error(t, RegisterVerificationCodeWithKey("", "code", EmailVerificationPurpose))
	require.Error(t, RegisterVerificationCodeWithKey(email, "", EmailVerificationPurpose))
	require.False(t, VerifyCodeWithKey("", "", EmailVerificationPurpose))
}

func TestVerificationCodesRedisUnavailableFailsClosed(t *testing.T) {
	resetIdentityProofMemory(t)
	require.NoError(t, RegisterVerificationCodeWithKey("local@example.com", "123456", EmailVerificationPurpose))
	platformcache.RedisEnabled, platformcache.RDB = true, nil
	require.Error(t, RegisterVerificationCodeWithKey("local@example.com", "123456", EmailVerificationPurpose))
	require.False(t, VerifyCodeWithKey("local@example.com", "123456", EmailVerificationPurpose))
	require.False(t, claimTelegramAuthorization("signed", time.Now().Add(time.Minute)))
}

func signTelegramProof(values map[string][]string, token string) {
	var lines []string
	for key, value := range values {
		if key != "hash" {
			lines = append(lines, key+"="+value[0])
		}
	}
	sort.Strings(lines)
	secret := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(strings.Join(lines, "\n")))
	values["hash"] = []string{hex.EncodeToString(mac.Sum(nil))}
}

func TestTelegramProofRejectsExpiredFutureMalformedAndReplay(t *testing.T) {
	resetIdentityProofMemory(t)
	const token = "local-test-bot-token"
	for i, tc := range []struct {
		name   string
		change func(map[string][]string)
	}{
		{"expired", func(v map[string][]string) {
			v["auth_date"] = []string{strconv.FormatInt(time.Now().Add(-6*time.Minute).Unix(), 10)}
		}},
		{"future", func(v map[string][]string) {
			v["auth_date"] = []string{strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)}
		}},
		{"missing-date", func(v map[string][]string) { delete(v, "auth_date") }},
		{"nonnumeric-id", func(v map[string][]string) { v["id"] = []string{"attacker"} }},
		{"zero-id", func(v map[string][]string) { v["id"] = []string{"0"} }},
		{"duplicate-id", func(v map[string][]string) { v["id"] = append(v["id"], "123") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := map[string][]string{"id": {fmt.Sprint(900000 + i)}, "auth_date": {strconv.FormatInt(time.Now().Unix(), 10)}}
			tc.change(v)
			signTelegramProof(v, token)
			_, err := validatedTelegramID(v, token)
			require.Error(t, err)
		})
	}
	v := map[string][]string{"id": {"999888"}, "auth_date": {strconv.FormatInt(time.Now().Unix(), 10)}}
	signTelegramProof(v, token)
	_, err := validatedTelegramID(v, "wrong-bot")
	require.Error(t, err)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := validatedTelegramID(v, token); err == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, accepted.Load())
	_, err = validatedTelegramID(v, token)
	require.Error(t, err)
}
