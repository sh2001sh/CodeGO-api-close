//go:build pgintegration

package legacy

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const historySourceFixtureSQL = `
CREATE TABLE migration_source.passkey_credentials (
 id bigint PRIMARY KEY,user_id bigint,credential_id text,public_key text,attestation_type text,aaguid text,
 sign_count bigint,clone_warning bool,user_present bool,user_verified bool,backup_eligible bool,backup_state bool,
 transports text,attachment text,last_used_at timestamptz,created_at timestamptz,updated_at timestamptz,deleted_at timestamptz);
CREATE TABLE migration_source.custom_oauth_providers (
 id bigint PRIMARY KEY,name text,slug text,icon text,enabled bool,client_id text,client_secret text,
 authorization_endpoint text,token_endpoint text,user_info_endpoint text,scopes text,user_id_field text,username_field text,
 display_name_field text,email_field text,well_known text,auth_style int,access_policy text,access_denied_message text,
 created_at timestamptz,updated_at timestamptz);
INSERT INTO migration_source.custom_oauth_providers VALUES
 (41,'External login','external_login','',true,'kept-client','kept-secret','https://login.test/authorize',
 'https://login.test/token','https://login.test/userinfo','openid profile email','sub','preferred_username','name','email',
 '',0,'','Denied','2024-01-01T00:00:00Z','2024-01-02T00:00:00Z');
CREATE TABLE migration_source.user_oauth_bindings(id bigint PRIMARY KEY,user_id bigint,provider_id bigint,provider_user_id text,created_at timestamptz);
INSERT INTO migration_source.user_oauth_bindings VALUES(42,7,41,'kept-external-subject','2024-01-03T00:00:00Z');
CREATE TABLE billing.ledger_entries (
 entry_id text PRIMARY KEY,account_id text,reference_type text,reference_id text,entry_type text,direction text,
 amount bigint,balance_after bigint,idempotency_key text,reason_code text,reason_detail text,operator_type text,operator_id text,
 metadata jsonb,created_at timestamptz);
INSERT INTO billing.ledger_entries VALUES
 ('debit-original','wallet-7','request','kept-request','settle_debit','debit',50,500,'debit-key','usage','kept debit','system','',
 '{"plan":"kept"}','2024-01-01T00:00:00Z'),
 ('credit-original','wallet-7','refund','refund-original','settle_credit','credit',20,NULL,'credit-key','refund','kept refund','admin','9',
 '{}','2024-01-02T00:00:00Z');
CREATE TABLE migration_source.logs (
 id bigint PRIMARY KEY,user_id bigint,created_at bigint,type int,content text,username text,token_name text,model_name text,
 quota bigint,prompt_tokens bigint,completion_tokens bigint,use_time bigint,is_stream bool,channel_id bigint,token_id bigint,
 "group" text,ip text,request_id text,upstream_request_id text,other text);
INSERT INTO migration_source.logs VALUES
 (51,7,1700000000,2,'kept usage','alice','kept-key','chat-model',50,11,7,3,true,13,11,'default','127.0.0.1','kept-request','upstream-original','{"cache_read_tokens":4}'),
 (52,7,1700000001,6,'kept refund','alice','','',20,0,0,0,false,0,0,'default','','refund-original','','{}'),
 (53,0,1700000002,4,'kept system','','','',0,0,0,0,false,0,0,'','','','',''),
 (54,7,1700000000,2,'same-request distinct adjustment','alice','kept-key','chat-model',10,2,1,0,false,13,11,'default','','kept-request','','{}');
CREATE SCHEMA gateway;
CREATE TABLE gateway.request_audits (
 request_id text PRIMARY KEY,trace_id text,user_id bigint,token_id bigint,model_name text,group_name text,protocol text,
 request_type text,status text,counted_in_success_rate bool,billable bool,quota bigint,prompt_tokens bigint,completion_tokens bigint,
 final_channel_id bigint,attempts_count bigint,retry_count bigint,status_code bigint,error_code text,
 started_at timestamptz,completed_at timestamptz,created_at timestamptz,updated_at timestamptz);
INSERT INTO gateway.request_audits VALUES
 ('kept-request','kept-trace',7,11,'chat-model','default','chat','text','succeeded',true,true,50,11,7,13,2,1,200,'',
 '2023-11-14T22:13:20Z','2023-11-14T22:13:23Z','2023-11-14T22:13:20Z','2023-11-14T22:13:23Z');
CREATE TABLE gateway.request_attempt_audits (
 attempt_id text PRIMARY KEY,request_id text,attempt_no bigint,retry_index bigint,channel_id bigint,model_name text,fault_domain text,
 request_type text,status text,success bool,status_code bigint,failure_class text,stage text,
 started_at timestamptz,completed_at timestamptz,duration_ms bigint,created_at timestamptz);
INSERT INTO gateway.request_attempt_audits VALUES
 ('kept-attempt','kept-request',1,0,13,'chat-model','same-upstream','text','succeeded',true,200,'','completed',
 '2023-11-14T22:13:20Z','2023-11-14T22:13:23Z',3000,'2023-11-14T22:13:20Z');`

