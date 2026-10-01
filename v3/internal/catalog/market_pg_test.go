//go:build pgintegration

package catalog

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCompileMarketPermissionsAndWire(t *testing.T) {
	pool := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	mustExec(t, pool, `INSERT INTO v3_identity.users(id,username) VALUES(1,'owner'),(2,'consumer')`)
	mustExec(t, pool, `INSERT INTO v3_identity.api_keys(id,user_id,key_hash,key_prefix,key_ciphertext,budget_limited) VALUES(10,2,decode(repeat('11',32),'hex'),'fixture',decode('11','hex'),true)`)
	mustExec(t, pool, `INSERT INTO v3_billing.accounts(id,owner_type,owner_id,kind) OVERRIDING SYSTEM VALUE VALUES(20,'api_key',10,'key_budget')`)
	mustExec(t, pool, `INSERT INTO v3_catalog.groups(name) VALUES('market'),('pool')`)
	mustExec(t, pool, `UPDATE v3_catalog.channels SET scope='marketplace',owner_user_id=1,multiplier_card_supported=true,status_code_mapping='{"400":"503"}',settings='{"reasoning":"high","multiplier_card_user_enabled":false}',param_override='{"temperature":0}',header_override='{"X-Override":"yes"}' WHERE id=1`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.groups(id,public_channel_id,channel_id,owner_user_id,public_slug,internal_group_name,display_name,visibility,lifecycle_status,multiplier_ppm) VALUES('m1','public1',1,1,'slug','market','market','private','active',1500000)`)
	mustExec(t, pool, `UPDATE v3_channelmarket.groups SET model_prices='{"gpt-4":{"billing_mode":"per_call","price_per_call":0.000002,"money_quantum":2}}' WHERE id='m1'`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.group_access(group_id,user_id) VALUES('m1',2)`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.channel_user_blocks(channel_id,user_id) VALUES(1,2)`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.user_multipliers(channel_id,user_id,multiplier_ppm) VALUES(1,2,750000)`)
	start := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.time_range_multipliers(id,channel_id,starts_at,ends_at,multiplier_ppm) VALUES('win',1,$1,$2,500000)`, start, start.Add(2*time.Hour))
	mustExec(t, pool, `INSERT INTO v3_channelmarket.route_pools(id,owner_user_id,name,internal_group_name,max_attempts,max_multiplier_ppm,strategy) VALUES('p1',2,'pool','pool',2,1000000,'score')`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.route_pool_members(pool_id,group_id) VALUES('p1','m1')`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.ranking_snapshots(id,group_id,window_hours,ranking_version,rank,score,request_count,observing,calculated_at)
	 VALUES('old','m1',24,'marketplace-v7-consumer-cost',1,95,1,true,$1),('latest','m1',24,'v3-usage',1,81,100,false,$2)`, start, start.Add(time.Minute))
	snap, err := Compile(context.Background(), pool, dec)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := sealSnapshot(snap, enc)
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
	loaded, err := openSnapshot(decoded, dec)
	if err != nil {
		t.Fatal(err)
	}
	policy, group, poolPolicy := loaded.Market.Channels[1], loaded.Market.Groups["market"], loaded.Market.Pools["pool"]
	if !group.Allows(1) || !group.Allows(2) || group.Allows(3) || !policy.Blocked[2] || policy.Factor(2, time.Now()) != 750000 || policy.Factor(3, time.Now()) != 500000 || poolPolicy.MaxAttempts != 2 || len(poolPolicy.GroupIDs) != 1 || poolPolicy.MaxMultiplierPPM != 1000000 {
		t.Fatal("market permission/multiplier projection lost during compile/wire")
	}
	if policy.CreditPolicy != "marketplace_subscription_and_universal" || !group.HasScore || group.Score != 81 || poolPolicy.Strategy != "score" || len(poolPolicy.Members) != 1 || poolPolicy.Members[0].CatalogGroupName != "market" {
		t.Fatal("credit policy/current ranking/pool strategy/member identity was lost in compile/wire")
	}
	if price := policy.ModelPrices["gpt-4"]; price.PerRequest != 2 || price.Rules["money_quantum"] != json.Number("2") {
		t.Fatal("source market money quantum was lost in actual PG compile/wire")
	}
	channel := loaded.Channels[1]
	if channel.StatusCodeMapping["400"] != 503 || channel.Settings["reasoning"] != "high" || channel.ParamOverride["temperature"] != json.Number("0") || channel.HeaderOverride["X-Override"] != "yes" || len(channel.Groups) != 1 {
		t.Fatal("channel config lost during compile/wire")
	}
	if !channel.MultiplierCardSupported || channel.MultiplierCardUserEnabled {
		t.Fatal("disabled card activation flag lost during compile/wire")
	}
	if p := loaded.AccountProfiles[2]; p.KeyBudgetAccounts[10] != 20 || len(p.AllowedGroups) == 0 || len(p.AutoGroups) == 0 {
		t.Fatal("account authorization/key-budget profile lost")
	}
}
