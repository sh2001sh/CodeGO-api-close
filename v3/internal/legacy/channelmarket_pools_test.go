package legacy

import (
	"encoding/json"
	"testing"
)

func TestMarketMissingOfficialPoolMembersKeepConfiguration(t *testing.T) {
	d := cmUnitData(t)
	d.rows["route_pools"] = []cmRow{cmTestRow(t, `{"id":"named","owner_user_id":8,"name":"Original","strategy":"score","max_attempts":4,"failure_cooldown_seconds":50,"max_multiplier":0.5,"auto_build_enabled":true,"auto_build_size":7}`)}
	d.rows["auto_route_pool_configs"] = []cmRow{cmTestRow(t, `{"owner_user_id":8,"strategy":"weighted","max_attempts":2,"failure_cooldown_seconds":20,"multiplier_weight":35}`)}
	d.rows["route_pool_members"] = []cmRow{
		cmTestRow(t, `{"id":41,"pool_id":"named","group_id":"official:retired","priority":-9}`),
		cmTestRow(t, `{"id":42,"pool_id":"named","group_id":"official:default","priority":3}`),
	}
	d.rows["auto_route_pool_members"] = []cmRow{cmTestRow(t, `{"id":51,"owner_user_id":8,"group_id":"official:retired","priority":11}`)}
	d.prepare("")
	if len(d.issues) != 0 {
		t.Fatalf("missing official catalog group blocked retained configuration: %+v", d.issues)
	}
	members := 0
	for _, r := range d.records {
		switch r.table {
		case "v3_channelmarket.route_pool_members":
			members++
			var wantID, wantPriority int64
			var wantSource, wantPool string
			var wantCatalog any
			switch r.values["legacy_id"] {
			case int64(41):
				wantID, wantPriority, wantSource, wantPool = 41, -9, "route_pool_members", "named"
			case int64(42):
				wantID, wantPriority, wantSource, wantPool, wantCatalog = 42, 3, "route_pool_members", "named", "default"
			case int64(51):
				wantID, wantPriority, wantSource, wantPool = 51, 11, "auto_route_pool_members", cmAutoID(8)
			default:
				t.Fatalf("invented member: %+v", r.values)
			}
			if r.values["legacy_id"] != wantID || r.values["priority"] != wantPriority || r.values["legacy_source"] != wantSource || r.values["pool_id"] != wantPool || r.values["catalog_group_name"] != wantCatalog {
				t.Fatalf("member identity or routing state changed: %+v", r.values)
			}
			if wantCatalog == nil && r.values["group_id"] != "official:retired" {
				t.Fatalf("original official configuration was lost: %+v", r.values)
			}
		case "v3_channelmarket.route_pools":
			config := r.values["config"].(map[string]any)
			if r.values["id"] == "named" {
				build := config["auto_build"].(map[string]any)
				if r.values["strategy"] != "score" || r.values["max_attempts"] != int64(4) || string(build["size"].(json.RawMessage)) != "7" {
					t.Fatalf("named pool configuration changed: %+v", r.values)
				}
			} else if r.values["strategy"] != "weighted" || string(config["multiplier_weight"].(json.RawMessage)) != "35" {
				t.Fatalf("auto pool configuration changed: %+v", r.values)
			}
		}
	}
	if members != 3 || len(d.internal) != 1 {
		t.Fatal("member omitted or a source gateway channel was invented")
	}
}

func TestMarketMissingOfficialPoolMembersStillRejectInvalidConfiguration(t *testing.T) {
	for _, member := range []string{
		`{"id":1,"pool_id":"missing","group_id":"official:retired","priority":1}`,
		`{"id":1,"pool_id":"named","group_id":"missing-market-group","priority":1}`,
		`{"id":1,"pool_id":"named","group_id":"official:","priority":1}`,
		`{"id":1,"pool_id":"named","group_id":"official: ","priority":1}`,
		`{"id":1,"pool_id":"named","group_id":"official:retired","priority":2147483648}`,
		`{"id":1,"pool_id":"named","group_id":"official:retired","priority":"invalid"}`,
		`{"id":"invalid","pool_id":"named","group_id":"official:retired","priority":1}`,
	} {
		d := cmUnitData(t)
		d.rows["route_pools"] = []cmRow{cmTestRow(t, `{"id":"named","owner_user_id":8,"name":"Original"}`)}
		d.rows["route_pool_members"] = []cmRow{cmTestRow(t, member)}
		d.prepare("")
		if len(d.issues) == 0 {
			t.Fatalf("invalid pool member accepted: %s", member)
		}
	}
	for _, config := range []string{
		`"strategy":"invalid"`, `"max_attempts":33`, `"failure_cooldown_seconds":-1`, `"owner_user_id":999`,
	} {
		d := cmUnitData(t)
		d.rows["route_pools"] = []cmRow{cmTestRow(t, `{"id":"named","owner_user_id":8,"name":"Original",`+config+`}`)}
		d.rows["route_pool_members"] = []cmRow{cmTestRow(t, `{"id":1,"pool_id":"named","group_id":"official:retired","priority":1}`)}
		d.prepare("")
		if len(d.issues) == 0 {
			t.Fatalf("invalid parent configuration accepted: %s", config)
		}
	}
}
