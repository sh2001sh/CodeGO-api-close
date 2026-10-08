//go:build pgintegration

package community

import (
	"context"
	"testing"
	"time"
)

func TestMigratedRatingIsVisibleAndUpdatingPreservesLegacyIdentity(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,external_id,username) VALUES(7,'ABC234','owner'),(8,'DEF567','viewer');
 INSERT INTO v3_catalog.channels(id,name,provider,scope,owner_user_id,settings)
 VALUES(13,'legacy','openai','marketplace',7,'{"community":{"id":"legacy-public-13","visibility":"public","verification_status":"passed","lifecycle_status":"active"}}');`)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := 0; i < 2; i++ {
		_, err := pool.Exec(ctx, `INSERT INTO v3_community.channel_ratings
 (channel_id,user_id,stars,legacy_id,created_at,updated_at) VALUES('legacy-public-13',8,5,9007199254740993,$1,$1)
 ON CONFLICT(channel_id,user_id) DO NOTHING`, created)
		if err != nil {
			t.Fatal(err)
		}
	}
	s := New(pool, Config{ServiceSecret: testServiceSecret})
	recordRealUsage(t, pool, 8, 13, "sync", true)
	channels, err := s.ListChannels(ctx, "ABC234", ChannelQuery{ViewerSubject: "DEF567"})
	if err != nil || len(channels.Items) != 1 || channels.Items[0].RatingCount != 1 || channels.Items[0].AverageScore != 10 || channels.Items[0].ViewerStars != 5 {
		t.Fatalf("historical channel rating %+v %v", channels, err)
	}
	owner, err := s.GetMember(ctx, "ABC234")
	if err != nil || owner.RatingCount != 1 || owner.AverageScore != 10 {
		t.Fatalf("historical seller rating %+v %v", owner, err)
	}
	if _, err := s.RateChannel(ctx, "legacy-public-13", RatingRequest{ViewerSubject: "DEF567", Stars: 2}); err != nil {
		t.Fatal(err)
	}
	var id int64
	var stars int
	var at time.Time
	if err := pool.QueryRow(ctx, `SELECT legacy_id,stars,created_at FROM v3_community.channel_ratings
 WHERE channel_id='legacy-public-13' AND user_id=8`).Scan(&id, &stars, &at); err != nil || id != 9007199254740993 || stars != 2 || !at.Equal(created) {
		t.Fatalf("historical identity overwritten %d %d %v %v", id, stars, at, err)
	}
}
