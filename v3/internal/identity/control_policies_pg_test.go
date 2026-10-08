//go:build pgintegration

package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/migrations"
)

// This suite uses a dedicated empty database and never clears schemas or Redis.
func policyTestControl(t *testing.T) *Control {
	t.Helper()
	dsn := os.Getenv("V3_POLICY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_POLICY_TEST_PG_DSN not set (requires dedicated codego_policy_* database)")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var database string
	var initialized bool
	if err := pool.QueryRow(context.Background(), `SELECT current_database(),to_regclass('v3_identity.policy_acceptances') IS NOT NULL`).Scan(&database, &initialized); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(database, "codego_policy_") {
		t.Fatal("policy tests require isolated codego_policy_* database")
	}
	if !initialized {
		names, err := migrations.Files()
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			sql, err := migrations.Read(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Fatalf("migration %s: %v", name, err)
			}
		}
	}
	c, err := NewControl(pool, ControlConfig{
		SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), PublicURL: "https://codego.test",
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPolicyAcceptancePGRegistrationAtomicReplayAndSelfScope(t *testing.T) {
	c := policyTestControl(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	registrationBody, err := json.Marshal(RegisterInput{Username: "pa" + suffix, Password: "strong-password",
		AcceptedTermsVersion: CurrentPolicyVersion, AcceptedPrivacyVersion: CurrentPolicyVersion, AgreementLocale: "zh-HK"})
	if err != nil {
		t.Fatal(err)
	}
	registration := httptest.NewRecorder()
	c.Handler().ServeHTTP(registration, httptest.NewRequest(http.MethodPost, "https://codego.test/api/user/register", bytes.NewReader(registrationBody)))
	var registered struct {
		Data Session `json:"data"`
	}
	if err := json.Unmarshal(registration.Body.Bytes(), &registered); err != nil || registration.Code != http.StatusOK || registered.Data.User.ID == 0 {
		t.Fatalf("registration status=%d body=%s err=%v", registration.Code, registration.Body.String(), err)
	}
	alice := registered.Data.User
	records, err := c.PolicyAcceptances(ctx, alice.ID)
	if err != nil || len(records) != 2 {
		t.Fatalf("registration records=%+v err=%v", records, err)
	}
	for _, record := range records {
		if record.Version != CurrentPolicyVersion || record.Locale != "zh-HK" || record.AcceptedAt.IsZero() {
			t.Fatalf("registration record=%+v", record)
		}
	}
	bob, err := c.Register(ctx, RegisterInput{Username: "pb" + suffix, Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	bobRecords, err := c.PolicyAcceptances(ctx, bob.ID)
	if err != nil || len(bobRecords) != 0 {
		t.Fatalf("historical/internal account falsely accepted=%+v err=%v", bobRecords, err)
	}
	if accepted, err := HasAcceptedCurrentSupplier(ctx, c.pool, alice.ID); err != nil || accepted {
		t.Fatalf("new owner prematurely eligible=%t err=%v", accepted, err)
	}
	first, err := c.AcceptPolicy(ctx, alice.ID, PolicyAcceptanceInput{Document: "supplier", Version: CurrentPolicyVersion, Locale: "zh-HK"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan PolicyAcceptance, 5)
	errors := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record, err := c.AcceptPolicy(ctx, alice.ID, PolicyAcceptanceInput{Document: "supplier", Version: CurrentPolicyVersion, Locale: "en"})
			results <- record
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for replay := range results {
		if replay.Locale != first.Locale || !replay.AcceptedAt.Equal(first.AcceptedAt) {
			t.Fatalf("replay overwrote original acceptance=%+v first=%+v", replay, first)
		}
	}
	if accepted, err := HasAcceptedCurrentSupplier(ctx, c.pool, alice.ID); err != nil || !accepted {
		t.Fatalf("persisted supplier acceptance=%t err=%v", accepted, err)
	}
	session, err := c.NewSession(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://codego.test"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+session.AccessToken)
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, r)
		return w
	}
	w := call(http.MethodGet, "/api/user/policy-acceptance?user_id="+fmt.Sprint(alice.ID), "")
	var envelope struct {
		Data []PolicyAcceptance `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || w.Code != http.StatusOK || len(envelope.Data) != 0 {
		t.Fatalf("other user's records disclosed: %d %s err=%v", w.Code, w.Body.String(), err)
	}
	for _, body := range []string{
		fmt.Sprintf(`{"document":"supplier","version":"%s","locale":"en","user_id":%d}`, CurrentPolicyVersion, alice.ID),
		`{"document":"supplier","version":"2026-10-04","locale":"en"}`,
	} {
		w = call(http.MethodPost, "/api/user/policy-acceptance", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("forged acceptance status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if accepted, err := HasAcceptedCurrentSupplier(ctx, c.pool, bob.ID); err != nil || accepted {
		t.Fatalf("invalid submission accepted=%t err=%v", accepted, err)
	}
	// A failed duplicate registration must not add or replace any acceptance.
	if _, err := c.Register(ctx, RegisterInput{Username: alice.Username, Password: "strong-password",
		AcceptedTermsVersion: CurrentPolicyVersion, AcceptedPrivacyVersion: CurrentPolicyVersion, AgreementLocale: "en"}); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	records, err = c.PolicyAcceptances(ctx, alice.ID)
	if err != nil || len(records) != 3 {
		t.Fatalf("failed registration changed records=%+v err=%v", records, err)
	}
	for _, record := range records {
		if record.Locale != "zh-HK" {
			t.Fatalf("failed registration replaced original locale=%+v", record)
		}
	}
}
