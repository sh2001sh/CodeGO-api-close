//go:build pgintegration

package legacy

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/desktop"
)

func TestRestoredStateReadonlyReplayCheckCredentialsAndRevocation(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedRestoredStateFixture(t, source)
	before := restoredSourceDigest(t, source)
	ctx := context.Background()
	importer := NewImporter(readonlySource(t, source), target, crypto).WithSourceCryptoSecret(restoredTestCrypto)
	preview, err := importer.Import(ctx, false)
	if err != nil || len(preview.Issues) != 0 || preview.Counts["restored.two_fas"] != 1 || preview.Counts["restored.two_fa_backup_codes"] != 2 || preview.Counts["restored.model_favorites"] != 1 {
		t.Fatalf("restored preview=%+v err=%v", preview, err)
	}
	for attempt := range 2 {
		if report, err := importer.Import(ctx, true); err != nil || !report.Applied {
			t.Fatalf("restored import%d=%+v err=%v", attempt, report, err)
		}
		if report, err := importer.Check(ctx); err != nil || len(report.Issues) != 0 || report.Counts["check:restored_state"] != 9 {
			t.Fatalf("restored check%d=%+v err=%v", attempt, report, err)
		}
	}
	if after := restoredSourceDigest(t, source); after != before {
		t.Fatal("restored import changed source data")
	}
	var sealed []byte
	var enabled bool
	var failed int64
	var counter int64
	var locked time.Time
	if err = target.QueryRow(ctx, `SELECT secret_ciphertext,enabled,failed_attempts,last_counter,locked_until FROM v3_identity.two_factor WHERE user_id=7`).Scan(&sealed, &enabled, &failed, &counter, &locked); err != nil {
		t.Fatal(err)
	}
	plain, err := crypto.Decrypt(sealed)
	if err != nil || string(plain) != restoredTestTOTP || !enabled || failed != 4 || counter != 1700000000/30+1 || locked.Unix() != 1700000400 {
		t.Fatal("TOTP secret, enabled state, lockout or replay guard changed")
	}
	if err = target.QueryRow(ctx, `SELECT ciphertext FROM v3_platform.settings WHERE key='model_deployment.ionet.api_key' AND sensitive`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	plain, err = crypto.Decrypt(sealed)
	if err != nil || string(plain) != `"local-original-deployment-token"` {
		t.Fatal("source deployment credential was not decrypted and resealed")
	}
	var used, unused int64
	if err = target.QueryRow(ctx, `SELECT count(*) FILTER(WHERE used_at IS NOT NULL),count(*) FILTER(WHERE used_at IS NULL) FROM v3_identity.two_factor_backup_codes WHERE user_id=7`).Scan(&used, &unused); err != nil || used != 1 || unused != 1 {
		t.Fatal("backup-code consumption changed")
	}
	service := desktop.New(target, nil, desktop.Config{Crypto: crypto, Now: func() time.Time { return time.Unix(1700000001, 0) }})
	for _, test := range []struct {
		token, scope string
		allow        bool
	}{
		{"desktop_original_active_fixture", "account:read", true},
		{"desktop_original_active_fixture", "telemetry:write", true},
		{"desktop_original_active_fixture", "tokens:write", false},
		{"desktop_original_revoked_fixture", "account:read", false},
		{"desktop_original_expired_fixture", "account:read", false},
	} {
		request := httptest.NewRequest("GET", "/api/desktop/account", nil)
		request.Header.Set("Authorization", "Bearer "+test.token)
		device, err := service.Authenticate(request, test.scope)
		if (err == nil) != test.allow || test.allow && (device.ID != 51 || device.UserID != 7) {
			t.Fatalf("migrated desktop access allow=%t gotID=%d err=%v", test.allow, device.ID, err)
		}
	}
	poll, err := service.Poll(ctx, "original-session")
	if err != nil || poll.Status != "approved" || !poll.Authenticated || poll.AccessToken != "desktop_original_active_fixture" {
		t.Fatal("existing unexpired desktop authorization cannot be polled")
	}
	poll, err = service.Poll(ctx, "expired-session")
	if err != nil || poll.Status != "expired" || poll.Authenticated || poll.AccessToken != "" {
		t.Fatal("expired legacy authorization revived")
	}
}

func TestRestoredStateMissingOrWrongSourceSecretBlocksAtomically(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedRestoredStateFixture(t, source)
	before := restoredSourceDigest(t, source)
	ctx := context.Background()
	for _, secret := range []string{"", "wrong-source-secret"} {
		importer := NewImporter(readonlySource(t, source), target, crypto).WithSourceCryptoSecret(secret)
		preview, err := importer.Import(ctx, false)
		if err != nil || len(preview.Issues) != 2 {
			t.Fatalf("bad source crypto preview issues=%d err=%v", len(preview.Issues), err)
		}
		if report, err := importer.Import(ctx, true); err == nil || report.Applied {
			t.Fatal("missing/wrong source crypto allowed apply")
		}
	}
	var count int64
	if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed source crypto import wrote target users")
	}
	if restoredSourceDigest(t, source) != before {
		t.Fatal("bad source crypto changed original data")
	}
}

