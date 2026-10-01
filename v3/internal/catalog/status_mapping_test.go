package catalog

import "testing"

func TestStatusMappingLegacyAndInvalidValues(t *testing.T) {
	values, err := ParseStatusCodeMapping([]byte(`{"400":503,"401":"429"}`))
	if err != nil || values["400"] != 503 || values["401"] != 429 {
		t.Fatalf("legacy mapping: %v %v", values, err)
	}
	for _, raw := range []string{`null`, `[]`, `{"0":503}`, `{"400":600}`, `{"400":true}`, `{"400":400.5}`, `{"400":"bad"}`} {
		if _, err := ParseStatusCodeMapping([]byte(raw)); err == nil {
			t.Fatalf("invalid mapping accepted: %s", raw)
		}
	}
}
