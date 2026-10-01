//go:build pgintegration

package legacy

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway/live"
)

func seedSourceBackground(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := source.Exec(ctx, `CREATE SCHEMA gateway;
		CREATE TABLE gateway.responses_background_jobs(id text PRIMARY KEY,user_id bigint,token_id bigint,model text,status text,stream bool,native_background bool,
		channel_id bigint,key_index int,routing_context_ciphertext text,final_response_ciphertext text,error_ciphertext text,upstream_response_id text,
		upstream_sequence bigint,last_sequence bigint,cancel_requested bool,created_at timestamptz,updated_at timestamptz,request_ciphertext text,authorization_ciphertext text);
		CREATE TABLE gateway.responses_background_events(id bigint PRIMARY KEY,job_id text,sequence bigint,type text,payload_ciphertext text,created_at timestamptz);`)
	if err != nil {
		t.Fatal(err)
	}
	for i, status := range []string{"completed", "failed", "cancelled"} {
		job, events := sourceBackgroundFixture(t, status)
		_, err = source.Exec(ctx, `INSERT INTO gateway.responses_background_jobs VALUES($1,$2,$3,$4,$5,$6,$7,$8,99,$9,$10,$11,$12,$13,$14,$15,$16,$17,'enc:v1:deliberately-unused-request','enc:v1:deliberately-unused-authorization')`,
			job.ID, job.UserID, job.TokenID, job.Model, job.Status, job.Stream, job.Native, job.ChannelID, job.RoutingCiphertext, job.FinalResponseCiphertext,
			job.ErrorCiphertext, job.UpstreamID, job.UpstreamSequence, job.LastSequence, job.CancelRequested, job.CreatedAt, job.UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			_, err = source.Exec(ctx, `INSERT INTO gateway.responses_background_events VALUES($1,$2,$3,$4,$5,$6)`, int64(i*2)+event.ID, event.JobID, event.Sequence, event.Type, event.PayloadCiphertext, event.CreatedAt)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("V3_MIGRATION_SOURCE_CRYPTO_SECRET", backgroundSourceTestSecret)
}

func migrationBackgroundRedis(t *testing.T, canonical bool) (*live.RedisBackgroundRepository, *redis.Client) {
	t.Helper()
	address := os.Getenv("V3_MIGRATION_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("V3_MIGRATION_TEST_REDIS_ADDR not set; use a disposable dedicated Redis")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	crypto, err := catalog.NewAESGCM(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	prefix := t.Name()
	if canonical {
		prefix = ""
		t.Setenv("V3_REDIS_ADDR", address)
		t.Setenv("V3_REDIS_PASSWORD", "")
		t.Setenv("V3_SECRET_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	}
	repository, err := live.NewRedisBackgroundRepository(client, prefix, crypto.DeriveKey("background-jobs"))
	if err != nil {
		t.Fatal(err)
	}
	return repository, client
}
