//go:build pgintegration

package catalog

import (
	"context"
	"testing"
)

func TestPublishedSnapshotsHaveBoundedRetentionAcrossVersionGaps(t *testing.T) {
	pool := testPool(t)
	rdb := testRedis(t)
	dec, enc := testDecrypter(t)
	seedCatalog(t, pool, enc)
	publisher := NewPublisher(pool, rdb, dec, enc, nil)
	ctx := context.Background()
	var versions []int64
	for round := 0; round < 8; round++ {
		if round == 4 {
			// A failed publication can bump PostgreSQL before reaching Redis.
			mustExec(t, pool, `UPDATE v3_platform.snapshot_versions SET version=version+2 WHERE kind='catalog'`)
		}
		mustExec(t, pool, `UPDATE v3_catalog.channels SET priority=$1 WHERE id=1`, round)
		if err := publisher.PublishNow(ctx); err != nil {
			t.Fatal(err)
		}
		version, err := latestVersion(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		versions = append(versions, version)
	}
	if count, err := rdb.ZCard(ctx, snapshotVersionsKey).Result(); err != nil || count != retainedSnapshots {
		t.Fatalf("retained snapshots=%d want=%d err=%v", count, retainedSnapshots, err)
	}
	for i, version := range versions {
		exists, err := rdb.Exists(ctx, snapshotKey(version)).Result()
		want := int64(0)
		if i >= len(versions)-int(retainedSnapshots) {
			want = 1
		}
		if err != nil || exists != want {
			t.Fatalf("snapshot %d exists=%d want=%d err=%v", version, exists, want, err)
		}
		if want == 1 {
			ttl, err := rdb.TTL(ctx, snapshotKey(version)).Result()
			if err != nil || (i == len(versions)-1 && ttl >= 0) || (i < len(versions)-1 && (ttl <= 0 || ttl > snapshotTTL)) {
				t.Fatalf("snapshot %d ttl=%v err=%v", version, ttl, err)
			}
		}
	}
	store := NewStore(pool, rdb, dec, nil)
	if err := store.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if store.Version() != versions[len(versions)-1] || store.Current().Channels[1].Priority != 7 {
		t.Fatalf("latest snapshot lost: version=%d channel=%+v", store.Version(), store.Current().Channels[1])
	}
}
