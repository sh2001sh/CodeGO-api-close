package gateway

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestBodyReadersAndOverridesAreIndependent(t *testing.T) {
	body := []byte(`{"model":"model","input":"original","service_tier":"fast"}`)
	out := &http.Request{Header: make(http.Header)}
	out.Header.Set("Content-Type", "application/json")
	SetRequestBody(out, body)
	borrowed, err := ReadRequestBody(out)
	if err != nil || &borrowed[0] != &body[0] {
		t.Fatal("immutable body was copied", err)
	}
	replay, err := out.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := replay.Close(); err != nil {
			t.Error(err)
		}
	})
	req := &Request{Protocol: ProtocolResponses, Body: body}
	if err = ApplyUpstreamRequest(out, req, Target{ParamOverride: map[string]any{"input": "changed"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(out.Body)
	old, _ := io.ReadAll(replay)
	newReplay, _ := out.GetBody()
	t.Cleanup(func() {
		if err := newReplay.Close(); err != nil {
			t.Error(err)
		}
	})
	newBytes, _ := io.ReadAll(newReplay)
	if !bytes.Equal(old, body) || !bytes.Equal(newBytes, got) || !strings.Contains(string(got), `"input":"changed"`) || out.ContentLength != int64(len(got)) {
		t.Fatal("override mutated original/replay or lost length")
	}
	if !strings.Contains(string(body), `"input":"original"`) {
		t.Fatal("billing input was modified")
	}
}

func TestRequestBodyFallbackStillChecksReadErrorsAndJSON(t *testing.T) {
	out := &http.Request{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"model":"model"}`)), GetBody: func() (io.ReadCloser, error) { return nil, errors.New("read failed") }}
	if _, err := ReadRequestBody(out); err == nil {
		t.Fatal("reader error was ignored")
	}
	for _, body := range []string{`[]`, `null`, `{"model":"model",}`, `{"model":"model"} {}`} {
		SetRequestBody(out, []byte(body))
		out.Header.Set("Content-Type", "application/json")
		if err := ApplyUpstreamRequest(out, &Request{Body: []byte(body)}, Target{}); err == nil {
			t.Fatalf("invalid JSON accepted: %s", body)
		}
	}
}

func TestParseLongRequestKeepsModelAndRejectsInvalidBody(t *testing.T) {
	body := ` {"model":"escaped-\u006dodel","stream":true,"service_tier":"priority","input":"` + strings.Repeat("a", 1<<20) + `"}`
	r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req := &Request{}
	if err := parseRequest(httptest.NewRecorder(), r, 2<<20, req); err != nil || req.Model != "escaped-model" || !req.Stream || FastServiceTier(req) != "priority" || string(req.Body) != body {
		t.Fatal("long body parse mismatch", err)
	}
	for _, body := range []string{`[]`, `null`, `{"model":1}`, `{"model":""}`, `{"model":"x","input":`} {
		r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
		if err := parseRequest(httptest.NewRecorder(), r, 2<<20, &Request{}); err == nil {
			t.Fatalf("invalid body accepted: %s", body)
		}
	}
}
