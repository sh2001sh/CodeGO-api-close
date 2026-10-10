package legacy

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
)

func historicalPasskeyFixture(t *testing.T) (json.RawMessage, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cose, err := cbor.Marshal(map[int]any{1: 1, 3: -8, -1: 6, -2: []byte(public)})
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"id": 31, "user_id": 7, "credential_id": base64.StdEncoding.EncodeToString([]byte("kept-credential")),
		"public_key": base64.StdEncoding.EncodeToString(cose), "attestation_type": "none", "aaguid": base64.StdEncoding.EncodeToString(make([]byte, 16)),
		"sign_count": 17, "clone_warning": false, "user_present": true, "user_verified": true, "backup_eligible": true, "backup_state": true,
		"transports": `["internal","usb"]`, "attachment": "platform", "created_at": "2024-03-04T05:06:07Z", "updated_at": "2024-04-05T06:07:08Z",
		"last_used_at": "2024-04-05T06:07:08Z", "deleted_at": nil}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	return b, private
}

func TestHistoryPasskeyUsesRealWebAuthnCredentialAndSignature(t *testing.T) {
	raw, private := historicalPasskeyFixture(t)
	p, err := decodeHistoryPasskey(raw)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p.Credential)
	if err != nil {
		t.Fatal(err)
	}
	// This is the exact JSON decode used by v3 loadPasskeyUser.
	var credential webauthn.Credential
	if err = json.Unmarshal(b, &credential); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(credential.ID, []byte("kept-credential")) || credential.Authenticator.SignCount != 17 ||
		len(credential.Transport) != 2 || !credential.Flags.UserVerified || !credential.Flags.BackupState || p.ID != 31 ||
		!p.CreatedAt.Equal(time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)) {
		t.Fatalf("lost WebAuthn fields: %+v", p)
	}
	key, err := webauthncose.ParsePublicKey(credential.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("authenticator-data and client-hash")
	signature := ed25519.Sign(private, message)
	valid, err := webauthncose.VerifySignature(key, message, signature)
	if err != nil || !valid {
		t.Fatalf("migrated credential cannot verify: %v", err)
	}
	message[0] ^= 1
	valid, err = webauthncose.VerifySignature(key, message, signature)
	if err == nil && valid {
		t.Fatal("tampered assertion signature accepted")
	}
}

func TestHistoryPasskeyRejectsInvalidAndPreservesDeletedState(t *testing.T) {
	raw, _ := historicalPasskeyFixture(t)
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"credential_id", "public_key", "transports", "aaguid"} {
		t.Run(field, func(t *testing.T) {
			copy := map[string]any{}
			for k, v := range row {
				copy[k] = v
			}
			copy[field] = "invalid%%"
			bad, _ := json.Marshal(copy)
			if _, err := decodeHistoryPasskey(bad); err == nil {
				t.Fatal("invalid field accepted")
			}
		})
	}
	row["deleted_at"] = "2025-01-01T00:00:00Z"
	row["public_key"] = "invalid%%"
	deleted, _ := json.Marshal(row)
	p, err := decodeHistoryPasskey(deleted)
	if err != nil || !p.Deleted {
		t.Fatalf("deleted credential should not reactivate: %v", err)
	}
}

func TestHistoryLedgerExactSignedAmountsAndNullableBalances(t *testing.T) {
	for _, tc := range []struct {
		direction       string
		input, expected int64
	}{
		{"credit", 23, 46}, {"debit", 23, -46}, {"credit", 0, 0}, {"debit", math.MaxInt64 / 2, -(math.MaxInt64 - 1)},
	} {
		raw, _ := json.Marshal(map[string]any{"entry_id": "old-entry", "account_id": "old-account", "entry_type": "settle_debit", "direction": tc.direction, "amount": tc.input, "balance_after": nil, "idempotency_key": "same-key", "metadata": map[string]any{"reference": "kept"}})
		e, err := decodeHistoryEntry(raw)
		if err != nil || e.Amount != tc.expected || e.BalanceAfter != nil {
			t.Fatalf("entry=%+v err=%v", e, err)
		}
	}
	for _, amount := range []int64{-1, math.MaxInt64/2 + 1, math.MaxInt64} {
		raw, _ := json.Marshal(map[string]any{"entry_id": "e", "account_id": "a", "entry_type": "grant_credit", "direction": "credit", "amount": amount, "idempotency_key": "k"})
		if _, err := decodeHistoryEntry(raw); err == nil {
			t.Fatalf("unsafe amount %d accepted", amount)
		}
	}
}

