//go:build pgintegration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// This fixture uses a dedicated sampler test database. It creates the audit
// relation with the production columns and a real identity foreign key, then
// validates audit.Service's actual SQL writes and recursive JSON redaction.
func TestAsyncAuditSamplerPGRedactsActualRequestAndEvents(t *testing.T) {
	dsn := os.Getenv("V3_AUDIT_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_AUDIT_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS v3_identity;
CREATE TABLE IF NOT EXISTS v3_identity.users (id bigint PRIMARY KEY,username text NOT NULL);
CREATE SCHEMA IF NOT EXISTS v3_audit;
CREATE TABLE IF NOT EXISTS v3_audit.request_samples (
request_id text PRIMARY KEY,user_id bigint NOT NULL REFERENCES v3_identity.users(id),
model text NOT NULL,created_at timestamptz NOT NULL,request_body jsonb NOT NULL,response_body jsonb NOT NULL);
INSERT INTO v3_identity.users(id,username) VALUES(7,'audit-fixture') ON CONFLICT(id) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("V3_AUDIT_SAMPLE_RATE_PPM", "1000000")
	t.Setenv("V3_AUDIT_SAMPLE_QUEUE", "4")
	t.Setenv("V3_AUDIT_SAMPLE_MAX_BYTES", "2048")
	s, closeFn, err := newAuditSampler(ctx, pool, sampleTestLog(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFn)
	id := fmt.Sprintf("audit-fixture-%d", time.Now().UnixNano())
	req := auditTestRequest(id)
	req.Body = []byte(`{"api_key":"request-secret","nested":[{"Authorization":"Bearer header-secret"}],"amount":9007199254740993,"messages":[{"content":"actual request"}]}`)
	response := gateway.NewResponseSample(s.MaxBytes())
	response.Add(gateway.Event{Kind: gateway.EventData, Payload: []byte(`{"delta":{"text":"actual response","access_token":"response-secret"}}`)})
	response.Add(gateway.Event{Kind: gateway.EventError, Payload: []byte(`{"error":{"code":"actual_error","password":"error-secret"}}`)})
	s.Record(req, gateway.Outcome{Terminal: gateway.TerminalUpstreamErrorAfterOutput, Usage: gateway.Usage{PromptTokens: 7, CompletionTokens: 3}, Charge: true}, response.JSON())
	large := auditTestRequest(id + "-large")
	large.Body = []byte(`{"content":"` + strings.Repeat("x", 3000) + `"}`)
	s.Record(large, gateway.Outcome{Terminal: gateway.TerminalEmptyStream}, nil)
	closeFn()
	var requestBody, responseBody []byte
	if err := pool.QueryRow(ctx, `SELECT request_body,response_body FROM v3_audit.request_samples WHERE request_id=$1`, id).Scan(&requestBody, &responseBody); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"request-secret", "header-secret", "response-secret", "error-secret"} {
		if bytes.Contains(requestBody, []byte(secret)) || bytes.Contains(responseBody, []byte(secret)) {
			t.Fatalf("persisted a credential: %s", secret)
		}
	}
	if !bytes.Contains(requestBody, []byte("9007199254740993")) || !bytes.Contains(requestBody, []byte("actual request")) || !bytes.Contains(responseBody, []byte("actual response")) || !bytes.Contains(responseBody, []byte("actual_error")) || !bytes.Contains(responseBody, []byte("[redacted]")) {
		t.Fatalf("actual payload or numeric precision lost: %s %s", requestBody, responseBody)
	}
	var decoded struct {
		Terminal string
		Usage    gateway.Usage
		Response sampledResponse
	}
	if json.Unmarshal(responseBody, &decoded) != nil || decoded.Terminal != "upstream_error_after_output" || decoded.Usage.PromptTokens != 7 || decoded.Usage.CompletionTokens != 3 || len(decoded.Response.Events) != 2 {
		t.Fatalf("audit outcome lost: %s", responseBody)
	}
	if err := pool.QueryRow(ctx, `SELECT request_body FROM v3_audit.request_samples WHERE request_id=$1`, large.ID).Scan(&requestBody); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(requestBody, []byte(`"truncated": true`)) {
		t.Fatalf("large request was not explicitly bounded: %s", requestBody)
	}
	t.Log("real PostgreSQL persisted request, data/error events and settled usage; nested credentials redacted; accepted records flushed on shutdown")
}
