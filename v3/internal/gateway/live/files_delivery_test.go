package live

import (
	"bytes"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFilesSignedDeliveryTamperingExpiryAndUnavailableKeys(t *testing.T) {
	store := filesTestStore(t)
	file := filesTestCreate(t, store, 11, []byte{0, 1, 255, 128})
	key := bytes.Repeat([]byte{0x51}, 32)
	mux := filesTestMux(t, store, 1024, key)
	now := time.Now().UTC()
	full, err := BuildSignedFileDeliveryURL("https://example.test/v1", file.ID, key, now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(full)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/v1/files/"+file.ID+"/delivery" {
		t.Fatalf("delivery path=%s", parsed.Path)
	}
	w := filesTestRequest(mux, "GET", parsed.RequestURI(), "", nil, "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), []byte{0, 1, 255, 128}) {
		t.Fatalf("signed delivery=%d %x", w.Code, w.Body.Bytes())
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "immutable") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("missing delivery headers: %v", w.Header())
	}
	q := parsed.Query()
	if err := VerifyFileDeliveryToken(file.ID, q.Get("expires"), q.Get("signature"), key, now.Add(time.Hour)); !errors.Is(err, ErrFileDeliveryToken) {
		t.Fatalf("expired token err=%v", err)
	}
	for _, mutate := range []func(url.Values){
		func(q url.Values) { q.Set("signature", strings.Repeat("0", 64)) },
		func(q url.Values) { q.Set("expires", "1") },
		func(q url.Values) { q.Set("expires", "999999999999999999") },
		func(q url.Values) { q.Del("signature") },
	} {
		q := parsed.Query()
		mutate(q)
		w := filesTestRequest(mux, "GET", parsed.Path+"?"+q.Encode(), "", nil, "")
		if w.Code != 404 {
			t.Errorf("invalid delivery query %v status=%d", q, w.Code)
		}
	}
	for _, weak := range [][]byte{nil, []byte("short")} {
		if _, err := BuildSignedFileDeliveryURL("https://example.test", file.ID, weak, now); err == nil {
			t.Error("weak key accepted")
		}
		weakMux := filesTestMux(t, store, 1024, weak)
		if w := filesTestRequest(weakMux, "GET", parsed.RequestURI(), "", nil, ""); w.Code != 404 {
			t.Errorf("weak key delivery status=%d", w.Code)
		}
	}
	q = parsed.Query()
	if err := VerifyFileDeliveryToken("file-codego-"+strings.Repeat("1", 32), q.Get("expires"), q.Get("signature"), key, now); err == nil {
		t.Fatal("signature did not bind file ID")
	}
}

func TestFilesDeliveryPreservesConfiguredTTL(t *testing.T) {
	t.Setenv("FILE_DELIVERY_TTL_MINUTES", "1")
	id := localFilePrefix + strings.Repeat("1", 32)
	key := bytes.Repeat([]byte{0x31}, 32)
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	raw, err := BuildSignedFileDeliveryURL("https://example.test", id, key, now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if err := VerifyFileDeliveryToken(id, query.Get("expires"), query.Get("signature"), key, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("configured delivery expired too early: %v", err)
	}
	if err := VerifyFileDeliveryToken(id, query.Get("expires"), query.Get("signature"), key, now.Add(7*time.Minute)); !errors.Is(err, ErrFileDeliveryToken) {
		t.Fatalf("configured delivery did not expire: %v", err)
	}
	t.Setenv("FILE_DELIVERY_TTL_MINUTES", "-1")
	if _, err := BuildSignedFileDeliveryURL("https://example.test", id, key, now); err == nil {
		t.Fatal("invalid delivery TTL accepted")
	}
	if err := VerifyFileDeliveryToken(id, query.Get("expires"), query.Get("signature"), key, now); err == nil {
		t.Fatal("invalid delivery configuration accepted token")
	}
}
