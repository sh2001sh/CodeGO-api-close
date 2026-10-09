//go:build pgintegration

package catalog

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
)

func TestCompileMarketExactOverrideAndDeletedHistory(t *testing.T) {
	pool := testPool(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	mustExec(t, pool, `INSERT INTO v3_identity.users(id,username) VALUES(1,'exact-owner'),(2,'exact-consumer')`)
	mustExec(t, pool, `INSERT INTO v3_catalog.groups(name) VALUES('exact-market'),('exact-deleted')`)
	mustExec(t, pool, `UPDATE v3_catalog.channels SET scope='marketplace',owner_user_id=1 WHERE id IN (1,2)`)
	mustExec(t, pool, `INSERT INTO v3_channelmarket.groups(id,public_channel_id,channel_id,owner_user_id,public_slug,internal_group_name,display_name,visibility,lifecycle_status,multiplier_ppm,deleted_at)
	 VALUES('exact-active','exact-public',1,1,'exact-slug','exact-market','exact','public','active',0,NULL),
	       ('exact-deleted','exact-history',2,1,'exact-history-slug','exact-deleted','history','public','paused',0,now())`)
	// An inactive historical channel's fractional override must not be scanned
	// into int64 before the snapshot can decide whether to use that channel.
	mustExec(t, pool, `INSERT INTO v3_channelmarket.user_multipliers(channel_id,user_id,multiplier_ppm)
	 VALUES(1,2,0.00000000000001),(2,2,0.000000000000000000000000000000000000000000000000000000001)`)
	snap, err := Compile(context.Background(), pool, dec)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := snap.Market.Groups["exact-deleted"]; exists {
		t.Fatal("deleted history was published as a routable market group")
	}
	if _, exists := snap.Market.Channels[2]; exists {
		t.Fatal("deleted group's override revived its channel policy")
	}
	policy := snap.Market.Channels[1]
	if cmp, err := exactfactor.Compare(policy.FactorExact(2, time.Now()), "0.00000000000001"); err != nil || cmp != 0 {
		t.Fatalf("PG numeric fractional override lost: %s %v", policy.FactorExact(2, time.Now()), err)
	}
	if got := policy.FactorExact(1, time.Now()); got != "0" {
		t.Fatalf("zero group was rewritten: %s", got)
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
	if cmp, err := exactfactor.Compare(loaded.Market.Channels[1].FactorExact(2, time.Now()), "0.00000000000001"); err != nil || cmp != 0 {
		t.Fatal("exact override changed in the published gateway snapshot")
	}
}
