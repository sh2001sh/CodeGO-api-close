//go:build pgintegration

package catalogcontrol

import (
	"context"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestMetadataImportedIdentitiesAndConstraintsPostgres(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	created := time.Date(2020, 2, 3, 4, 5, 6, 0, time.UTC)
	metadataExec(t, pool, `INSERT INTO v3_catalog.vendors(id,name,created_at,updated_at) VALUES(42,'imported-vendor',$1,$1)`, created)
	metadataExec(t, pool, `INSERT INTO v3_catalog.models(id,model_name,vendor_id,name_rule,created_at,updated_at)
		VALUES(84,'-large',42,3,$1,$1),(85,'any',NULL,2,$1,$1)`, created)
	metadataExec(t, pool, `INSERT INTO v3_catalog.prefill_groups(id,name,type,items,created_at,updated_at)
		VALUES(96,'source-prefill','model','["preserved"]',$1,$1)`, created)
	metadataExec(t, pool, `INSERT INTO v3_catalog.models(id,model_name,deleted_at) VALUES(86,'removed',now())`)
	for _, sql := range []string{
		`INSERT INTO v3_catalog.models(model_name,name_rule) VALUES('bad-rule',4)`,
		`INSERT INTO v3_catalog.models(model_name,vendor_id) VALUES('bad-vendor',404)`,
		`INSERT INTO v3_catalog.models(model_name) VALUES(' ')`,
		`INSERT INTO v3_catalog.prefill_groups(name,type,items) VALUES('bad','model','[1]')`,
		`INSERT INTO v3_catalog.prefill_groups(name,type,items) VALUES('bad','tag','[""]')`,
		`INSERT INTO v3_catalog.prefill_groups(name,type,items) VALUES('bad','endpoint','true')`,
		`INSERT INTO v3_catalog.prefill_groups(name,type,items) VALUES('bad','retired-pet','[]')`,
	} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Fatalf("invalid metadata persisted: %s", sql)
		}
	}
	data, err := catalog.ReadMetadata(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	model, vendor := data.Describe("native-large")
	if model == nil || model.ID != 84 || vendor == nil || vendor.ID != 42 || model.CreatedTime != created.Unix() || vendor.CreatedTime != created.Unix() || len(data.PrefillGroups) != 1 || data.PrefillGroups[0].ID != 96 || data.PrefillGroups[0].CreatedTime != created.Unix() {
		t.Fatalf("source IDs/time/rules lost: %+v", data)
	}
	metadataExec(t, pool, `UPDATE v3_catalog.models SET deleted_at=now() WHERE id=84`)
	metadataExec(t, pool, `INSERT INTO v3_catalog.models(id,model_name,name_rule) VALUES(87,'-large',3)`)
	data, err = catalog.ReadMetadata(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	model, vendor = data.Describe("native-large")
	if model == nil || model.ID != 87 || vendor != nil {
		t.Fatal("deleted row selected over replacement model metadata")
	}
}
