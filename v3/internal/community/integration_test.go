//go:build pgintegration

package community

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	rows, err := pool.Query(ctx, `SELECT schema_name FROM information_schema.schemata WHERE left(schema_name,3)='v3_'`)
	if err != nil {
		t.Fatal(err)
	}
	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			t.Fatal(err)
		}
		schemas = append(schemas, schema)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, schema := range schemas {
		if _, err := pool.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		sql, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	return pool
}

func TestPublicEligibilityRatingUpsertWeightedOwnerAndRevocation(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,external_id,username,status) VALUES
 (1,'ABC234','owner@example.com','active'),(2,'DEF567','viewer','active'),(3,'GHJ678','other','active'),(4,'KLM789','disabled','disabled');`)
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range []struct {
		id                                               int
		public, slug, visibility, verification, provider string
	}{
		{1, "channel-1", "owner-1", "public", "passed", "openai"},
		{2, "channel-2", "owner-2", "public", "passed", "claude"},
		{3, "private-3", "owner-3", "private", "passed", "openai"},
		{4, "pending-4", "owner-4", "public", "pending", "openai"},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO v3_catalog.channels(id,name,provider,scope,owner_user_id,settings)
 VALUES($1,'display',$2,'marketplace',1,jsonb_build_object('community',jsonb_build_object(
 'id',$3::text,'slug',$4::text,'name','display','visibility',$5::text,'verification_status',$6::text,'lifecycle_status','active')))`,
			channel.id, channel.provider, channel.public, channel.slug, channel.visibility, channel.verification)
		if err != nil {
			t.Fatal(err)
		}
	}
	s := New(pool, Config{ServiceSecret: testServiceSecret})
	for _, use := range []struct{ user, channel int64 }{{2, 1}, {2, 2}, {3, 1}} {
		recordRealUsage(t, pool, use.user, use.channel, "sync", true)
	}
	member, err := s.GetMember(ctx, "ABC234")
	if err != nil || !member.VerifiedChannelOwner || member.Username == "owner@example.com" {
		t.Fatalf("public member %+v %v", member, err)
	}
	disabled, err := s.GetMember(ctx, "KLM789")
	if err != nil || disabled.Active || disabled.Username != "" {
		t.Fatalf("disabled fields %+v %v", disabled, err)
	}
	list, err := s.ListChannels(ctx, "ABC234", ChannelQuery{ViewerSubject: "DEF567"})
	if err != nil || len(list.Items) != 2 || list.Total != 2 {
		t.Fatalf("channels %+v %v", list, err)
	}
	for _, channel := range []string{"private-3", "pending-4"} {
		if _, err := s.RateChannel(ctx, channel, RatingRequest{ViewerSubject: "DEF567", Stars: 5}); !errors.Is(err, ErrChannelNotFound) {
			t.Fatalf("rated private/unverified %s: %v", channel, err)
		}
	}
	if _, err := s.RateChannel(ctx, "channel-1", RatingRequest{ViewerSubject: "ABC234", Stars: 5}); !errors.Is(err, ErrSelfRating) {
		t.Fatalf("self rating %v", err)
	}
	if _, err := s.RateChannel(ctx, "channel-1", RatingRequest{ViewerSubject: "KLM789", Stars: 5}); !errors.Is(err, ErrInactive) {
		t.Fatalf("inactive rating %v", err)
	}
	for _, rating := range []struct {
		channel, viewer string
		stars           int
	}{{"channel-1", "DEF567", 5}, {"channel-1", "DEF567", 4}, {"channel-1", "GHJ678", 2}, {"channel-2", "DEF567", 5}} {
		if _, err := s.RateChannel(ctx, rating.channel, RatingRequest{ViewerSubject: rating.viewer, Stars: rating.stars}); err != nil {
			t.Fatal(err)
		}
	}
	// Concurrent repeats must retain one vote for a (channel, viewer) pair.
	var concurrent sync.WaitGroup
	errorsCh := make(chan error, 12)
	for i := 0; i < 12; i++ {
		concurrent.Add(1)
		go func(stars int) {
			defer concurrent.Done()
			_, err := s.RateChannel(ctx, "channel-1", RatingRequest{ViewerSubject: "DEF567", Stars: stars})
			errorsCh <- err
		}(i%5 + 1)
	}
	concurrent.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RateChannel(ctx, "channel-1", RatingRequest{ViewerSubject: "DEF567", Stars: 4}); err != nil {
		t.Fatal(err)
	}
	member, err = s.GetMember(ctx, "ABC234")
	if err != nil || member.RatingCount != 2 || member.AverageScore != 6.5 {
		t.Fatalf("weighted owner %+v %v", member, err)
	}
	list, err = s.ListChannels(ctx, "ABC234", ChannelQuery{ViewerSubject: "DEF567", Sort: "name"})
	if err != nil || list.Items[0].ViewerStars != 4 {
		t.Fatalf("viewer stars %+v %v", list, err)
	}
	sellers, err := s.ListSellers(ctx, SellerQuery{})
	if err != nil || sellers.Total != 1 || len(sellers.Items) != 1 || sellers.Items[0].ChannelCount != 2 || sellers.Items[0].RatingCount != 2 {
		t.Fatalf("sellers %+v %v", sellers, err)
	}
	filtered, err := s.ListSellers(ctx, SellerQuery{Provider: "openai"})
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].ChannelCount != 1 || filtered.Items[0].AverageScore != 6.5 {
		t.Fatalf("provider filter %+v %v", filtered, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{community,visibility}','"private"') WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RateChannel(ctx, "channel-1", RatingRequest{ViewerSubject: "DEF567", Stars: 1}); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("revocation not enforced %v", err)
	}
}
