package jimeng

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func TestOverridesAreAppliedBeforeSigningAndKeepFrozenInputs(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Error(err)
			return
		}
		if fields["req_key"] != "configured-task-model" || r.Header.Get("X-Channel") != "configured" {
			t.Errorf("overrides missing: %s, headers %v", body, r.Header)
		}
		if !reflect.DeepEqual(fields["trace"], []any{"once"}) || fields["actor"] != "verified" {
			t.Errorf("policy lost caller metadata or applied more than once: %v", fields)
		}
		if r.Host != "signed.example.test" {
			t.Errorf("host override missing: %s", r.Host)
		}
		if r.URL.Query().Get("Action") == "CVSync2AsyncSubmitTask" && (fields["prompt"] != "configured prompt" || fields["frames"] != float64(241)) {
			t.Errorf("body override missing: %s", body)
		}
		payloadHash := sha256.Sum256(body)
		if r.Header.Get("X-Content-Sha256") != hex.EncodeToString(payloadHash[:]) {
			t.Error("signature payload hash does not cover final HTTP body")
		}
		signedAt, err := time.Parse("20060102T150405Z", r.Header.Get("X-Date"))
		if err != nil {
			t.Error(err)
			return
		}
		check, err := http.NewRequest("POST", "http://"+r.Host+r.URL.RequestURI(), bytes.NewReader(body))
		if err != nil {
			t.Error(err)
			return
		}
		check.Header.Set("Content-Type", r.Header.Get("Content-Type"))
		if err := signRequest(check, body, "fixture-access", "fixture-secret", signedAt); err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("Authorization") != check.Header.Get("Authorization") {
			t.Error("signature does not match final body and overridden host")
		}
		w.WriteHeader(http.StatusTooManyRequests)
		if r.URL.Query().Get("Action") == "CVSync2AsyncSubmitTask" {
			_, _ = w.Write([]byte(`{"code":10000,"data":{"task_id":"native-task"}}`))
		} else {
			_, _ = w.Write([]byte(`{"code":10000,"data":{"status":"done","duration":4,"video_url":"https://media.example/video"}}`))
		}
	}))
	defer server.Close()
	target := gateway.Target{Provider: "jimeng", BaseURL: server.URL, Secret: "fixture-access|fixture-secret", UpstreamModel: "jimeng_v30", ChannelID: 17, CredentialID: 19,
		ParamOverride: map[string]any{"prompt": "configured prompt", "req_key": "configured-task-model", "frames": 241,
			"operations": []any{map[string]any{"mode": "set", "path": "trace", "value": []any{}, "keep_origin": true}, map[string]any{"mode": "append", "path": "trace", "value": []any{"once"}}, map[string]any{"mode": "set", "path": "actor", "value": "verified", "conditions": map[string]any{"user_id": 7}}}},
		HeaderOverride: map[string]string{"X-Channel": "configured", "Host": "signed.example.test"}, StatusCodeMapping: map[string]int{"429": 200}}
	originalBody := []byte(`{"model":"client-model","prompt":"original prompt"}`)
	frozen := &gateway.Request{ID: "request-id", Model: "client-model", Body: bytes.Clone(originalBody), Principal: gateway.Principal{UserID: 7, KeyID: 9}, PricingHeaders: map[string]string{"X-Pricing": "frozen"}, Reserve: &struct{}{}}
	reservation := frozen.Reserve
	selected := 0
	clients := func(_ context.Context, selectedTarget gateway.Target) (*http.Client, error) {
		selected++
		if selectedTarget.CredentialID != 19 || selectedTarget.ChannelID != 17 {
			t.Errorf("wrong selected credential %+v", selectedTarget)
		}
		return server.Client(), nil
	}
	ctx := native.WithRequest(context.Background(), frozen, target, clients)
	p := New(nil)
	r, err := p.Submit(ctx, target, native.Submit{Model: "client-model", Body: originalBody})
	if err != nil || r.ID != "native-task" {
		t.Fatalf("submit %+v %v", r, err)
	}
	var saved struct {
		Key    string `json:"_v3_req_key"`
		Frames int    `json:"_v3_frames"`
	}
	if err := json.Unmarshal(r.Data, &saved); err != nil || saved.Key != "configured-task-model" || saved.Frames != 241 {
		t.Errorf("stored polling model is not the submitted final model: %s", r.Data)
	}
	r, err = p.Poll(ctx, target, native.Task{UpstreamID: r.ID, Data: r.Data})
	if err != nil || r.Status != "completed" || r.Units != 4 {
		t.Fatalf("poll %+v %v", r, err)
	}
	if selected != 2 || calls != 2 {
		t.Errorf("credential client not used exactly once per request: selected %d, calls %d", selected, calls)
	}
	if !bytes.Equal(frozen.Body, originalBody) || frozen.Reserve != reservation || !reflect.DeepEqual(frozen.PricingHeaders, map[string]string{"X-Pricing": "frozen"}) {
		t.Error("frozen billing inputs mutated")
	}
}
