package catalog

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Decrypter opens a credential secret sealed with AES-256-GCM (or an
// equivalent AEAD). Compile receives one from its caller so this package
// never hard-codes a key source (env, KMS, ...).
type Decrypter interface {
	Decrypt(ciphertext []byte) ([]byte, error)
}

// Encrypter is the write-side counterpart of Decrypter. It seals credential
// secrets before they are persisted to PostgreSQL or Redis.
type Encrypter interface {
	Encrypt(plaintext []byte) ([]byte, error)
}

// AESGCM seals and opens secrets with AES-256-GCM. Ciphertext is
// nonce||sealed so callers only ever pass one blob around.
type AESGCM struct {
	gcm cipher.AEAD
	key [32]byte
}

// NewAESGCMFromBase64 builds an AESGCM from a base64 (std encoding) 32-byte
// key, the form used in V3_SECRET_KEY.
func NewAESGCMFromBase64(s string) (*AESGCM, error) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("catalog: secret key is not valid base64: %w", err)
	}
	return NewAESGCM(key)
}

// NewAESGCM builds an AESGCM from a 32-byte AES-256 key.
func NewAESGCM(key []byte) (*AESGCM, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("catalog: AES-256 key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("catalog: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("catalog: new gcm: %w", err)
	}
	a := &AESGCM{gcm: gcm}
	copy(a.key[:], key)
	return a, nil
}

// DeriveKey gives shared processes a stable, purpose-separated key for encrypted
// background jobs and signed file delivery without reusing credential ciphertext.
func (a *AESGCM) DeriveKey(purpose string) []byte {
	mac := hmac.New(sha256.New, a.key[:])
	_, _ = mac.Write([]byte("codego-v3:" + purpose))
	return mac.Sum(nil)
}

// Encrypt seals plaintext with a fresh random nonce, prefixed to the result.
func (a *AESGCM) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, a.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("catalog: read nonce: %w", err)
	}
	return a.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens a nonce||sealed blob produced by Encrypt (or the control
// plane's admin API, which uses the same scheme). Truncated or tampered
// input is rejected.
func (a *AESGCM) Decrypt(ciphertext []byte) ([]byte, error) {
	n := a.gcm.NonceSize()
	if len(ciphertext) < n {
		return nil, errors.New("catalog: ciphertext shorter than nonce")
	}
	nonce, sealed := ciphertext[:n], ciphertext[n:]
	plaintext, err := a.gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: decrypt: %w", err)
	}
	return plaintext, nil
}
