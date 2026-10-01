package legacy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var catalogDataSourceNames = []string{"vendors", "models", "prefill_groups", "route_pools", "route_pool_members"}

type catalogData struct {
	rows     map[string][]commerceRow
	channels map[int64]commerceRow
}

type catalogDataRecord struct {
	table, key string
	id         int64
	fields     map[string]any
}

func loadCatalogData(ctx context.Context, source pgx.Tx, sources map[string]string) (*catalogData, error) {
	d := &catalogData{rows: map[string][]commerceRow{}, channels: map[int64]commerceRow{}}
	for _, name := range append(append([]string{}, catalogDataSourceNames...), "channels") {
		rows, err := loadRows(ctx, source, sources[name])
		if err != nil {
			return nil, fmt.Errorf("legacy: load catalog %s: %w", name, err)
		}
		for _, raw := range rows {
			var row commerceRow
			if err = json.Unmarshal(raw, &row); err != nil {
				return nil, err
			}
			if name == "channels" {
				id, err := row.integer("id")
				if err != nil {
					return nil, err
				}
				d.channels[id] = row
			} else {
				d.rows[name] = append(d.rows[name], row)
			}
		}
	}
	return d, nil
}

func (d *catalogData) validate(report *Report) {
	if report.Counts == nil {
		report.Counts = map[string]int64{}
	}
	ids := map[string]map[int64]bool{}
	for _, name := range catalogDataSourceNames {
		ids[name] = map[int64]bool{}
		for _, row := range d.rows[name] {
			id, _ := row.integer("id")
			if ids[name][id] {
				report.Issues = append(report.Issues, Issue{name, id, "duplicate_catalog_id", "source catalog contains duplicate IDs"})
			}
			ids[name][id] = true
		}
	}
	activeKeys, memberKeys, activeGroups := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, name := range catalogDataSourceNames {
		report.Counts[name] = int64(len(d.rows[name]))
		for _, row := range d.rows[name] {
			id, _ := row.integer("id")
			record, err := d.project(name, row)
			if err != nil {
				report.Issues = append(report.Issues, Issue{name, id, "invalid_catalog_row", err.Error()})
				continue
			}
			fields := record.fields
			if vendor, ok := fields["vendor_id"].(int64); ok && !ids["vendors"][vendor] {
				report.Issues = append(report.Issues, Issue{name, id, "catalog_vendor_missing", "model references an absent source vendor"})
			}
			if name == "route_pool_members" {
				pool, channel := fields["pool_id"].(int64), fields["channel_id"].(int64)
				if !ids["route_pools"][pool] || d.channels[channel] == nil {
					report.Issues = append(report.Issues, Issue{name, id, "catalog_pool_reference_missing", "pool member references an absent pool or channel"})
				}
				key := fmt.Sprintf("%d/%d", pool, channel)
				if memberKeys[key] {
					report.Issues = append(report.Issues, Issue{name, id, "duplicate_catalog_member", "duplicate pool/channel membership"})
				}
				memberKeys[key] = true
				continue
			}
			if fields["deleted_at"] == nil {
				field := "name"
				if name == "models" {
					field = "model_name"
				}
				key := name + ":" + fields[field].(string)
				if activeKeys[key] {
					report.Issues = append(report.Issues, Issue{name, id, "duplicate_catalog_name", "duplicate live catalog name"})
				}
				activeKeys[key] = true
				if name == "route_pools" && fields["enabled"] == true {
					group := fields["group_name"].(string)
					if activeGroups[group] {
						report.Issues = append(report.Issues, Issue{name, id, "ambiguous_enabled_pool", "multiple enabled official pools in one source group"})
					}
					activeGroups[group] = true
				}
			}
		}
	}
}
