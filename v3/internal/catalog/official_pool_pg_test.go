//go:build pgintegration

package catalog

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestOfficialPoolPostgresCompilePreservesHistoryAndEligibility(t *testing.T) {
	ctx := context.Background()
	db := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, db, enc)
	mustExec(t, db, `INSERT INTO v3_catalog.channels(id,name,provider) VALUES(3,'autodiscovered','openai'),(4,'outside-model','openai'),(5,'outside-group','openai')`)
	mustExec(t, db, `INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(3,'default'),(4,'default')`)
	mustExec(t, db, `INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(3,'gpt-4'),(4,'other'),(5,'gpt-4')`)
	// Multiple retained named pools share a scope; only one live pool is active.
	mustExec(t, db, `INSERT INTO v3_catalog.route_pools(id,name,group_name,model,strategy,enabled,deleted_at,auto_discover,model_scope) VALUES
	(40,'live','default','gpt-4','scored',true,NULL,true,' GPT-4 '),
	(41,'disabled sibling','default','gpt-4','scored',false,NULL,true,'gpt-4'),
	(42,'deleted sibling','default','gpt-4','scored',true,now(),true,'gpt-4')`)
	mustExec(t, db, `INSERT INTO v3_catalog.route_pool_members(pool_id,channel_id,legacy_id,cost_multiplier,model_cost_overrides,fault_domain,enabled,deleted_at) VALUES
	(40,1,501,1.123456789012345678901,'{"gpt-4":0.00000000000000000003}','domain-a',true,NULL),
	(40,2,502,0.25,'{}','domain-b',false,NULL),
	(41,1,503,1,'{}','disabled-history',true,NULL),
	(42,2,504,1,'{}','deleted-history',true,NULL)`)
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	channels, err := loadChannels(ctx, tx, dec)
	if err != nil {
		t.Fatal(err)
	}
	cg, err := loadChannelGroups(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := loadChannelModels(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	pools, err := loadOfficialPools(ctx, tx, channels, cg, cm)
	if err != nil {
		t.Fatal(err)
	}
	p := pools["default"]
	if len(pools) != 1 || p.ID != 40 || p.ModelScope != " GPT-4 " || len(p.Members) != 2 || p.Members[0].ChannelID != 1 || p.Members[1].ChannelID != 3 {
		t.Fatalf("compile lost scope/disable/group/model policy: %+v", pools)
	}
	if p.Members[0].CostMultiplier != "1.123456789012345678901" || p.Members[0].Cost("gpt-4") != "0.00000000000000000003" || p.Members[0].FaultDomain != "domain-a" || p.Members[1].CostMultiplier != "1" {
		t.Fatalf("exact source procurement fields changed: %+v", p.Members)
	}
	plain, err := loadRoutePools(ctx, tx)
	if err != nil || len(plain) != 0 {
		t.Fatalf("scored pool accidentally compiled as random native route: %+v %v", plain, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(ctx, db, dec)
	if err != nil || !reflect.DeepEqual(compiled.OfficialPools, pools) {
		t.Fatalf("actual Compile did not consume official loader: %v", err)
	}
	wire, err := sealSnapshot(compiled, enc)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeWireSnapshot(blob)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := openSnapshot(decoded, dec)
	if err != nil || !reflect.DeepEqual(opened.OfficialPools, pools) {
		t.Fatalf("Redis wire rounded/lost scored source fields: %v", err)
	}
	// Native unnamed upsert remains usable next to all retained history.
	mustExec(t, db, `INSERT INTO v3_catalog.route_pools(group_name,model,strategy) VALUES('default','gpt-4','weighted') ON CONFLICT(group_name,model) WHERE name='' DO UPDATE SET strategy=excluded.strategy`)
	mustExec(t, db, `INSERT INTO v3_catalog.route_pools(group_name,model,strategy) VALUES('default','gpt-4','fill_first') ON CONFLICT(group_name,model) WHERE name='' DO UPDATE SET strategy=excluded.strategy`)
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM v3_catalog.route_pools WHERE group_name='default'`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("named source siblings collapsed/native upsert failed: %d %v", count, err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO v3_catalog.route_pools(name,group_name,model,strategy,enabled) VALUES('second active','default','other','scored',true)`); err == nil {
		t.Fatal("ambiguous second live scored pool accepted")
	}
}
