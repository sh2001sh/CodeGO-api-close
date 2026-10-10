package legacy

import (
	"encoding/json"
	"testing"
)

func TestMarketMissingGatewayParentRequiresDeletedChannelAndUniqueDeletedGroup(t *testing.T) {
	stamp := json.RawMessage(`"2026-10-01T00:00:00Z"`)
	for _, test := range []struct {
		name   string
		change func(*channelMarketData)
		valid  bool
	}{
		{"both_deleted", func(d *channelMarketData) {}, true},
		{"active_channel", func(d *channelMarketData) { delete(d.rows["channels"][0], "deleted_at") }, false},
		{"active_group", func(d *channelMarketData) { delete(d.rows["groups"][0], "deleted_at") }, false},
		{"invalid_channel_deletion", func(d *channelMarketData) { d.rows["channels"][0]["deleted_at"] = json.RawMessage(`"invalid"`) }, false},
		{"invalid_group_deletion", func(d *channelMarketData) { d.rows["groups"][0]["deleted_at"] = json.RawMessage(`"invalid"`) }, false},
		{"missing_group", func(d *channelMarketData) { d.rows["groups"] = nil }, false},
		{"multiple_groups", func(d *channelMarketData) { d.rows["groups"] = append(d.rows["groups"], d.rows["groups"][0]) }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := cmUnitData(t)
			delete(d.internal, 13)
			d.rows["channels"][0]["deleted_at"] = stamp
			d.rows["groups"][0]["deleted_at"] = stamp
			test.change(d)
			d.prepare("")
			if (len(d.issues) == 0) != test.valid {
				t.Fatalf("valid=%t issues=%+v", test.valid, d.issues)
			}
			if test.valid && (!d.channels["public-not-numeric-201"].archiveParent || d.channels["public-not-numeric-201"].newCatalog || d.channels["public-not-numeric-201"].catalogID != 13 || len(d.internal) != 0) {
				t.Fatal("deleted parent identity changed or fabricated a source gateway row")
			}
		})
	}
}

func TestMarketDeletedParentIDsStayDistinctFromNewCatalogIDs(t *testing.T) {
	d := cmUnitData(t)
	delete(d.internal, 13)
	d.rows["channels"][0]["deleted_at"] = json.RawMessage(`"2026-10-01T00:00:00Z"`)
	d.rows["groups"][0]["deleted_at"] = json.RawMessage(`"2026-10-01T00:00:00Z"`)
	d.rows["channels"] = append(d.rows["channels"], cmTestRow(t, `{"id":"a-new-public","owner_user_id":7,"internal_channel_id":0,"provider_type":"openai","base_url_ciphertext":"https://example.invalid","credential_ciphertext":"fixture-new-credential","declared_models":"[\"chat-model\"]","model_prices":"{}","model_verification_results":"{}","gpt56_mapping_results":"{}","transport_capabilities":"{}"}`))
	d.rows["groups"] = append(d.rows["groups"], cmTestRow(t, `{"id":"g-new","channel_id":"a-new-public","owner_user_id":7,"public_slug":"new-channel","internal_group_name":"new_internal","system_display_name":"New","multiplier":1,"visibility":"private","lifecycle_status":"draft","verification_status":"pending"}`))
	d.prepare("")
	if len(d.issues) != 0 || d.channels["public-not-numeric-201"].catalogID != 13 || d.channels["a-new-public"].catalogID == 13 {
		t.Fatalf("deleted/new IDs collide: %+v issues=%+v", d.channels, d.issues)
	}
}
