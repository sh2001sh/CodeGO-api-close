package legacy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// These adapters stage provenance and history only. Resolving an account uses
// IDs reserved by the coordinator; it never posts a balance or opening entry.
func (p *onlineProjector) projectOnlineFunding(name string, raw json.RawMessage) ([]onlineProjection, error) {
	if p.funding == nil {
		return nil, fmt.Errorf("legacy: online funding dependencies are not loaded")
	}
	var row commerceRow
	if err := json.Unmarshal(raw, &row); err != nil || row == nil {
		return nil, fmt.Errorf("legacy: invalid online funding %s row", name)
	}
	if p.funding.retiredFundingRow(name, row) {
		return nil, nil
	}
	projected, err := p.funding.projectFunding(name, row)
	if err != nil {
		return nil, fmt.Errorf("legacy: project online funding %s: %w", name, err)
	}
	if err := resolveFundingAccount(p.mappings, &projected); err != nil {
		return nil, err
	}
	return []onlineProjection{{Schema: "v3_billing", Table: projected.table, Keys: []string{projected.key}, Values: projected.values}}, nil
}

func (p *onlineProjector) projectOnlineMarket(name string, raw json.RawMessage) ([]onlineProjection, error) {
	if p.market == nil {
		return nil, fmt.Errorf("legacy: online marketplace dependencies are not loaded")
	}
	if !cmStreamedTable(name) {
		return nil, fmt.Errorf("legacy: unsupported online marketplace table %s", name)
	}
	var row cmRow
	if err := json.Unmarshal(raw, &row); err != nil || row == nil {
		return nil, fmt.Errorf("legacy: invalid online marketplace %s row", name)
	}
	var record cmRecord
	var err error
	if name == "settlements" {
		record, err = p.market.projectSettlement(row)
	} else {
		record, err = p.market.projectMarketHistory(name, row)
	}
	if err != nil {
		return nil, fmt.Errorf("legacy: project online marketplace %s: %w", name, err)
	}
	for _, key := range record.keys {
		if value := record.values[key]; value == nil || value == "" {
			return nil, fmt.Errorf("legacy: empty online marketplace %s source key %s", name, key)
		}
	}
	return []onlineProjection{{Schema: "v3_channelmarket", Table: strings.TrimPrefix(record.table, "v3_channelmarket."), Keys: record.keys, Values: record.values}}, nil
}

// Dependencies contain only fields that influence already staged projections.
// Updating prices, credentials, user settings or account versions does not
// invalidate historical rows. Existing identities may not be remapped/deleted.
func (p *onlineProjector) onlineDependencyValues() map[string]any {
	values := map[string]any{}
	if p.funding != nil {
		for id, account := range p.funding.accounts {
			values["funding.account:"+id] = []any{account.OwnerType, account.OwnerID, account.Kind, account.Unit}
		}
		for id, user := range p.funding.users {
			values["funding.user:"+strconv.FormatInt(id, 10)] = user.CreatedAt
		}
	}
	if p.market != nil {
		for id, channel := range p.market.channels {
			values["market.channel:"+id] = []any{channel.catalogID, channel.owner}
		}
		for id, group := range p.market.groups {
			values["market.group:"+id] = []any{group.text("channel_id"), cmInt(group, "owner_user_id")}
		}
		for id := range p.market.users {
			values["market.user:"+strconv.FormatInt(id, 10)] = true
		}
	}
	return values
}
