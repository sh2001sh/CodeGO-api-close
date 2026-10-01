package catalog

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestRetainedMetadataAndScoredPoolsSurviveSnapshotWire(t *testing.T) {
	cipher, err := NewAESGCM(bytes.Repeat([]byte{23}, 32))
	if err != nil {
		t.Fatal(err)
	}
	snap := &Snapshot{
		Metadata: MetadataSnapshot{
			Vendors:       []VendorMetadata{{ID: 17, Name: "retained-vendor", Description: "description", Icon: "provider.svg", Status: 1}},
			Models:        []ModelMetadata{{ID: 29, ModelName: "gpt", NameRule: NameRulePrefix, VendorID: 17, Tags: "retained"}},
			PrefillGroups: []PrefillGroup{{ID: 31, Name: "endpoints", Type: "endpoint", Items: json.RawMessage(`{"chat":{"path":"/v1/chat/completions"}}`)}},
		},
		OfficialPools: map[string]OfficialPool{"default": {
			ID: 9007199254740993, Name: "source-costs", Group: "default", TTFTWeight: 25,
			Members: []OfficialPoolMember{{ChannelID: 21, CostMultiplier: "0.123456789012345678", ModelCostOverrides: map[string]json.Number{"gpt": "0.999999999999999999"}, Models: []string{"gpt"}, FaultDomain: "isolated"}},
		}},
	}
	wire, err := sealSnapshot(snap, cipher)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	wire, err = decodeWireSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := openSnapshot(wire, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Metadata, snap.Metadata) || !reflect.DeepEqual(restored.OfficialPools, snap.OfficialPools) {
		t.Fatal("retained metadata/scored procurement facts changed over Redis serialization")
	}
	model, vendor := restored.Metadata.Describe("gpt-mini")
	if model == nil || vendor == nil || model.ID != 29 || vendor.Name != "retained-vendor" {
		t.Fatal("restored metadata cannot describe an actual matched model")
	}
}