func TestHistoryLogTypesAndBoundaryValidation(t *testing.T) {
	for typ := 0; typ <= 6; typ++ {
		l, err := decodeHistoryLog(json.RawMessage(`{"id":55,"user_id":7,"created_at":1700000000,"type":` + string(rune('0'+typ)) + `,"quota":19,"other":"{\"cache_read_tokens\":3}"}`))
		if err != nil || l.Amount != 38 || l.CachedTokens != 3 || l.CreatedAt.Unix() != 1700000000 {
			t.Fatalf("log=%+v err=%v", l, err)
		}
	}
	for _, raw := range []string{
		`{"id":1,"type":2,"quota":-1}`, `{"id":1,"type":7}`, `{"id":1,"type":2,"prompt_tokens":-1}`,
		`{"id":1,"type":2,"other":"bad"}`, `{"id":1,"type":2,"quota":9223372036854775807}`,
	} {
		if _, err := decodeHistoryLog(json.RawMessage(raw)); err == nil {
			t.Fatalf("unsafe log accepted: %s", raw)
		}
	}
}

func TestHistoryLogCacheSubtractionAnomalyPreservesRawEvidence(t *testing.T) {
	row := map[string]any{"id": 55, "user_id": 7, "type": 2, "quota": 19, "prompt_tokens": -12,
		"completion_tokens": 978, "use_time": 38, "other": `{"usage_semantic":"anthropic","cache_creation_tokens":9827,"cache_tokens":0}`}
	raw, _ := json.Marshal(row)
	l, err := decodeHistoryLog(raw)
	if err != nil || l.PromptTokens != -12 || l.Amount != 38 || !l.LegacyPromptAnomaly || string(l.Metadata) != row["other"] {
		t.Fatalf("raw evidence changed: %+v %v", l, err)
	}
	for _, mutation := range []map[string]any{
		{"other": `{}`}, {"other": `{"usage_semantic":"openai","cache_creation_tokens":9827}`},
		{"other": `{"usage_semantic":"anthropic","cache_creation_tokens":11}`},
		{"other": `{"usage_semantic":"anthropic","cache_creation_tokens":9223372036854775808}`},
		{"type": 1}, {"completion_tokens": -1}, {"use_time": -1}, {"quota": -1},
		{"prompt_tokens": int64(math.MinInt64)},
	} {
		copy := map[string]any{}
		for k, v := range row {
			copy[k] = v
		}
		for k, v := range mutation {
			copy[k] = v
		}
		bad, _ := json.Marshal(copy)
		if _, err := decodeHistoryLog(bad); err == nil {
			t.Fatalf("unwitnessed anomaly accepted: %v", mutation)
		}
	}
	row["prompt_tokens"], row["legacy_prompt_anomaly"] = 0, true
	raw, _ = json.Marshal(row)
	if l, err := decodeHistoryLog(raw); err != nil || l.LegacyPromptAnomaly {
		t.Fatalf("source forged anomaly marker: %+v %v", l, err)
	}
}

func TestHistoryTimestampsBothLegacyFormats(t *testing.T) {
	for _, raw := range []string{`1700000000`, `"2023-11-14T22:13:20Z"`} {
		var tm historyTime
		if err := json.Unmarshal([]byte(raw), &tm); err != nil || tm.Unix() != 1700000000 {
			t.Fatalf("time=%v err=%v", tm, err)
		}
	}
	var tm historyTime
	if err := json.Unmarshal([]byte(`"unknown"`), &tm); err == nil {
		t.Fatal("invalid timestamp accepted")
	}
}