func historyFixture(t *testing.T, source *pgxpool.Pool) ed25519.PrivateKey {
	t.Helper()
	ctx := context.Background()
	if _, err := source.Exec(ctx, historySourceFixtureSQL); err != nil {
		t.Fatal(err)
	}
	raw, private := historicalPasskeyFixture(t)
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.passkey_credentials SELECT * FROM jsonb_populate_record(NULL::migration_source.passkey_credentials,$1::jsonb)`, raw); err != nil {
		t.Fatal(err)
	}
	return private
}

func TestHistoryImportIndependentSourcePreservesNativeRowsAndBalances(t *testing.T) {
	source, target, crypto := importTestDB(t)
	private := historyFixture(t, source)
	seedRetiredHistoryFixture(t, source)
	ctx := context.Background()
	reader := readonlySource(t, source)
	role := pgx.Identifier{reader.Config().ConnConfig.User}.Sanitize()
	if _, err := source.Exec(ctx, "GRANT USAGE ON SCHEMA gateway TO "+role); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA gateway TO "+role); err != nil {
		t.Fatal(err)
	}
	tx, err := reader.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadHistory(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	r := Report{}
	d.validate(&r)
	assertRetiredHistoryReport(t, r)
	if len(r.Issues) > 0 || r.Counts["history.logs"] != 4 || r.Counts["history.usage_request_ids_disambiguated"] != 2 || r.Amounts["history.ledger_entries.micro_credits"] != "-60" {
		t.Fatalf("history report=%+v", r)
	}
	var count int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.passkeys`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("load wrote target: %d %v", count, err)
	}
	users, err := loadUsers(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	importer := NewImporter(reader, target, crypto)
	for i := 0; i < 2; i++ {
		if err = pgx.BeginFunc(ctx, target, func(out pgx.Tx) error {
			if err := importer.importUsers(ctx, out, users); err != nil {
				return err
			}
			return importer.importHistory(ctx, out, d)
		}); err != nil {
			t.Fatalf("history apply %d: %v", i, err)
		}
	}
	var balance, version, liveEntries, historicEntries, events, usageAmount, usageCount int64
	if err = target.QueryRow(ctx, `SELECT balance,version,(SELECT count(*) FROM v3_billing.ledger_entries),
	 (SELECT count(*) FROM v3_billing.historical_entries),(SELECT count(*) FROM v3_audit.events),
	 (SELECT sum(amount) FROM v3_billing.usage_logs),(SELECT count(*) FROM v3_billing.usage_logs)
	 FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=7 AND kind='wallet'`).Scan(&balance, &version, &liveEntries, &historicEntries, &events, &usageAmount, &usageCount); err != nil {
		t.Fatal(err)
	}
	if balance != 1000 || version != 1 || liveEntries != 1 || historicEntries != 2 || events != 4 || usageAmount != 120 || usageCount != 2 {
		t.Fatalf("balance=%d version=%d live=%d history=%d events=%d usage=%d/%d", balance, version, liveEntries, historicEntries, events, usageCount, usageAmount)
	}
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_billing.historical_accounts WHERE source_account_id LIKE 'retired-%'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retired accounts imported: count=%d err=%v", count, err)
	}
	var linkedUser, legacyBinding, legacyPasskey, signCount int64
	if err = target.QueryRow(ctx, `SELECT user_id,legacy_binding_id FROM v3_identity.user_identities WHERE provider='external_login' AND subject='kept-external-subject'`).Scan(&linkedUser, &legacyBinding); err != nil || linkedUser != 7 || legacyBinding != 42 {
		t.Fatalf("binding=%d/%d err=%v", linkedUser, legacyBinding, err)
	}
	if err = target.QueryRow(ctx, `SELECT legacy_id,(credential->'authenticator'->>'signCount')::bigint FROM v3_identity.passkeys`).Scan(&legacyPasskey, &signCount); err != nil || legacyPasskey != 31 || signCount != 17 {
		t.Fatalf("passkey=%d/%d err=%v", legacyPasskey, signCount, err)
	}
	var secret []byte
	if err = target.QueryRow(ctx, `SELECT secret_ciphertext FROM v3_identity.oauth_providers WHERE id=41`).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	plaintext, err := crypto.Decrypt(secret)
	if err != nil || string(plaintext) != "kept-secret" {
		t.Fatal("custom OAuth secret not usable after import")
	}
	if err = target.QueryRow(ctx, `SELECT count(*) FROM v3_audit.request_attempt_audits a JOIN v3_audit.request_audits r ON r.request_id=a.request_id WHERE a.attempt_id='kept-attempt' AND r.amount=100`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit link=%d err=%v", count, err)
	}
	checked := Report{}
	if err = pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(out pgx.Tx) error { return importer.checkHistory(ctx, out, d, &checked) }); err != nil || len(checked.Issues) > 0 || checked.Counts["check:history"] != 14 {
		t.Fatalf("history check=%+v err=%v", checked, err)
	}
	// Changed immutable source semantics must reject a second import, not replace
	// an original history record or post another charge.
	if _, err = target.Exec(ctx, `UPDATE v3_billing.historical_entries SET amount=-102 WHERE entry_id='debit-original'`); err != nil {
		t.Fatal(err)
	}
	checked = Report{}
	if err = pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(out pgx.Tx) error { return importer.checkHistory(ctx, out, d, &checked) }); err != nil || len(checked.Issues) == 0 {
		t.Fatalf("mutation not detected: %+v err=%v", checked, err)
	}
	if _, err = target.Exec(ctx, `DELETE FROM v3_audit.events WHERE id=53`); err != nil {
		t.Fatal(err)
	}
	checked = Report{}
	if err = pgx.BeginTxFunc(ctx, target, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(out pgx.Tx) error { return importer.checkHistory(ctx, out, d, &checked) }); err != nil || len(checked.Issues) < 2 {
		t.Fatalf("dropped log not detected: %+v err=%v", checked, err)
	}
	if err = pgx.BeginFunc(ctx, target, func(out pgx.Tx) error { return importer.importHistory(ctx, out, d) }); err == nil {
		t.Fatal("changed history semantics silently accepted")
	}
	assertHistoryPasskeyLogin(t, target, private)
}

