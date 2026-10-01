package legacy

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/tidwall/gjson"
)

const backgroundSourceTestSecret = "offline-background-test-secret"

func sealSourceBackground(t *testing.T, plain string) string {
	t.Helper()
	if plain == "" {
		return ""
	}
	key := sha256.Sum256([]byte(backgroundSourceTestSecret))
	crypto, err := catalog.NewAESGCM(key[:])
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := crypto.Encrypt([]byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	return "enc:v1:" + base64.RawURLEncoding.EncodeToString(sealed)
}

func sourceBackgroundFixture(t *testing.T, status string) (sourceBackgroundJob, []sourceBackgroundEvent) {
	t.Helper()
	id := "resp_bg_legacy_" + status
	created := time.Date(2026, 9, 20, 4, 5, 6, 123456000, time.UTC)
	job := sourceBackgroundJob{ID: id, UserID: 7, TokenID: 11, Model: "chat-model", Status: status, Stream: true, Native: true,
		ChannelID: 13, UpstreamID: "resp_original_" + status, UpstreamSequence: 9, LastSequence: 1, CreatedAt: created, UpdatedAt: created.Add(time.Minute),
		RoutingCiphertext:       sealSourceBackground(t, `{"using_group":"default","token_group":"default"}`),
		FinalResponseCiphertext: sealSourceBackground(t, `{"id":"`+id+`","model":"chat-model","status":"`+status+`","output":[{"text":"PRIVATE_RESULT"}],"error":null}`)}
	if status == "failed" {
		job.ErrorCiphertext = sealSourceBackground(t, `{"type":"server_error","message":"ORIGINAL_PRIVATE_ERROR"}`)
	}
	if status == "cancelled" {
		job.CancelRequested = true
	}
	events := []sourceBackgroundEvent{
		{ID: 1, JobID: id, Sequence: 0, Type: "response.created", CreatedAt: created,
			PayloadCiphertext: sealSourceBackground(t, `{"type":"response.created","sequence_number":0,"response":{"id":"`+id+`"}}`)},
		{ID: 2, JobID: id, Sequence: 1, Type: "response." + status, CreatedAt: created.Add(time.Second),
			PayloadCiphertext: sealSourceBackground(t, `{"type":"response.`+status+`","sequence_number":1,"response":{"id":"`+id+`","status":"`+status+`"}}`)},
	}
	return job, events
}

func TestBackgroundProjectionRetainsTerminalResponseAndExactCursor(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled"} {
		source, events := sourceBackgroundFixture(t, status)
		asset, err := projectBackground(source, []sourceBackgroundEvent{events[1], events[0]}, backgroundSourceTestSecret)
		if err != nil {
			t.Fatal(err)
		}
		job := asset.job
		if job.ID != source.ID || job.Status != status || job.KeyID != 11 || job.UserID != 7 || job.Model != source.Model || job.ChannelID != 13 ||
			job.Group != "default" || job.UpstreamID != source.UpstreamID || job.LastUpstreamSequence != 9 || job.CredentialID != 0 || !job.Billed ||
			!job.CreatedAt.Equal(source.CreatedAt) || !job.UpdatedAt.Equal(source.UpdatedAt) || len(job.Body) != 0 || len(job.Reservation) != 0 || job.Error != "" {
			t.Fatal("terminal source metadata or execution isolation changed")
		}
		if !strings.Contains(string(job.Snapshot), "PRIVATE_RESULT") || len(asset.events) != 2 || asset.events[1].Sequence != 1 {
			t.Fatal("terminal result or event cursor changed")
		}
		if status == "failed" && gjson.GetBytes(job.Snapshot, "error.message").Str != "ORIGINAL_PRIVATE_ERROR" {
			t.Fatal("original source error was replaced")
		}
	}
	job, _ := sourceBackgroundFixture(t, "cancelled")
	job.FinalResponseCiphertext, job.LastSequence = "", -1
	asset, err := projectBackground(job, nil, backgroundSourceTestSecret)
	if err != nil || gjson.GetBytes(asset.job.Snapshot, "status").Str != "cancelled" || len(asset.events) != 0 {
		t.Fatal("source cancellation before execution lost fallback result")
	}
}

func TestBackgroundProjectionRefusesCorruptionAndEventGaps(t *testing.T) {
	for _, corrupt := range []string{"wrong_key", "final_cipher", "error_cipher", "error_disagreement", "routing_cipher", "event_cipher", "event_gap", "foreign_response", "cursor", "fractional_sequence"} {
		t.Run(corrupt, func(t *testing.T) {
			job, events := sourceBackgroundFixture(t, "completed")
			secret := backgroundSourceTestSecret
			switch corrupt {
			case "wrong_key":
				secret = "wrong-source-secret"
			case "final_cipher":
				job.FinalResponseCiphertext = "enc:v1:malformed"
			case "error_cipher":
				job.ErrorCiphertext = "enc:v1:malformed"
			case "error_disagreement":
				job.FinalResponseCiphertext = `{"id":"` + job.ID + `","status":"completed","error":{"code":"PRIVATE_ERROR_ONE"}}`
				job.ErrorCiphertext = `{"code":"PRIVATE_ERROR_TWO"}`
			case "routing_cipher":
				job.RoutingCiphertext = "enc:v1:malformed"
			case "event_cipher":
				events[0].PayloadCiphertext = "enc:v1:malformed"
			case "event_gap":
				events[1].Sequence = 2
			case "foreign_response":
				events[0].PayloadCiphertext = `{"type":"response.created","sequence_number":0,"response":{"id":"foreign"}}`
			case "cursor":
				job.LastSequence = 3
			case "fractional_sequence":
				events[0].PayloadCiphertext = `{"type":"response.created","sequence_number":0.0}`
			}
			if _, err := projectBackground(job, events, secret); err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("invalid history accepted or source content exposed")
			}
		})
	}
}