func TestHistoryTimestampPostgresSecondOffsetsPreserveInstants(t *testing.T) {
	for _, tc := range []struct {
		input, utc string
	}{
		{"0001-01-01T08:05:43+08:05:43", "0001-01-01T00:00:00Z"},
		{"0001-01-01T00:53:28+00:53:28", "0001-01-01T00:00:00Z"},
		{"1900-01-01T08:05:43.123456+08:05:43", "1900-01-01T00:00:00.123456Z"},
		{"1883-11-18T12:03:58-04:56:02", "1883-11-18T17:00:00Z"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var tm historyTime
			if err := json.Unmarshal([]byte(`"`+tc.input+`"`), &tm); err != nil {
				t.Fatal(err)
			}
			if tm.Format(time.RFC3339Nano) != tc.utc || tm.Location() != time.UTC {
				t.Fatalf("timestamp changed: %v; want %s in UTC", tm.Time, tc.utc)
			}
			// time.Time's JSON encoding drops offset seconds. UTC normalization
			// must preserve the instant through the import/check JSON round trip.
			encoded, err := json.Marshal(tm.Time)
			if err != nil || string(encoded) != `"`+tc.utc+`"` {
				t.Fatalf("timestamp JSON changed: %s, %v", encoded, err)
			}
			if tc.utc == "0001-01-01T00:00:00Z" && !tm.IsZero() {
				t.Fatal("original unset Go timestamp lost its zero-time meaning")
			}
		})
	}
	for _, raw := range []string{
		`"1900-02-30T08:05:43+08:05:43"`,
		`"1900-01-01T08:05:43+08:05:99"`,
		`"1900-01-01T08:05:43+08:05:60"`,
		`"1900-01-01T08:05:43+08:60:43"`,
		`"1900-01-01T08:05:43+24:05:43"`,
		`"1900-01-01T08:05:43+08:05:43junk"`,
		`"infinity"`, `"-infinity"`, `"unknown"`,
	} {
		var tm historyTime
		if err := json.Unmarshal([]byte(raw), &tm); err == nil {
			t.Fatalf("invalid timestamp accepted: %s", raw)
		}
	}
}

func TestHistoryRetiredAccountKindsAndOriginalUnitReporting(t *testing.T) {
	for _, kind := range []string{"gpt_wallet", "points", "point_wallet", "bonus_quota", "blind_box_credits", "wallet"} {
		raw, _ := json.Marshal(map[string]any{"account_id": "old", "owner_type": "user", "account_type": kind,
			"version": "invalid-retired-number", "created_at": "invalid-retired-date"})
		id, retired := retiredHistoryAccount(raw)
		if id != "old" || !retired {
			t.Fatalf("retired kind %s requires no monetary parsing: id=%s retired=%v", kind, id, retired)
		}
	}
	if _, retired := retiredHistoryAccount(json.RawMessage(`{"account_id":"active","owner_type":"user","account_type":"claude_wallet"}`)); retired {
		t.Fatal("current Claude wallet excluded")
	}
	d := &historyData{retiredAccounts: map[string]bool{"old": true}, counts: map[string]int64{}, amounts: map[string]*big.Int{}}
	for i := 0; i < 2; i++ {
		raw := json.RawMessage(`{"account_id":"old","amount":9223372036854775807,"balance_after":9223372036854775807}`)
		if !d.retiredHistoryEntry(raw) {
			t.Fatal("retired entry would reach monetary conversion")
		}
		d.reportRetiredHistoryEntry(raw)
	}
	d.reportRetiredHistoryEntry(json.RawMessage(`{"account_id":"old","amount":"invalid","balance_after":null}`))
	r := Report{}
	d.validate(&r)
	if len(r.Issues) != 0 || r.Counts["retired_features.billing_history.ledger_entries"] != 3 ||
		r.Counts["retired_features.billing_history.ledger_entries.amount_unparseable"] != 1 ||
		r.Amounts["retired_features.billing_history.ledger_entries.amount_v2_units"] != "18446744073709551614" ||
		r.Amounts["retired_features.billing_history.ledger_entries.balance_after_v2_units"] != "18446744073709551614" {
		t.Fatalf("original-unit retirement report=%+v", r)
	}
	if d.retiredHistoryEntry(json.RawMessage(`{"account_id":"active","amount":9223372036854775807}`)) {
		t.Fatal("current money would evade overflow validation")
	}
}