func TestRestoredStateCheckAndReplayRejectTargetTampering(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE v3_identity.two_factor SET enabled=false WHERE user_id=7`,
		`UPDATE v3_identity.two_factor_backup_codes SET used_at=NULL WHERE id=62`,
		`UPDATE v3_identity.desktop_devices SET revoked_at=NULL WHERE id=52`,
		`UPDATE v3_identity.desktop_devices SET token_ciphertext='invalid'::bytea WHERE id=51`,
		`DELETE FROM v3_adminops.model_favorites WHERE user_id=7`,
		`UPDATE v3_platform.settings SET ciphertext='invalid'::bytea WHERE key='model_deployment.ionet.api_key'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			seedRestoredStateFixture(t, source)
			ctx := context.Background()
			importer := NewImporter(readonlySource(t, source), target, crypto).WithSourceCryptoSecret(restoredTestCrypto)
			if _, err := importer.Import(ctx, true); err != nil {
				t.Fatal(err)
			}
			if _, err := target.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			if report, err := importer.Check(ctx); err == nil || len(report.Issues) == 0 {
				t.Fatal("target tampering escaped check")
			}
			// A removed source row may be re-created. Changed surviving values
			// must never be overwritten by replay.
			if mutation[0:6] != "DELETE" {
				if report, err := importer.Import(ctx, true); err == nil || report.Applied {
					t.Fatal("target tampering escaped apply conflict")
				}
			}
		})
	}
}

func TestRestoredStateMalformedSourceBlocksBeforeAnyTargetWrites(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE migration_source.two_fa_backup_codes SET is_used=false WHERE id=62`,
		`UPDATE migration_source.desktop_auth_sessions SET device_id=999 WHERE status='approved'`,
		`UPDATE migration_source.users SET setting='{"favorite_model_ids":[32]}' WHERE id=7`,
		`UPDATE migration_source.desktop_authorized_devices SET scopes='desktop:root:write' WHERE id=51`,
	} {
		t.Run(mutation, func(t *testing.T) {
			source, target, crypto := importTestDB(t)
			seedRestoredStateFixture(t, source)
			ctx := context.Background()
			if _, err := source.Exec(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			before := restoredSourceDigest(t, source)
			importer := NewImporter(readonlySource(t, source), target, crypto).WithSourceCryptoSecret(restoredTestCrypto)
			if report, err := importer.Import(ctx, true); err == nil || len(report.Issues) == 0 || report.Applied {
				t.Fatal("malformed restored source was accepted")
			}
			var count int64
			if err := target.QueryRow(ctx, `SELECT count(*) FROM v3_identity.users`).Scan(&count); err != nil || count != 0 {
				t.Fatal("invalid source partially wrote target")
			}
			if restoredSourceDigest(t, source) != before {
				t.Fatal("malformed source validation changed source")
			}
		})
	}
}
