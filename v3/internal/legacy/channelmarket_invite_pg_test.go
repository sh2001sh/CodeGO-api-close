//go:build pgintegration

package legacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestChannelMarketMigratedInviteAcceptsOriginalLegacyToken(t *testing.T) {
	for _, encoding := range []string{"hex", "raw_base64url"} {
		t.Run(encoding, func(t *testing.T) {
			source, target, crypto := marketMigrationDBs(t)
			ctx := context.Background()
			sources := seedChannelMarketFixture(t, source)
			onlineRecoveryExec(t, source, `CREATE TABLE public.cm_users(id bigint);INSERT INTO public.cm_users VALUES(7),(8),(9);
 CREATE TABLE public.cm_channels(id bigint,"group" text);INSERT INTO public.cm_channels VALUES(13,'default')`)
			sources["users"], sources["channels"] = "public.cm_users", "public.cm_channels"
			onlineRecoveryExec(t, target, `INSERT INTO v3_identity.users(id,username,role,status) VALUES(7,'owner','user','active'),(8,'consumer','user','active'),(9,'invite-consumer','user','active');
 INSERT INTO v3_catalog.groups(name) VALUES('default');
 INSERT INTO v3_catalog.channels(id,name,provider,base_url) VALUES(13,'core','openai','https://example.invalid');
 INSERT INTO v3_catalog.channel_groups(channel_id,group_name) VALUES(13,'default');
 INSERT INTO v3_catalog.channel_models(channel_id,model) VALUES(13,'chat-model')`)
			// Reproduce V2's exact token and persisted digest encoding.
			token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0xfb}, 32))
			digest := sha256.Sum256([]byte(token))
			stored := hex.EncodeToString(digest[:])
			if encoding == "raw_base64url" {
				stored = base64.RawURLEncoding.EncodeToString(digest[:])
			}
			onlineRecoveryExec(t, source, "UPDATE marketplace.group_invites SET token_hash=$1 WHERE id=91", stored)
			read, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = read.Rollback(ctx) }()
			data, err := loadChannelMarket(ctx, read, sources)
			if err != nil || len(data.issues) != 0 {
				t.Fatalf("legacy invite blocked marketplace dependencies: data=%+v err=%v", data, err)
			}
			importer := NewImporter(source, target, crypto)
			for i := 0; i < 2; i++ {
				write, err := target.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := importer.importChannelMarket(ctx, write, data); err != nil {
					_ = write.Rollback(ctx)
					t.Fatal(err)
				}
				if err := write.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var migrated []byte
			if err := target.QueryRow(ctx, "SELECT token_hash FROM v3_channelmarket.group_invites WHERE id=91").Scan(&migrated); err != nil || !bytes.Equal(migrated, digest[:]) {
				t.Fatalf("migration changed original digest bytes: %x err=%v", migrated, err)
			}
			service := channelmarket.New(target, crypto, nil, channelmarket.Config{}, nil)
			if _, err := service.AcceptInvite(ctx, 9, token+"changed"); !errors.Is(err, channelmarket.ErrNotFound) {
				t.Fatalf("wrong original token was accepted: %v", err)
			}
			var access int
			if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_channelmarket.group_access WHERE user_id=9").Scan(&access); err != nil || access != 0 {
				t.Fatalf("rejected token granted access: count=%d err=%v", access, err)
			}
			for i := 0; i < 2; i++ {
				group, err := service.AcceptInvite(ctx, 9, token)
				if err != nil || group != "legacy-group-201" {
					t.Fatalf("original V2 token could not accept migrated invite: group=%s err=%v", group, err)
				}
			}
			if err := target.QueryRow(ctx, "SELECT count(*) FROM v3_channelmarket.group_access WHERE user_id=9 AND group_id='legacy-group-201' AND invite_id=91").Scan(&access); err != nil || access != 1 {
				t.Fatalf("original token did not grant exact invite access: count=%d err=%v", access, err)
			}
			var unchanged string
			if err := source.QueryRow(ctx, "SELECT token_hash FROM marketplace.group_invites WHERE id=91").Scan(&unchanged); err != nil || unchanged != stored {
				t.Fatalf("migration altered source invite: err=%v", err)
			}
		})
	}
}
