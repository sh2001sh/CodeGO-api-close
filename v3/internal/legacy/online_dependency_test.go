package legacy

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestOnlineGeneratedBindingRejectsOwnerAndRealRemapping(t *testing.T) {
	id := "public-channel"
	market := &channelMarketData{channels: map[string]*cmChannel{id: {publicID: id, catalogID: 13, owner: 7}}}
	for _, test := range []struct {
		name, key, before, after string
		want                     bool
	}{
		{"reviewed_binding", "market.channel:" + id, fmt.Sprintf("[%d,7]", cmGeneratedCatalogID(id)), "[13,7]", true},
		{"owner_changed", "market.channel:" + id, fmt.Sprintf("[%d,8]", cmGeneratedCatalogID(id)), "[13,7]", false},
		{"real_remapped", "market.channel:" + id, "[12,7]", "[13,7]", false},
		{"wrong_generated_id", "market.channel:" + id, fmt.Sprintf("[%d,7]", cmGeneratedCatalogID(id)+1), "[13,7]", false},
		{"wrong_current_id", "market.channel:" + id, fmt.Sprintf("[%d,7]", cmGeneratedCatalogID(id)), "[14,7]", false},
		{"other_dependency", "funding.account:" + id, "[12,7]", "[13,7]", false},
		{"missing_channel", "market.channel:missing", "[12,7]", "[13,7]", false},
		{"invalid_array", "market.channel:" + id, "[7]", "[13,7]", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := onlineGeneratedChannelBound(test.key, json.RawMessage(test.before), json.RawMessage(test.after), market); got != test.want {
				t.Fatalf("accepted=%v want=%v", got, test.want)
			}
		})
	}
}
