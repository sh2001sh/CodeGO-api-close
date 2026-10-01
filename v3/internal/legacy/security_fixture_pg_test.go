//go:build pgintegration

package legacy

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func seedSecurityFixture(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	_, err := source.Exec(context.Background(), `
	 CREATE TABLE migration_source.account_request_abuse_states(user_id bigint PRIMARY KEY,strikes integer NOT NULL,
	 restricted_until bigint NOT NULL,last_window_end bigint NOT NULL,blocked boolean NOT NULL,evidence text,updated_at bigint);
	 INSERT INTO migration_source.users VALUES(8,'blocked-user','bcrypt-placeholder',1,1,'default',0,0,'{}');
	 INSERT INTO migration_source.account_request_abuse_states VALUES(7,1,1800086400,1799999980,false,'original restricted evidence',1800000000),(8,2,0,1799999900,true,NULL,NULL);
	 CREATE TABLE migration_source.security_audit_events(
	 id varchar(64) PRIMARY KEY,dedupe_key varchar(64) UNIQUE NOT NULL,request_id varchar(128),source varchar(32) NOT NULL,
	 decision varchar(24) NOT NULL,risk_code varchar(64) NOT NULL,severity varchar(16) NOT NULL,
	 user_id bigint,token_id bigint,token_name varchar(128),channel_id bigint,marketplace_channel_id varchar(64),marketplace_group_id varchar(64),
	 owner_user_id bigint,model varchar(191),protocol varchar(96),http_status integer,upstream_error_type varchar(64),upstream_error_code varchar(64),
	 upstream_error_message text,upstream_error_body text,prompt_hash varchar(64),prompt_preview varchar(512),prompt_length integer,message_count integer,
	 billing_result varchar(24),notification_status varchar(24),notification_targets integer NOT NULL,notification_success integer NOT NULL,
	 notified_at timestamptz,review_status varchar(24) NOT NULL,review_note varchar(1000),reviewed_by bigint,reviewed_at timestamptz,created_at timestamptz,updated_at timestamptz);
	 INSERT INTO migration_source.security_audit_events VALUES
	 ('original-string-id','original-dedupe','original-request','upstream_policy','blocked','cyber_policy','high',7,11,'kept-key',13,'market-original','group-original',7,
	 'chat-model','chat',403,'policy_error','policy_block','dummy-private-message','dummy-private-body','original-hash','dummy-private-preview',17,2,
	 'not_charged','sent',2,1,'2025-01-01T01:02:03.123456Z','resolved','original note',7,'2025-01-02T01:02:03.654321Z','2025-01-01T01:02:03.123456Z',NULL),
	 ('deleted-historical-id','historical-dedupe',NULL,'prompt_guard','blocked','risk','high',9007199254740993,NULL,NULL,999,NULL,NULL,8,
	 NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,0,0,NULL,'unreviewed',NULL,NULL,NULL,NULL,NULL);`)
	if err != nil {
		t.Fatal(err)
	}
}

func migrationSecurityRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("V3_MIGRATION_SECURITY_REDIS_ADDR")
	if addr == "" {
		addr = os.Getenv("V3_TEST_REDIS_ADDR")
	}
	if addr == "" {
		t.Skip("disposable V3_MIGRATION_SECURITY_REDIS_ADDR or V3_TEST_REDIS_ADDR required")
	}
	// Separate logical DB from native security fixtures; only our exact keys are removed.
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	keys := []string{"v3:request-abuse:state:7", "v3:request-abuse:rpm:7", "v3:request-abuse:state:8", "v3:request-abuse:rpm:8"}
	if err := client.Del(context.Background(), keys...).Err(); err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Del(context.Background(), keys...).Err(); err != nil {
			t.Error(err)
		}
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return client
}