func TestHistoryValidationRejectsOrphanBindingsAndLedgerOverflow(t *testing.T) {
	source, _, _ := importTestDB(t)
	historyFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `UPDATE migration_source.user_oauth_bindings SET provider_id=999; UPDATE billing.ledger_entries SET amount=9223372036854775807 WHERE entry_id='debit-original'`); err != nil {
		t.Fatal(err)
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sources, err := discoverSources(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadHistory(ctx, tx, sources)
	if err != nil {
		t.Fatal(err)
	}
	r := Report{}
	d.validate(&r)
	if len(r.Issues) != 2 {
		b, _ := json.Marshal(r)
		t.Fatalf("unsafe history report=%s", b)
	}
}

func TestHistoryWalkersPreserveOwnedJSONAndCallbackErrors(t *testing.T) {
	source, _, _ := importTestDB(t)
	historyFixture(t, source)
	ctx := context.Background()
	if _, err := source.Exec(ctx, `INSERT INTO gateway.request_attempt_audits
	 (attempt_id,request_id,attempt_no,duration_ms) VALUES
	 ('extra-linked','kept-request',2,9007199254740993),
	 ('extra-orphan','missing-parent',1,9223372036854775806);
	 UPDATE migration_source.logs SET id=9007199254740993 WHERE id=51`); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("历史😀\n\"\\", 4096)
	for _, table := range []string{"billing.ledger_entries", "migration_source.logs", "gateway.request_attempt_audits"} {
		if _, err := source.Exec(ctx, "ALTER TABLE "+table+" ADD COLUMN scan_payload jsonb"); err != nil {
			t.Fatal(err)
		}
		if _, err := source.Exec(ctx, "UPDATE "+table+` SET scan_payload=jsonb_build_object(
		 'exact_int',9007199254740993::bigint,'text',$1::text,
		 'nested',jsonb_build_array(null,true,jsonb_build_object('kept',$1::text)))`, payload); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, tc := range []struct {
		name, table, key string
		flags            map[string]bool
		walk             func(func(json.RawMessage, bool) error) error
	}{
		{"history", "billing.ledger_entries", "entry_id", nil, func(visit func(json.RawMessage, bool) error) error {
			return walkHistory(ctx, tx, "billing.ledger_entries", func(raw json.RawMessage) error { return visit(raw, false) })
		}},
		{"logs", "migration_source.logs", "id", map[string]bool{"9007199254740993": true, "54": true}, func(visit func(json.RawMessage, bool) error) error {
			return walkHistoryLogs(ctx, tx, "migration_source.logs", visit)
		}},
		{"attempts", "gateway.request_attempt_audits", "attempt_id", map[string]bool{`"extra-orphan"`: true}, func(visit func(json.RawMessage, bool) error) error {
			return walkHistoryAttempts(ctx, tx, "gateway.request_attempt_audits", "gateway.request_audits", visit)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := map[string]string{}
			rows, err := tx.Query(ctx, "SELECT to_jsonb(t)::text FROM "+tc.table+" t")
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var text string
				if err := rows.Scan(&text); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal([]byte(text), &fields); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				expected[string(fields[tc.key])] = text
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			retained := map[string]json.RawMessage{}
			err = tc.walk(func(raw json.RawMessage, flag bool) error {
				if len(raw) < 128*1024 {
					t.Fatalf("large source payload not exercised: %d bytes", len(raw))
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(raw, &fields); err != nil {
					return err
				}
				key := string(fields[tc.key])
				if string(raw) != expected[key] || flag != tc.flags[key] {
					t.Fatalf("source JSON or classification changed for %s", key)
				}
				var got struct {
					ExactInt int64             `json:"exact_int"`
					Text     string            `json:"text"`
					Nested   []json.RawMessage `json:"nested"`
				}
				if err := json.Unmarshal(fields["scan_payload"], &got); err != nil {
					return err
				}
				var nested struct{ Kept string }
				if len(got.Nested) != 3 || string(got.Nested[0]) != "null" || string(got.Nested[1]) != "true" {
					t.Fatal("nested JSON changed")
				}
				if err := json.Unmarshal(got.Nested[2], &nested); err != nil {
					return err
				}
				if got.ExactInt != 9007199254740993 || got.Text != payload || nested.Kept != payload {
					t.Fatal("integer precision or non-ASCII payload changed")
				}
				retained[key] = raw
				for savedKey, saved := range retained {
					if string(saved) != expected[savedKey] {
						t.Fatalf("retained JSON overwritten after Next: %s", savedKey)
					}
				}
				return nil
			})
			if err != nil || len(retained) != len(expected) || len(retained) < 2 {
				t.Fatalf("walker rows=%d expected=%d err=%v", len(retained), len(expected), err)
			}
			var unrelated string
			if err := tx.QueryRow(ctx, "SELECT repeat('overwrite',40000)").Scan(&unrelated); err != nil {
				t.Fatal(err)
			}
			for key, saved := range retained {
				if string(saved) != expected[key] {
					t.Fatalf("retained JSON overwritten after another query: %s", key)
				}
			}
			callbackErr := errors.New("history callback failure")
			calls := 0
			if err := tc.walk(func(json.RawMessage, bool) error {
				calls++
				if calls == 2 {
					return callbackErr
				}
				return nil
			}); err != callbackErr || calls != 2 {
				t.Fatalf("callback error changed: calls=%d err=%v", calls, err)
			}
			var one int
			if err := tx.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
				t.Fatalf("transaction connection unusable after callback error: %d %v", one, err)
			}
			t.Logf("verified %d source rows, retained large JSON and callback error identity", len(retained))
		})
	}
}
