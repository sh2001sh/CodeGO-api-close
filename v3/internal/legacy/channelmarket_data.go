package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type cmChannel struct {
	publicID         string
	catalogID        int64
	owner            int64
	row              cmRow
	url              string
	credential       string
	credentialKind   string
	credentialExpiry *time.Time
	models           []string
	settings         json.RawMessage
	group            cmRow
	newCatalog       bool
}
type channelMarketData struct {
	rows            map[string][]cmRow
	channels        map[string]*cmChannel
	groups          map[string]cmRow
	users           map[int64]bool
	internal        map[int64]cmRow
	records         []cmRecord
	pending         map[int64]int64
	platformRevenue *int64
	issues          []Issue
	keyBindings     []cmKeyBinding
}

type cmKeyBinding struct {
	ID, UserID int64
	Group      string
}

func loadChannelMarket(ctx context.Context, source pgx.Tx, sources map[string]string) (*channelMarketData, error) {
	d := &channelMarketData{rows: map[string][]cmRow{}, channels: map[string]*cmChannel{}, groups: map[string]cmRow{}, users: map[int64]bool{}, internal: map[int64]cmRow{}, pending: map[int64]int64{}}
	for _, table := range channelMarketSourceTables {
		rows, err := loadRows(ctx, source, sources["marketplace_"+table])
		if err != nil {
			return nil, err
		}
		for _, raw := range rows {
			var row cmRow
			if err = json.Unmarshal(raw, &row); err != nil {
				return nil, fmt.Errorf("legacy: decode marketplace %s: %w", table, err)
			}
			d.rows[table] = append(d.rows[table], row)
		}
	}
	for _, table := range []string{"users", "channels", "community_channel_ratings"} {
		rows, err := loadRows(ctx, source, sources[table])
		if err != nil {
			return nil, err
		}
		for _, raw := range rows {
			var row cmRow
			if err = json.Unmarshal(raw, &row); err != nil {
				return nil, err
			}
			switch table {
			case "users":
				id, e := row.integer("id")
				if e != nil {
					return nil, e
				}
				d.users[id] = true
			case "channels":
				id, e := row.integer("id")
				if e != nil {
					return nil, e
				}
				d.internal[id] = row
			default:
				d.rows[table] = append(d.rows[table], row)
			}
		}
	}
	keyRows, err := loadRows(ctx, source, sources["tokens"])
	if err != nil {
		return nil, err
	}
	for _, raw := range keyRows {
		var row cmRow
		if err = json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		id, e := row.integer("id")
		if e != nil {
			return nil, e
		}
		owner, e := row.integer("user_id")
		if e != nil {
			return nil, e
		}
		d.keyBindings = append(d.keyBindings, cmKeyBinding{ID: id, UserID: owner, Group: row.text("group")})
	}
	d.prepare(os.Getenv("V3_MIGRATION_SOURCE_CRYPTO_SECRET"))
	if err := d.loadAccountBalances(ctx, source, sources); err != nil {
		return nil, err
	}
	return d, nil
}

// The native API key stores its concrete internal group. Core key verification
// must use this projection rather than compare the obsolete market: binding.
func (d *channelMarketData) targetKeyGroup(group string, user int64) string {
	if group == "market:auto" {
		return "pool_" + cmAutoID(user)
	}
	if strings.HasPrefix(group, "market:pool:") {
		id := strings.TrimPrefix(group, "market:pool:")
		for _, record := range d.records {
			if record.table == "v3_channelmarket.route_pools" && record.values["id"] == id {
				return record.values["internal_group_name"].(string)
			}
		}
		return group
	}
	if strings.HasPrefix(group, "market:") {
		if row := d.groups[strings.TrimPrefix(group, "market:")]; row != nil {
			return row.text("internal_group_name")
		}
	}
	return group
}

func cmUnix(value int64) time.Time { return time.Unix(value, 0).UTC() }
func cmAutoID(owner int64) string  { return "auto:" + strconv.FormatInt(owner, 10) }
