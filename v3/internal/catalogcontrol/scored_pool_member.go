package catalogcontrol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// V2 encodes model cost overrides as a JSON string and exposes its member
// identity as id. Both shapes preserve the original exact JSON number text.
func (m *PoolMember) UnmarshalJSON(raw []byte) error {
	var input struct {
		ChannelID          int64           `json:"channel_id"`
		Priority           int             `json:"priority"`
		Weight             int             `json:"weight"`
		LegacyID           int64           `json:"legacy_id"`
		ID                 int64           `json:"id"`
		PoolID             int64           `json:"route_pool_id"`
		CostMultiplier     json.Number     `json:"cost_multiplier"`
		ModelCostOverrides json.RawMessage `json:"model_cost_overrides"`
		FaultDomain        string          `json:"fault_domain"`
		Enabled            *bool           `json:"enabled"`
		DeletedAt          json.RawMessage `json:"deleted_at"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return err
	}
	if input.LegacyID != 0 && input.ID != 0 && input.ID != input.LegacyID {
		return fmt.Errorf("conflicting pool member identity")
	}
	*m = PoolMember{ChannelID: input.ChannelID, Priority: input.Priority, Weight: input.Weight, LegacyID: input.LegacyID, CostMultiplier: input.CostMultiplier, FaultDomain: input.FaultDomain, Enabled: input.Enabled}
	if m.LegacyID == 0 {
		m.LegacyID = input.ID
	}
	overrides := bytes.TrimSpace(input.ModelCostOverrides)
	if len(overrides) > 0 && overrides[0] == '"' {
		var text string
		if err := json.Unmarshal(overrides, &text); err != nil {
			return err
		}
		overrides = bytes.TrimSpace([]byte(text))
	}
	if len(overrides) > 0 && !bytes.Equal(overrides, []byte("null")) {
		if err := json.Unmarshal(overrides, &m.ModelCostOverrides); err != nil || m.ModelCostOverrides == nil {
			return fmt.Errorf("model cost overrides require a numeric object")
		}
	}
	if len(input.DeletedAt) > 0 {
		if err := json.Unmarshal(input.DeletedAt, &m.DeletedAt); err != nil {
			return err
		}
	}
	return nil
}
