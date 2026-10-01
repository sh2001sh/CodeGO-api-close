package identity

import (
	"crypto/rand"
	"math/big"
	mrand "math/rand/v2"
	"time"
)

const (
	keyPrefix   = "sk-"
	keyBodyLen  = 48
	base62      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	shownPrefix = len(keyPrefix) + 4
)

// GenerateKey returns a new API key, its storage hash and its display prefix.
// Only the hash and an encrypted copy are ever persisted.
func GenerateKey() (plaintext string, hash [32]byte, displayPrefix string, err error) {
	b := make([]byte, keyBodyLen)
	limit := big.NewInt(int64(len(base62)))
	for i := range b {
		n, err := crand(limit)
		if err != nil {
			return "", [32]byte{}, "", err
		}
		b[i] = base62[n]
	}
	plaintext = keyPrefix + string(b)
	return plaintext, HashKey(plaintext), plaintext[:shownPrefix], nil
}

// crand draws a uniform index with crypto/rand (no modulo bias).
func crand(limit *big.Int) (int64, error) {
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}

// jitter spreads d by ±20% so entries created together do not expire together.
func jitter(d time.Duration) time.Duration {
	spread := int64(d) / 5
	if spread <= 0 {
		return d
	}
	return d + time.Duration(mrand.Int64N(2*spread+1)-spread)
}
