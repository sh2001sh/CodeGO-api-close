package legacy

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func cmTestRow(t *testing.T, raw string) cmRow {
	t.Helper()
	var row cmRow
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestMarketInviteDigestEncodingsPreserveBytesAndRejectNoncanonicalInput(t *testing.T) {
	digest := sha256.Sum256([]byte("legacy-invite-original-token"))
	hexDigest := hex.EncodeToString(digest[:])
	rawURL := base64.RawURLEncoding.EncodeToString(digest[:])
	want := "\\x" + hexDigest
	for _, value := range []string{hexDigest, strings.ToUpper(hexDigest), rawURL} {
		got, err := cmInviteHash(value)
		if err != nil || got != want {
			t.Fatalf("invite digest bytes changed: got=%s err=%v", got, err)
		}
		d := cmUnitData(t)
		d.rows["group_invites"] = []cmRow{cmTestRow(t, `{"id":91,"group_id":"g-201","created_by":7,"token_hash":"`+value+`"}`)}
		d.prepare("")
		if len(d.issues) != 0 {
			t.Fatalf("valid invite encoding blocked projection: %+v", d.issues)
		}
		var projected bool
		for _, record := range d.records {
			if record.table == "v3_channelmarket.group_invites" {
				projected = record.values["token_hash"] == want
			}
		}
		if !projected {
			t.Fatal("invite projection did not retain original SHA256 bytes")
		}
	}
	// A loose decoder accepts B's nonzero padding bits as the same final byte.
	// Strict canonical decoding must reject it rather than normalize the input.
	noncanonical := strings.Repeat("A", 42) + "B"
	if decoded, err := base64.RawURLEncoding.DecodeString(noncanonical); err != nil || len(decoded) != sha256.Size {
		t.Fatal("noncanonical boundary did not exercise a decodable SHA256 digest")
	}
	for _, value := range []string{"", hexDigest[:62], hexDigest + "00", strings.Repeat("z", 64), rawURL[:42], rawURL + "A", rawURL + "=", rawURL + "\n", strings.Repeat("/", 42) + "8", noncanonical} {
		if _, err := cmInviteHash(value); err == nil {
			t.Fatalf("invalid invite digest accepted: %q", value)
		}
	}
	if _, err := cmHex(rawURL); err == nil {
		t.Fatal("non-invite SHA256 fields accepted Base64URL")
	}
}

func TestMarketExactFactorAndSecretMigration(t *testing.T) {
	for _, test := range []struct {
		value string
		zero  bool
		want  int64
		bad   bool
	}{{"0.075", false, 75000, false}, {"1.000001", false, 1000001, false}, {"0", true, 0, false}, {"0", false, 0, true}, {"0.0000001", false, 0, true}, {"9223372036854.775808", false, 0, true}, {"NaN", false, 0, true}} {
		got, err := cmFactor(test.value, test.zero)
		if (err != nil) != test.bad || (!test.bad && got != test.want) {
			t.Errorf("%s: got=%d err=%v", test.value, got, err)
		}
	}
	digest := sha256.Sum256([]byte("fixture-source-secret"))
	enc, err := catalog.NewAESGCM(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := enc.Encrypt([]byte("fixture-credential"))
	if err != nil {
		t.Fatal(err)
	}
	value := "enc:v1:" + base64.RawURLEncoding.EncodeToString(sealed)
	plain, err := cmSecret(value, "fixture-source-secret")
	if err != nil || plain != "fixture-credential" {
		t.Fatal("credential round trip failed")
	}
	for _, key := range []string{"", "wrong-source-secret"} {
		_, err = cmSecret(value, key)
		if err == nil || strings.Contains(err.Error(), "fixture-credential") {
			t.Fatal("credential failure was not safely rejected")
		}
	}
	sealed[len(sealed)-1] ^= 1
	if _, err = cmSecret("enc:v1:"+base64.RawURLEncoding.EncodeToString(sealed), "fixture-source-secret"); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func cmUnitData(t *testing.T) *channelMarketData {
	return &channelMarketData{rows: map[string][]cmRow{
		"channels": {cmTestRow(t, `{"id":"public-not-numeric-201","owner_user_id":7,"internal_channel_id":13,"provider_type":"openai","declared_models":"[\"chat-model\"]","model_prices":"{}","model_verification_results":"{}","gpt56_mapping_results":"{}","transport_capabilities":"{}"}`)},
		"groups":   {cmTestRow(t, `{"id":"g-201","channel_id":"public-not-numeric-201","owner_user_id":7,"public_slug":"my-channel","internal_group_name":"market_internal","system_display_name":"Fixture","multiplier":0.075,"visibility":"private","lifecycle_status":"active","verification_status":"passed"}`)},
	}, channels: map[string]*cmChannel{}, groups: map[string]cmRow{}, users: map[int64]bool{7: true, 8: true}, internal: map[int64]cmRow{13: cmTestRow(t, `{"id":13,"group":"default"}`)}, pending: map[int64]int64{}}
}

func TestMarketPreparePreservesIDsAndRejectsBadRelationships(t *testing.T) {
	d := cmUnitData(t)
	d.rows["settlements"] = []cmRow{cmTestRow(t, `{"id":"s-201","request_id":"request-201","group_id":"g-201","owner_user_id":7,"consumer_user_id":8,"consumer_amount":500,"settlement_gross_amount":100,"platform_commission":5,"transaction_fee":0,"owner_net_amount":95,"reclaimed_amount":0,"multiplier":0.075,"subscription_multiplier":0.75,"status":"pending","available_at":"2026-09-30T00:00:00Z"}`)}
	d.prepare("")
	if len(d.issues) != 0 {
		t.Fatalf("issues=%+v", d.issues)
	}
	if d.channels["public-not-numeric-201"].catalogID != 13 || d.pending[7] != 190 {
		t.Fatal("public ID or money mapping changed")
	}
	for _, r := range d.records {
		if r.table == "v3_channelmarket.settlements" && r.values["gross_micro"] != int64(200) {
			t.Fatal("settlement conversion incorrect")
		}
	}
	d = cmUnitData(t)
	d.rows["channel_user_blocks"] = []cmRow{cmTestRow(t, `{"id":1,"channel_id":"missing-public-id","user_id":8}`)}
	d.prepare("")
	if len(d.issues) == 0 {
		t.Fatal("missing referenced market channel accepted")
	}
	d = cmUnitData(t)
	d.rows["user_multipliers"] = []cmRow{cmTestRow(t, `{"id":1,"channel_id":"public-not-numeric-201","user_id":999,"multiplier":1}`)}
	d.prepare("")
	if len(d.issues) == 0 {
		t.Fatal("missing consumer accepted")
	}
}

func TestMarketMoneyBigintBoundary(t *testing.T) {
	b := cmBuild()
	row := cmRow{"amount": json.RawMessage("4611686018427387903")}
	if got := b.money(row, "amount", "amount_micro"); b.err != nil || got != math.MaxInt64-1 {
		t.Fatalf("boundary=%d err=%v", got, b.err)
	}
	b = cmBuild()
	row["amount"] = json.RawMessage("4611686018427387904")
	b.money(row, "amount", "amount_micro")
	if b.err == nil {
		t.Fatal("overflowing market amount accepted")
	}
}

func TestMarketLegacyKeyBindingsRemainUsable(t *testing.T) {
	d := cmUnitData(t)
	d.keyBindings = []cmKeyBinding{{ID: 11, UserID: 8, Group: "market:auto"}}
	d.rows["route_pools"] = []cmRow{cmTestRow(t, `{"id":"named","owner_user_id":8,"name":"Named","strategy":"score","max_attempts":3,"failure_cooldown_seconds":30,"max_multiplier":0,"auto_build_enabled":true,"auto_build_interval":60,"auto_build_models":"[\"chat-model\"]"}`)}
	d.prepare("")
	if len(d.issues) != 0 {
		t.Fatalf("issues=%+v", d.issues)
	}
	for source, want := range map[string]string{"market:auto": "pool_auto:8", "market:pool:named": "pool_named", "market:g-201": "market_internal", "market:pool:deleted": "market:pool:deleted", "default": "default"} {
		if got := d.targetKeyGroup(source, 8); got != want {
			t.Errorf("source=%s got=%s want=%s", source, got, want)
		}
	}
	var configured bool
	for _, record := range d.records {
		if record.table == "v3_channelmarket.route_pools" && record.values["id"] == "named" {
			payload, err := json.Marshal(record.values["config"])
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				AutoBuild struct {
					Enabled  bool     `json:"enabled"`
					Interval int      `json:"interval_minutes"`
					Models   []string `json:"models"`
				} `json:"auto_build"`
			}
			if err = json.Unmarshal(payload, &config); err != nil {
				t.Fatal(err)
			}
			configured = config.AutoBuild.Enabled && config.AutoBuild.Interval == 60 && len(config.AutoBuild.Models) == 1
		}
	}
	if !configured {
		t.Fatal("source scheduled builder configuration does not activate native worker")
	}
}
