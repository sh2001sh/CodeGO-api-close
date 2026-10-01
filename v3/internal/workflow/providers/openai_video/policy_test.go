package openai_video

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type policyTransport func(*http.Request) (*http.Response, error)

func (f policyTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestChannelPolicyAndSelectedClientAcrossOperations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Policy") != "applied" || r.Header.Get("X-Selected") != "yes" || r.Header.Get("Authorization") != "Bearer credential" {
			t.Error("channel headers/client missing")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["model"] != "mapped" || body["seconds"] != "8" || body["prompt"] != "original" {
				t.Errorf("policy not applied after conversion: %+v", body)
			}
			_, _ = io.WriteString(w, `{"id":"id","status":"queued"}`)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil || len(data) != 0 || r.Header.Get("Content-Type") != "" {
			t.Errorf("GET policy changed body: %s %v", data, err)
		}
		if r.URL.Path == "/v1/videos/id/content" {
			_, _ = io.WriteString(w, "movie")
			return
		}
		_, _ = io.WriteString(w, `{"id":"id","status":"completed","seconds":"8"}`)
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, UpstreamModel: "mapped", Secret: "credential", ChannelID: 42,
		ParamOverride: map[string]any{"seconds": "8"}, HeaderOverride: map[string]string{"X-Policy": "applied", "Authorization": "Bearer {api_key}"}, StatusCodeMapping: map[string]int{"503": 200}}
	var selected atomic.Int64
	original := &gateway.Request{Model: "client", Body: []byte(`{"prompt":"original","model":"client"}`)}
	ctx := native.WithRequest(context.Background(), original, target, func(_ context.Context, got gateway.Target) (*http.Client, error) {
		if got.ChannelID != 42 {
			t.Error("selected target lost")
		}
		selected.Add(1)
		return &http.Client{Transport: policyTransport(func(req *http.Request) (*http.Response, error) {
			req.Header.Set("X-Selected", "yes")
			return http.DefaultTransport.RoundTrip(req)
		})}, nil
	})
	p := New(server.Client())
	if result, err := p.Submit(ctx, target, native.Submit{Body: original.Body}); err != nil || result.ID != "id" {
		t.Fatalf("submit %+v %v", result, err)
	}
	if result, err := p.Poll(ctx, target, native.Task{UpstreamID: "id"}); err != nil || result.Units != 8 {
		t.Fatalf("poll %+v %v", result, err)
	}
	resp, err := p.Content(ctx, target, native.Task{UpstreamID: "id"})
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil || string(data) != "movie" || resp.StatusCode != 200 {
		t.Fatalf("content %s %v %v status=%d", data, readErr, closeErr, resp.StatusCode)
	}
	if selected.Load() != 3 || string(original.Body) != `{"prompt":"original","model":"client"}` {
		t.Fatal("request policy context or original billing body lost")
	}
}

func TestMultipartRejectsParamOverrideBeforeDispatch(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("prompt", "original"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, UpstreamModel: "mapped", ParamOverride: map[string]any{"seconds": "8"}}
	_, err := New(server.Client()).Submit(context.Background(), target, native.Submit{Body: body.Bytes(), ContentType: writer.FormDataContentType()})
	var invalid *native.InvalidRequest
	if !errors.As(err, &invalid) || calls.Load() != 0 {
		t.Fatalf("multipart override dispatched or misclassified: %v calls=%d", err, calls.Load())
	}
}

func TestContentMappedRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Policy") != "applied" {
			t.Error("content skipped channel policy")
		}
		_, _ = io.WriteString(w, "movie")
	}))
	defer server.Close()
	target := gateway.Target{BaseURL: server.URL, HeaderOverride: map[string]string{"X-Policy": "applied"}, StatusCodeMapping: map[string]int{"200": 403}}
	_, err := New(server.Client()).Content(context.Background(), target, native.Task{UpstreamID: "id"})
	var rejected *native.Rejected
	if !errors.As(err, &rejected) || rejected.Status != 403 {
		t.Fatalf("mapped content status ignored: %v", err)
	}
}
