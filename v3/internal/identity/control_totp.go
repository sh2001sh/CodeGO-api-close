package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// totpCode implements RFC 6238's SHA-1, 30-second, six-digit profile used by v2.
func totpCode(secret string, counter int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil || len(key) < 10 || counter < 0 {
		return "", ErrInvalidInput
	}
	var input [8]byte
	binary.BigEndian.PutUint64(input[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(input[:])
	hash := mac.Sum(nil)
	offset := hash[len(hash)-1] & 15
	value := binary.BigEndian.Uint32(hash[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000), nil
}

func totpCounter(secret, code string, now time.Time, last int64) (int64, bool) {
	code = strings.ReplaceAll(code, " ", "")
	if len(code) != 6 {
		return 0, false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	current := now.Unix() / 30
	for _, offset := range []int64{0, -1, 1} {
		counter := current + offset
		if counter <= last {
			continue
		}
		expected, err := totpCode(secret, counter)
		if err == nil && subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return counter, true
		}
	}
	return 0, false
}

func newTwoFactorSecret() (string, error) {
	key := make([]byte, 20)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key), nil
}

func twoFactorURI(secret, username string) string {
	return "otpauth://totp/" + url.PathEscape("CodeGo:"+username) + "?" + url.Values{
		"secret": {secret}, "issuer": {"CodeGo"}, "digits": {"6"}, "period": {"30"}, "algorithm": {"SHA1"},
	}.Encode()
}

func normalizeBackupCode(code string) string {
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if len(code) != 8 {
		return ""
	}
	for _, r := range code {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return code[:4] + "-" + code[4:]
}

func newBackupCodes() ([]string, error) {
	codes := make([]string, 4)
	for i := range codes {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		codes[i] = normalizeBackupCode(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	}
	return codes, nil
}
