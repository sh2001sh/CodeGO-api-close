//go:build pgintegration

package legacy

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const restoredTestTOTP = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
const restoredTestCrypto = "local-source-crypto-fixture"

func sourceEncryptedFixture(t *testing.T, plain string) string {
	t.Helper()
	key := sha256.Sum256([]byte(restoredTestCrypto))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	return "enc:v1:" + base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil))
}

func seedRestoredStateFixture(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	seedCatalogDataFixture(t, source)
	hash, err := bcrypt.GenerateFromPassword([]byte("ABCD-EFGH"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Exec(context.Background(), `
	 UPDATE migration_source.users SET setting='{"favorite_model_ids":[31,31]}' WHERE id=7;
	 CREATE TABLE migration_source.two_fas(id bigint PRIMARY KEY,user_id bigint,secret text,is_enabled bool,failed_attempts bigint,locked_until timestamptz,last_used_at timestamptz,created_at timestamptz,deleted_at timestamptz);
	 CREATE TABLE migration_source.two_fa_backup_codes(id bigint PRIMARY KEY,user_id bigint,code_hash text,is_used bool,used_at timestamptz,deleted_at timestamptz);
	 CREATE TABLE migration_source.desktop_authorized_devices(id bigint PRIMARY KEY,user_id bigint,device_name text,platform text,app_version text,access_token text,scopes text,status text,created_at bigint,last_used_at bigint,expires_at bigint,revoked_at bigint);
	 CREATE TABLE migration_source.desktop_auth_sessions(session_id text PRIMARY KEY,user_code text,user_id bigint,device_id bigint,device_name text,platform text,app_version text,status text,created_at bigint,approved_at bigint,expires_at bigint);
	 INSERT INTO migration_source.desktop_authorized_devices VALUES
	 (51,7,'Original PC','windows','1.0','desktop_original_active_fixture','desktop:account:read,desktop:config:write','active',1700000000,1700000001,0,0),
	 (52,7,'Revoked PC','windows','0.9','desktop_original_revoked_fixture','','revoked',1700000000,1700000001,1800000000,1700000002),
	 (53,7,'Expired PC','windows','0.9','desktop_original_expired_fixture','desktop:account:read','active',1600000000,1600000001,1650000000,0);
	 INSERT INTO migration_source.desktop_auth_sessions VALUES
	 ('original-session','1234ABCD',7,51,'Original PC','windows','1.0','approved',1700000000,1700000001,1700000600),
	 ('expired-session','ABCD1234',0,0,'Old PC','windows','0.9','pending',1600000000,0,1600000600);`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Exec(context.Background(), `INSERT INTO migration_source.two_fas VALUES
	 (1,7,$1,true,4,'2023-11-14T22:20:00Z','2023-11-14T22:13:20Z','2023-01-01T00:00:00Z',NULL),
	 (2,7,'do-not-restore',true,0,NULL,NULL,'2022-01-01T00:00:00Z','2023-01-01T00:00:00Z');`, sourceEncryptedFixture(t, restoredTestTOTP))
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Exec(context.Background(), `INSERT INTO migration_source.two_fa_backup_codes VALUES(61,7,$1,false,NULL,NULL),(62,7,$1,true,'2023-11-14T22:14:00Z',NULL),(63,7,$1,false,NULL,'2023-01-01T00:00:00Z');`, string(hash))
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Exec(context.Background(), `INSERT INTO migration_source.options VALUES('model_deployment.ionet.api_key',$1),('model_deployment.ionet.enabled','true');`, sourceEncryptedFixture(t, "local-original-deployment-token"))
	if err != nil {
		t.Fatal(err)
	}
}

func restoredSourceDigest(t *testing.T, source *pgxpool.Pool) string {
	t.Helper()
	var digest string
	err := source.QueryRow(context.Background(), `SELECT md5(string_agg(value,'|' ORDER BY value)) FROM (
	 SELECT to_jsonb(t)::text value FROM migration_source.two_fas t UNION ALL
	 SELECT to_jsonb(t)::text FROM migration_source.two_fa_backup_codes t UNION ALL
	 SELECT to_jsonb(t)::text FROM migration_source.desktop_authorized_devices t UNION ALL
	 SELECT to_jsonb(t)::text FROM migration_source.desktop_auth_sessions t UNION ALL
	 SELECT to_jsonb(t)::text FROM migration_source.users t UNION ALL
	 SELECT to_jsonb(t)::text FROM migration_source.options t) all_rows`).Scan(&digest)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}
