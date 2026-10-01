package catalogcontrol

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func syncMetadataVendor(ctx context.Context, tx pgx.Tx, data *catalog.MetadataSnapshot, name string, upstream map[string]metadataUpstreamVendor, result *metadataSyncResult) (int64, error) {
	if name == "" {
		return 0, nil
	}
	for _, vendor := range data.Vendors {
		if vendor.Name == name {
			return vendor.ID, nil
		}
	}
	value := upstream[name]
	vendor := catalog.VendorMetadata{Name: name, Description: value.Description, Icon: value.Icon, Status: metadataSyncStatus(value.Status, 1)}
	err := tx.QueryRow(ctx, `INSERT INTO v3_catalog.vendors(name,description,icon,status)
		VALUES($1,$2,$3,$4) RETURNING id`, vendor.Name, vendor.Description, vendor.Icon, vendor.Status).Scan(&vendor.ID)
	if err != nil {
		return 0, err
	}
	data.Vendors = append(data.Vendors, vendor)
	result.CreatedVendors++
	return vendor.ID, nil
}

func metadataVendorID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func insertMetadataModel(ctx context.Context, tx pgx.Tx, item *catalog.ModelMetadata) error {
	return tx.QueryRow(ctx, `INSERT INTO v3_catalog.models(model_name,description,icon,tags,vendor_id,status,sync_official,name_rule)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, item.ModelName, item.Description, item.Icon, item.Tags,
		metadataVendorID(item.VendorID), item.Status, item.SyncOfficial, item.NameRule).Scan(&item.ID)
}

func overwriteMetadataModel(ctx context.Context, tx pgx.Tx, item *catalog.ModelMetadata, fields []string, upstream metadataUpstreamModel, data *catalog.MetadataSnapshot, vendors map[string]metadataUpstreamVendor, result *metadataSyncResult) error {
	for _, field := range fields {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "description":
			item.Description = upstream.Description
		case "icon":
			item.Icon = upstream.Icon
		case "tags":
			item.Tags = upstream.Tags
		case "name_rule":
			item.NameRule = upstream.NameRule
		case "status":
			item.Status = metadataSyncStatus(upstream.Status, item.Status)
		case "vendor":
			id, err := syncMetadataVendor(ctx, tx, data, upstream.VendorName, vendors, result)
			if err != nil {
				return err
			}
			item.VendorID = id
		}
	}
	var id int64
	return tx.QueryRow(ctx, `UPDATE v3_catalog.models SET description=$2,icon=$3,tags=$4,vendor_id=$5,status=$6,name_rule=$7
		WHERE id=$1 AND deleted_at IS NULL AND sync_official<>0 RETURNING id`, item.ID, item.Description, item.Icon,
		item.Tags, metadataVendorID(item.VendorID), item.Status, item.NameRule).Scan(&id)
}
