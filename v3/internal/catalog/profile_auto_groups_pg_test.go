//go:build pgintegration

package catalog

import (
	"context"
	"reflect"
	"testing"
)

func TestCompiledAutoGroupsMatchesIdentityPolicy(t *testing.T) {
	pool := testPool(t)
	_, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	mustExec(t, pool, `INSERT INTO v3_identity.users(id,username) VALUES(1,'owner'),(2,'member'),(3,'blocked')`)
	mustExec(t, pool, `INSERT INTO v3_catalog.groups(name) VALUES('market'),('pool')`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.groups(id,public_channel_id,channel_id,owner_user_id,public_slug,internal_group_name,display_name,visibility,lifecycle_status,multiplier_ppm)
	 VALUES('m1','public1',1,1,'slug','market','market','private','active',1000000)`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.group_access(group_id,user_id) VALUES('m1',2),('m1',3)`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.channel_user_blocks(channel_id,user_id) VALUES(1,3)`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.route_pools(id,owner_user_id,name,internal_group_name,max_attempts,max_multiplier_ppm,failure_cooldown_seconds,strategy)
	 VALUES('p1',2,'pool','pool',2,1000000,45,'score')`)

	ctx := context.Background()
	for _, raw := range []string{`["pool","market","default","missing","default"]`, `[]`, `["market"]`} {
		mustExec(t, pool, `INSERT INTO v3_platform.settings(key,value) VALUES('AutoGroups',$1::jsonb)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value,sensitive=false`, raw)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		profiles, err := loadBaseAccountProfiles(ctx, tx)
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		for id, profile := range profiles {
			var allowed, automatic []string
			if err := tx.QueryRow(ctx, `SELECT ARRAY(SELECT v3_identity.allowed_groups($1)),ARRAY(SELECT v3_identity.auto_groups($1))`, id).Scan(&allowed, &automatic); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(err)
			}
			if !reflect.DeepEqual(profile.AllowedGroups, allowed) || !reflect.DeepEqual(profile.AutoGroups, automatic) {
				_ = tx.Rollback(ctx)
				t.Fatalf("user %d configured %s: compiled allowed=%v auto=%v, SQL allowed=%v auto=%v", id, raw, profile.AllowedGroups, profile.AutoGroups, allowed, automatic)
			}
		}
		_ = tx.Rollback(ctx)
	}
}
