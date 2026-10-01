package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestMetadataNameRulesRetainLiteralCaseSensitiveMatching(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		rule    int
		name    string
		want    bool
	}{
		{"gpt", 0, "gpt", true}, {"gpt", 0, "gpt-4", false},
		{"gpt", 1, "gpt-4", true}, {"gpt", 1, "x-gpt", false},
		{"gpt", 2, "x-gpt-4", true}, {"gpt", 3, "x-gpt", true},
		{"gpt", 3, "gpt-4", false}, {"GPT", 1, "gpt-4", false},
		{"*", 2, "gpt-4", false}, {"*", 2, "gpt*-4", true},
		{"%", 1, "gpt-4", false}, {"", 1, "gpt", false},
		{"gpt", 4, "gpt", false}, {"gpt", 0, "", false},
	} {
		if got := MatchesModelName(tc.pattern, tc.rule, tc.name); got != tc.want {
			t.Errorf("pattern=%q rule=%d name=%q: %t, want %t", tc.pattern, tc.rule, tc.name, got, tc.want)
		}
	}
}

func TestMetadataDescriptionUsesSourcePricingPrecedence(t *testing.T) {
	snapshot := MetadataSnapshot{
		Vendors: []VendorMetadata{{ID: 8, Name: "source-vendor"}},
		Models: []ModelMetadata{
			{ID: 1, ModelName: "model", NameRule: NameRuleContains},
			{ID: 2, ModelName: "model", NameRule: NameRuleSuffix},
			{ID: 10, ModelName: "prefix", NameRule: NameRulePrefix},
			{ID: 5, ModelName: "prefix-", NameRule: NameRulePrefix, VendorID: 8},
			{ID: 99, ModelName: "prefix-model", NameRule: NameRuleExact, VendorID: 8, Status: 0},
		},
	}
	model, vendor := snapshot.Describe("prefix-model")
	if model == nil || model.ID != 99 || vendor == nil || vendor.Name != "source-vendor" {
		t.Fatalf("exact metadata/vendor lost: %+v, %+v", model, vendor)
	}
	snapshot.Models = snapshot.Models[:4]
	model, vendor = snapshot.Describe("prefix-model")
	if model == nil || model.ID != 5 || vendor == nil || vendor.ID != 8 {
		t.Fatalf("prefix precedence or lower ID tie break lost: %+v, %+v", model, vendor)
	}
	snapshot.Models = snapshot.Models[:2]
	model, _ = snapshot.Describe("prefix-model")
	if model == nil || model.ID != 2 {
		t.Fatal("suffix must outrank contains")
	}
	model, _ = snapshot.Describe("prefix-model-trailer")
	if model == nil || model.ID != 1 {
		t.Fatal("contains fallback lost")
	}
	model, vendor = snapshot.Describe("unconfigured")
	if model != nil || vendor != nil {
		t.Fatal("unmatched model acquired metadata")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var reopened MetadataSnapshot
	if err = json.Unmarshal(encoded, &reopened); err != nil || !reflect.DeepEqual(snapshot, reopened) {
		t.Fatalf("snapshot JSON round trip: %v", err)
	}
}

type metadataQueryFailure struct{ cause error }

func (q metadataQueryFailure) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, q.cause
}

func TestMetadataLoaderDoesNotSwallowDatabaseFailure(t *testing.T) {
	cause := errors.New("metadata connection unavailable")
	_, err := ReadMetadata(context.Background(), metadataQueryFailure{cause})
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "vendors") {
		t.Fatalf("lost loader cause/context: %v", err)
	}
}

func TestMetadataSurvivesEncryptedSnapshotWire(t *testing.T) {
	cipher, err := NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &Snapshot{Version: 3, Metadata: MetadataSnapshot{
		Vendors:       []VendorMetadata{{ID: 42, Name: "imported", Status: 0}},
		Models:        []ModelMetadata{{ID: 84, ModelName: "gpt-", NameRule: NameRulePrefix, VendorID: 42, Endpoints: `{"openai":"/v1/chat/completions"}`}},
		PrefillGroups: []PrefillGroup{{ID: 96, Name: "models", Type: "model", Items: json.RawMessage(`["gpt-4"]`)}},
	}}
	sealed, err := sealSnapshot(snapshot, cipher)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := decodeWireSnapshot(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := openSnapshot(wire, cipher)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.Metadata, reopened.Metadata) {
		t.Fatal("model/vendor/prefill metadata disappeared during encrypted publication")
	}
	model, vendor := reopened.Metadata.Describe("gpt-4")
	if model == nil || model.ID != 84 || vendor == nil || vendor.ID != 42 || model.Endpoints != snapshot.Metadata.Models[0].Endpoints {
		t.Fatal("discovery metadata cannot be consumed after publication")
	}
}
