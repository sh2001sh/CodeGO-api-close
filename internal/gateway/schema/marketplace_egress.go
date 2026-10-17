package schema

import (
	"encoding/json"
	"strings"
)

// RequiresMarketplaceEgress identifies an owner-created internal channel from
// server-owned metadata, including administrator calls without a group binding.
// It is an egress decision only and must not synthesize pricing or ownership.
func (channel *Channel) RequiresMarketplaceEgress() bool {
	if channel == nil || channel.IsOfficial() {
		return false
	}
	var metadata struct {
		ChannelID string `json:"marketplace_channel_id"`
		GroupID   string `json:"marketplace_group_id"`
		Source    string `json:"source_type"`
	}
	return json.Unmarshal([]byte(channel.OtherInfo), &metadata) == nil &&
		metadata.Source == "marketplace_user" && strings.TrimSpace(metadata.ChannelID) != "" && strings.TrimSpace(metadata.GroupID) != ""
}
