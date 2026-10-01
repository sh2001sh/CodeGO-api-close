package vertex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestCanonicalStorageContentKeepsCredentialTransportAndAuthentication(t *testing.T) {
	const operation = "projects/fixture/locations/global/publishers/google/models/veo/operations/task"
	target := gateway.Target{ChannelID: 11, CredentialID: 12, Provider: "vertex", UpstreamModel: "veo", BaseURL: "https://aiplatform.googleapis.com",
		Secret:   `{"project_id":"fixture","access_token":"fixture-token"}`,
		ProxyURL: "http://proxy.invalid", Fingerprint: gateway.CredentialFingerprint{UserAgent: "stable-gcs", TLSProfile: "firefox"},
		HeaderOverride: map[string]string{"X-Policy": "configured"}, ParamOverride: map[string]any{"not_on_get": true}, StatusCodeMapping: map[string]int{"503": 200}}
	original := &gateway.Request{Model: "alias", Body: []byte(`{"model":"alias","prompt":"cat"}`), Targets: []gateway.Target{target}}
	before, _ := json.Marshal(original)
	calls := 0
	clients := func(_ context.Context, selected gateway.Target) (*http.Client, error) {
		if selected.ChannelID != 11 || selected.CredentialID != 12 || selected.ProxyURL != target.ProxyURL || selected.Fingerprint != target.Fingerprint {
			t.Fatal("GCS content lost credential identity")
		}
		return &http.Client{Transport: policyTransportFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.String() != "https://storage.googleapis.com/fixture-bucket/video.mp4" || req.Header.Get("Authorization") != "Bearer fixture-token" || req.Header.Get("X-Goog-User-Project") != "fixture" || req.Header.Get("X-Policy") != "configured" || req.Method != http.MethodGet {
				t.Errorf("wrong canonical storage request: %s %v", req.URL, req.Header)
			}
			return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("gcs-video")), Request: req}, nil
		})}, nil
	}
	ctx := native.WithRequest(context.Background(), original, target, clients)
	response, err := New(nil).Content(ctx, target, native.Task{UpstreamID: operation, Model: "veo", Data: json.RawMessage(`{"done":true,"response":{"videos":[{"gcsUri":"gs://fixture-bucket/video.mp4"}]}}`)})
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if response.StatusCode != 200 || string(body) != "gcs-video" || readErr != nil || closeErr != nil || calls != 1 {
		t.Fatalf("GCS content response %d %q %v %v calls=%d", response.StatusCode, body, readErr, closeErr, calls)
	}
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("canonical GCS request mutated frozen target/body")
	}
}
