package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type MetadataQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// ReadMetadata must be called inside the same read transaction as the catalog
// compiler. Deleted records remain persisted for history but are not published.
func ReadMetadata(ctx context.Context, q MetadataQuerier) (MetadataSnapshot, error) {
	result := MetadataSnapshot{
		Vendors: []VendorMetadata{}, Models: []ModelMetadata{}, PrefillGroups: []PrefillGroup{},
	}
	rows, err := q.Query(ctx, `SELECT id,name,description,icon,status,created_at,updated_at
		FROM v3_catalog.vendors WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return result, fmt.Errorf("catalog: vendors: %w", err)
	}
	vendors, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (VendorMetadata, error) {
		var value VendorMetadata
		var created, updated time.Time
		err := row.Scan(&value.ID, &value.Name, &value.Description, &value.Icon, &value.Status, &created, &updated)
		value.CreatedTime, value.UpdatedTime = created.Unix(), updated.Unix()
		return value, err
	})
	if err != nil {
		return result, fmt.Errorf("catalog: scan vendors: %w", err)
	}
	result.Vendors = vendors
	rows, err = q.Query(ctx, `SELECT id,model_name,description,icon,tags,endpoints,
		COALESCE(vendor_id,0),status,sync_official,name_rule,created_at,updated_at
		FROM v3_catalog.models WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return result, fmt.Errorf("catalog: models: %w", err)
	}
	models, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ModelMetadata, error) {
		var value ModelMetadata
		var created, updated time.Time
		err := row.Scan(&value.ID, &value.ModelName, &value.Description, &value.Icon, &value.Tags,
			&value.Endpoints, &value.VendorID, &value.Status, &value.SyncOfficial, &value.NameRule, &created, &updated)
		value.CreatedTime, value.UpdatedTime = created.Unix(), updated.Unix()
		return value, err
	})
	if err != nil {
		return result, fmt.Errorf("catalog: scan models: %w", err)
	}
	result.Models = models
	rows, err = q.Query(ctx, `SELECT id,name,type,description,items,created_at,updated_at
		FROM v3_catalog.prefill_groups WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return result, fmt.Errorf("catalog: prefill groups: %w", err)
	}
	prefills, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PrefillGroup, error) {
		var value PrefillGroup
		var created, updated time.Time
		err := row.Scan(&value.ID, &value.Name, &value.Type, &value.Description, &value.Items, &created, &updated)
		value.CreatedTime, value.UpdatedTime = created.Unix(), updated.Unix()
		return value, err
	})
	if err != nil {
		return result, fmt.Errorf("catalog: scan prefill groups: %w", err)
	}
	result.PrefillGroups = prefills
	return result, nil
}
