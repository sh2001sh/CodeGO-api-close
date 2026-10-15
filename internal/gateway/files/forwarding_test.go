package files

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gatewaySchema "github.com/sh2001sh/new-api/internal/gateway/schema"
	"github.com/stretchr/testify/require"
)

func TestPrepareFileIDsPrefersNativeAndDeduplicatesReferences(t *testing.T) {
	setupFileTestDB(t)
	file, err := Create(7, "image.png", "vision", "image/png", bytes.NewReader([]byte("image")), 1024)
	require.NoError(t, err)
	var uploads atomic.Int32
	raw := []byte(`{"input":[{"type":"input_image","file_id":"` + file.ID + `"},{"type":"input_image","file_id":"` + file.ID + `"}]}`)
	prepared, err := PrepareFileIDsJSON(raw, PrepareOptions{
		UserID: 7, Mode: InputModeAuto,
		NativeUpload: func(_ *gatewaySchema.UserFile) (string, error) {
			uploads.Add(1)
			return "file-upstream", nil
		},
	})
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(prepared, &payload))
	items := payload["input"].([]any)
	require.Equal(t, "file-upstream", items[0].(map[string]any)["file_id"])
	require.Equal(t, "file-upstream", items[1].(map[string]any)["file_id"])
	require.EqualValues(t, 1, uploads.Load())
}

func TestPrepareFileIDsUsesSignedURLAfterNativeFailure(t *testing.T) {
	setupFileTestDB(t)
	file, err := Create(7, "image.png", "vision", "image/png", bytes.NewReader([]byte("image")), 1024)
	require.NoError(t, err)
	prepared, err := PrepareFileIDsJSON([]byte(`{"input":[{"type":"input_image","file_id":"`+file.ID+`"}]}`), PrepareOptions{
		UserID: 7,
		Mode:   InputModeAuto,
		NativeUpload: func(_ *gatewaySchema.UserFile) (string, error) {
			return "", errors.New("unsupported")
		},
		SignedURL: func(_ *gatewaySchema.UserFile) (string, error) {
			return "https://api.example.com/v1/files/signed", nil
		},
	})
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(prepared, &payload))
	item := payload["input"].([]any)[0].(map[string]any)
	require.Equal(t, "https://api.example.com/v1/files/signed", item["image_url"])
	require.NotContains(t, item, "file_id")
}

func TestSignedDeliveryURLRejectsTamperingAndExpiry(t *testing.T) {
	t.Setenv("FILE_DELIVERY_BASE_URL", "https://api.example.com")
	t.Setenv("FILE_DELIVERY_SIGNING_SECRET", "test-signing-secret")
	now := time.Date(2026, 8, 31, 12, 2, 0, 0, time.UTC)
	fileID := "file-codego-test"
	deliveryURL, err := BuildSignedDeliveryURL(fileID, now)
	require.NoError(t, err)
	parsed, err := url.Parse(deliveryURL)
	require.NoError(t, err)
	require.NoError(t, VerifyDeliveryToken(fileID, parsed.Query().Get("expires"), parsed.Query().Get("signature"), now))
	require.ErrorIs(t, VerifyDeliveryToken(fileID+"x", parsed.Query().Get("expires"), parsed.Query().Get("signature"), now), ErrInvalidDeliveryToken)
	require.ErrorIs(t, VerifyDeliveryToken(fileID, parsed.Query().Get("expires"), parsed.Query().Get("signature"), now.Add(time.Hour)), ErrInvalidDeliveryToken)
}

func TestResolveUpstreamFileCoalescesConcurrentUploadsAndIsolatesKeys(t *testing.T) {
	setupFileTestDB(t)
	file, err := Create(7, "image.png", "vision", "image/png", bytes.NewReader([]byte("image")), 1024)
	require.NoError(t, err)
	target := UpstreamTarget{ChannelID: 9, KeyFingerprint: FingerprintCredential("key-a"), BaseURLFingerprint: FingerprintBaseURL("https://upstream.example/v1"), Protocol: "responses"}
	var uploads atomic.Int32
	start := make(chan struct{})
	upload := func() (string, error) {
		uploads.Add(1)
		<-start
		return "file-upstream-a", nil
	}
	results := make(chan string, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			id, resolveErr := ResolveUpstreamFile(file, target, upload)
			require.NoError(t, resolveErr)
			results <- id
		}()
	}
	require.Eventually(t, func() bool { return uploads.Load() == 1 }, time.Second, 10*time.Millisecond)
	close(start)
	workers.Wait()
	close(results)
	for id := range results {
		require.Equal(t, "file-upstream-a", id)
	}
	require.EqualValues(t, 1, uploads.Load())

	otherTarget := target
	otherTarget.KeyFingerprint = FingerprintCredential("key-b")
	id, err := ResolveUpstreamFile(file, otherTarget, func() (string, error) {
		uploads.Add(1)
		return "file-upstream-b", nil
	})
	require.NoError(t, err)
	require.Equal(t, "file-upstream-b", id)
	require.EqualValues(t, 2, uploads.Load())
}
