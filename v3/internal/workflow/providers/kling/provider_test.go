package kling

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

func assertJWT(t *testing.T, auth string) {
	t.Helper()
	parts := strings.Split(strings.TrimPrefix(auth, "Bearer "), ".")
	if len(parts) != 3 {
		t.Errorf("expected signed JWT")
		return
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Error(err)
		return
	}
	mac := hmac.New(sha256.New, []byte("fixture-secret"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		t.Error("JWT signature mismatch")
	}
	var claims struct {
		Iss      string `json:"iss"`
		Exp, Nbf int64
	}
	data, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(data, &claims); err != nil {
		t.Error(err)
		return
	}
	now := time.Now().Unix()
	if claims.Iss != "fixture-access" || claims.Exp-now < 1798 || claims.Exp-now > 1800 || claims.Nbf > now {
		t.Errorf("invalid JWT claims %+v", claims)
	}
}

func TestImageSubmitJWTMappingAndPollUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertJWT(t, r.Header.Get("Authorization"))
		if r.Header.Get("User-Agent") != "kling-sdk/1.0" {
			t.Error("SDK header missing")
		}
		if r.Method == "POST" {
			if r.URL.Path != "/v1/videos/image2video" {
				t.Errorf("wrong submit path %s", r.URL.Path)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["model_name"] != "kling-v2-master" || body["model"] != "kling-v2-master" || body["duration"] != "10" || body["aspect_ratio"] != "16:9" || body["image_tail"] != "https://image.example/tail" {
				t.Errorf("bad payload %v", body)
			}
			if _, ok := body["metadata"]; ok {
				t.Error("metadata wrapper leaked")
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"up-id","task_status":"submitted"}}`))
		} else {
			if r.URL.Path != "/v1/videos/image2video/up-id" {
				t.Errorf("wrong poll path %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"up-id","task_status":"succeed","final_unit_deduction":"12.1","task_result":{"videos":[{"url":"https://video.example/one","duration":"10"}]}}}`))
		}
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, Secret: "fixture-access|fixture-secret", UpstreamModel: "kling-v2-master"}
	p := New(server.Client())
	r, err := p.Submit(context.Background(), target, native.Submit{Model: "client-model", Action: "textGenerate", Body: []byte(`{"prompt":"hello","duration":10,"size":"1920x1080","metadata":{"model_name":"spoof","image_tail":"https://image.example/tail"}}`)})
	if err != nil || r.ID != "up-id" || r.Status != "queued" {
		t.Fatalf("submit %+v %v", r, err)
	}
	r, err = p.Poll(context.Background(), target, native.Task{UpstreamID: r.ID, Action: "textGenerate", Data: r.Data})
	if err != nil || r.Status != "completed" || r.Units != 10 || r.URL != "https://video.example/one" || r.Usage.CompletionTokens != 13 {
		t.Fatalf("poll %+v %v", r, err)
	}
}

func TestRelayTextAndFailedTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-fixture-relay" {
			t.Error("relay token changed")
		}
		if r.Method == "POST" {
			if r.URL.Path != "/kling/v1/videos/text2video" {
				t.Error(r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"relay-id"}}`))
		} else {
			if r.URL.Path != "/kling/v1/videos/text2video/relay-id" {
				t.Error(r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"relay-id","task_status":"failed","task_status_msg":"policy rejected"}}`))
		}
	}))
	defer server.Close()
	p := New(server.Client())
	target := gateway.Target{BaseURL: server.URL, Secret: "sk-fixture-relay"}
	r, err := p.Submit(context.Background(), target, native.Submit{Body: []byte(`{"prompt":"hi"}`)})
	if err != nil || r.ID != "relay-id" {
		t.Fatalf("submit %+v %v", r, err)
	}
	r, err = p.Poll(context.Background(), target, native.Task{UpstreamID: r.ID, Data: r.Data})
	if err != nil || r.Status != "failed" || r.Error != "policy rejected" || r.Units != 0 {
		t.Fatalf("poll %+v %v", r, err)
	}
}

func TestInvalidCredentialsAndBodyDoNotReachUpstream(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	for _, tc := range []struct{ secret, body string }{
		{"invalid", `{"prompt":"hi"}`}, {"a|", `{}`}, {"a|b", `{"duration":"oops"}`}, {"a|b", `{"metadata":3}`},
	} {
		_, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: tc.secret}, native.Submit{Body: []byte(tc.body)})
		var invalid *native.InvalidRequest
		if !errors.As(err, &invalid) {
			t.Fatalf("expected validation error, got %v", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input reached upstream")
	}
}

func TestUnknownPollAndMalformedSubmitRemainIndeterminate(t *testing.T) {
	for _, tc := range []struct {
		body string
		poll bool
	}{
		{`{"code":0,"data":{"task_id":"id","task_status":"brand-new"}}`, true},
		{`{"code":0,"data":{}}`, false},
		{`{"data":{"task_id":"id","task_status":"succeed"}}`, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
		p := New(server.Client())
		target := gateway.Target{BaseURL: server.URL, Secret: "sk-fixture"}
		var err error
		if tc.poll {
			_, err = p.Poll(context.Background(), target, native.Task{UpstreamID: "id"})
		} else {
			_, err = p.Submit(context.Background(), target, native.Submit{Body: []byte(`{}`)})
		}
		server.Close()
		var invalid *native.InvalidRequest
		if err == nil || errors.As(err, &invalid) {
			t.Fatalf("expected indeterminate upstream response, got %v", err)
		}
	}
}

func TestMultipartImageSubmit(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("prompt", "a video"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("seconds", "5"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("cfg_scale", "0.75"); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("input_reference", "image.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("fixture-image"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/kling/v1/videos/image2video" || fields["image"] != "Zml4dHVyZS1pbWFnZQ==" || fields["duration"] != "5" || fields["cfg_scale"] != 0.75 {
			t.Errorf("multipart payload %v", fields)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"id"}}`))
	}))
	defer server.Close()
	r, err := New(server.Client()).Submit(context.Background(), gateway.Target{BaseURL: server.URL, Secret: "sk-fixture"}, native.Submit{Body: body.Bytes(), ContentType: writer.FormDataContentType()})
	if err != nil || r.ID != "id" {
		t.Fatalf("submit %+v %v", r, err)
	}
}
