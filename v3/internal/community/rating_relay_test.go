package community

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const relayTestSecret = "rating-relay-test-secret-of-at-least-thirty-two-characters"

func relayTestLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRatingRelayRequiresTrustedEndpoint(t *testing.T) {
	for _, endpoint := range []string{
		"http://community.example.com/api/codego/rating-events", "http://169.254.169.254/latest/", "ftp://localhost/event",
		"https://user:password@example.com/event", "https://example.com/event?secret=1", "https://example.com/event?",
		"https://example.com/event#fragment", "https://example.com/event#", "/api/codego/rating-events", "https://",
	} {
		if _, err := NewRatingRelay(nil, endpoint, relayTestSecret, relayTestLog()); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
	for _, endpoint := range []string{
		"https://community.example.com/api/codego/rating-events", "http://localhost:8081/api/codego/rating-events",
		"http://127.0.0.1:8081/event", "http://[::1]:8081/event", "http://192.168.1.2/event", "http://host.docker.internal:8081/event",
		"http://nodebb:4567/api/codego/rating-events", "http://codego-community:4567/event",
	} {
		if _, err := NewRatingRelay(nil, endpoint, relayTestSecret, relayTestLog()); err != nil {
			t.Fatalf("trusted endpoint rejected: %s: %v", endpoint, err)
		}
	}
	if _, err := NewRatingRelay(nil, "https://community.example.com/event", "short", nil); err == nil {
		t.Fatal("short secret accepted")
	}
}

func TestRatingRelaySendsRawSignedVendorJSON(t *testing.T) {
	var requests atomic.Int64
	now := time.Unix(1800000000, 0)
	event := ratingEvent{ChannelID: "channel-1", OwnerSub: "ABC234", Version: "9007199254740993"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		if req.Method != http.MethodPost || req.URL.Path != "/api/codego/rating-events" ||
			req.Header.Get("Authorization") != "Bearer "+relayTestSecret ||
			req.Header.Get("Content-Type") != "application/vnd.codego.rating-event+json" || req.Header.Get("X-CodeGo-Timestamp") != "1800000000" {
			t.Error("request did not match authenticated raw JSON contract")
			w.WriteHeader(401)
			return
		}
		mac := hmac.New(sha256.New, []byte(relayTestSecret))
		_, _ = mac.Write([]byte(req.Header.Get("X-CodeGo-Timestamp") + "."))
		_, _ = mac.Write(body)
		if req.Header.Get("X-CodeGo-Signature") != hex.EncodeToString(mac.Sum(nil)) {
			t.Error("raw body signature mismatch")
			w.WriteHeader(401)
			return
		}
		var got ratingEvent
		if err := json.Unmarshal(body, &got); err != nil || got != event {
			t.Errorf("event mismatch: %s", body)
			w.WriteHeader(400)
			return
		}
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer server.Close()
	relay, err := NewRatingRelay(nil, server.URL+"/api/codego/rating-events", relayTestSecret, relayTestLog())
	if err != nil {
		t.Fatal(err)
	}
	relay.now = func() time.Time { return now }
	if err := relay.send(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func TestRatingRelayRejectsRedirectsAndInvalidAcknowledgements(t *testing.T) {
	var redirectHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		redirectHits.Add(1)
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer target.Close()
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		location string
	}{
		{"redirect", 302, "", target.URL}, {"server failure", 503, `{"success":true}`, ""},
		{"negative acknowledgement", 200, `{"success":false}`, ""}, {"malformed", 200, "not-json", ""},
		{"trailing JSON", 200, `{"success":true}{"error":true}`, ""}, {"oversized", 200, `{"success":true}` + strings.Repeat(" ", 4096), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if tc.location != "" {
					w.Header().Set("Location", tc.location)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			relay, err := NewRatingRelay(nil, server.URL, relayTestSecret, relayTestLog())
			if err != nil {
				t.Fatal(err)
			}
			if err := relay.send(context.Background(), ratingEvent{ChannelID: "1", OwnerSub: "ABC234", Version: "1"}); err == nil || strings.Contains(err.Error(), relayTestSecret) || strings.Contains(err.Error(), server.URL) {
				t.Fatalf("expected sanitized delivery error, got %v", err)
			}
		})
	}
	if redirectHits.Load() != 0 {
		t.Fatal("redirect followed with service credentials")
	}
}

func TestRatingRelayDisabledLeavesOutboxUntouched(t *testing.T) {
	relay, err := NewRatingRelay(nil, "", "", relayTestLog())
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.DeliverPending(context.Background()); err != nil {
		t.Fatal(err)
	}
}
