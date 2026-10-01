package catalog

import (
	"bytes"
	"testing"
)

func TestAESGCMRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	a, err := NewAESGCM(key)
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}

	plaintext := []byte("sk-super-secret-credential")
	ciphertext, err := a.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext must not equal plaintext")
	}

	got, err := a.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("got %q, want %q", got, plaintext)
	}
}

func TestAESGCMDistinctNonces(t *testing.T) {
	key := bytes.Repeat([]byte{0x01}, 32)
	a, err := NewAESGCM(key)
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}
	c1, _ := a.Encrypt([]byte("same input"))
	c2, _ := a.Encrypt([]byte("same input"))
	if bytes.Equal(c1, c2) {
		t.Fatal("two encryptions of the same plaintext must differ (nonce reuse)")
	}
}

func TestAESGCMTamperDetected(t *testing.T) {
	key := bytes.Repeat([]byte{0x7f}, 32)
	a, err := NewAESGCM(key)
	if err != nil {
		t.Fatalf("NewAESGCM: %v", err)
	}
	ciphertext, err := a.Encrypt([]byte("do not trust this"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	tampered := append([]byte{}, ciphertext...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := a.Decrypt(tampered); err == nil {
		t.Fatal("expected tamper detection to fail decryption")
	}
}

func TestAESGCMWrongKeyRejected(t *testing.T) {
	a, _ := NewAESGCM(bytes.Repeat([]byte{0x01}, 32))
	b, _ := NewAESGCM(bytes.Repeat([]byte{0x02}, 32))
	ciphertext, err := a.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := b.Decrypt(ciphertext); err == nil {
		t.Fatal("expected decryption with the wrong key to fail")
	}
}

func TestAESGCMRejectsShortCiphertext(t *testing.T) {
	a, _ := NewAESGCM(bytes.Repeat([]byte{0x03}, 32))
	if _, err := a.Decrypt([]byte("short")); err == nil {
		t.Fatal("expected an error for ciphertext shorter than the nonce")
	}
}

func TestNewAESGCMRejectsBadKeyLength(t *testing.T) {
	if _, err := NewAESGCM([]byte("too-short")); err == nil {
		t.Fatal("expected an error for a non-32-byte key")
	}
}
