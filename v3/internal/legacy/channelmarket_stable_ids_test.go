package legacy

import (
	"strconv"
	"testing"
)

func addPublicOnlyChannel(t *testing.T, d *channelMarketData, id, group string) {
	t.Helper()
	d.rows["channels"] = append(d.rows["channels"], cmTestRow(t, `{"id":"`+id+`","owner_user_id":7,"internal_channel_id":0,"provider_type":"openai","base_url_ciphertext":"https://example.invalid","credential_ciphertext":"fixture-new-credential","declared_models":"[\"chat-model\"]","model_prices":"{}","model_verification_results":"{}","gpt56_mapping_results":"{}","transport_capabilities":"{}"}`))
	d.rows["groups"] = append(d.rows["groups"], cmTestRow(t, `{"id":"`+group+`","channel_id":"`+id+`","owner_user_id":7,"public_slug":"`+group+`","internal_group_name":"`+group+`","system_display_name":"New","multiplier":1,"visibility":"private","lifecycle_status":"draft","verification_status":"pending"}`))
}

func TestMarketGeneratedCatalogIDsSurviveNewSourceChannels(t *testing.T) {
	before := cmUnitData(t)
	addPublicOnlyChannel(t, before, "m-existing", "g-existing")
	before.prepare("")
	if len(before.issues) != 0 {
		t.Fatal(before.issues)
	}
	oldID := before.channels["m-existing"].catalogID
	if oldID < 1<<46 || oldID >= 1<<47 {
		t.Fatalf("generated ID outside safe integer range: %d", oldID)
	}
	after := cmUnitData(t)
	addPublicOnlyChannel(t, after, "m-existing", "g-existing")
	// This channel sorts first; the old maximum-based allocator shifted every
	// existing public-only mapping even though no ownership had changed.
	addPublicOnlyChannel(t, after, "a-new", "g-new")
	after.internal[856] = cmTestRow(t, `{"id":856,"group":"default"}`)
	after.prepare("")
	if len(after.issues) != 0 || after.channels["m-existing"].catalogID != oldID || after.channels["a-new"].catalogID == oldID || after.channels["public-not-numeric-201"].catalogID != 13 {
		t.Fatalf("new source channels changed existing mappings: before=%d after=%+v issues=%+v", oldID, after.channels, after.issues)
	}
}

func TestMarketGeneratedCatalogIDCollisionIsRejected(t *testing.T) {
	d := cmUnitData(t)
	addPublicOnlyChannel(t, d, "m-existing", "g-existing")
	id := cmGeneratedCatalogID("m-existing")
	d.internal[id] = cmTestRow(t, `{"id":`+strconv.FormatInt(id, 10)+`,"group":"default"}`)
	d.prepare("")
	if len(d.issues) == 0 {
		t.Fatal("generated ID collision with an existing source channel was accepted")
	}
}
