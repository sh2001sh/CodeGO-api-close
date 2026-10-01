//go:build pgintegration

package catalogcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestLegacyBulkPostgres(t *testing.T) {
	pool := legacyBulkPool(t)
	ctx := context.Background()
	enc, err := catalog.NewAESGCM(bytes.Repeat([]byte{13}, 32))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, enc, nil).Register(mux, func(h http.Handler) http.Handler { return h })
	call := func(method, path, body string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != expected {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, expected, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "copy-only-secret") {
			t.Fatal("credential was returned by a bulk operation")
		}
		return w
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if e := pool.QueryRow(ctx, sql, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	read := func(id int64) Channel {
		t.Helper()
		c, e := scanChannel(pool.QueryRow(ctx, channelSelect+` WHERE c.id=$1`, id))
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	resultID := func(w *httptest.ResponseRecorder) int64 {
		t.Helper()
		var result struct {
			Data struct {
				ID int64 `json:"id"`
			} `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.Data.ID <= 0 {
			t.Fatal("missing persisted channel identifier")
		}
		return result.Data.ID
	}
	call("PUT", "/api/catalog/groups/default", `{"multiplier":1}`, 200)
	call("PUT", "/api/catalog/groups/alternate", `{"multiplier":1}`, 200)
	sourceID := resultID(call("POST", "/api/catalog/channels", `{"name":"origin","provider":"openai","status":"auto_disabled","tag":"source","models":["a","b"],"groups":["default"],"weight":3,"priority":5,"max_concurrency":4,"max_user_concurrency":2,"auto_disable":true,"model_mapping":{"a":"upstream"},"param_override":{"temperature":0.2},"header_override":{"X-Test":"stable"},"status_code_mapping":{"429":503},"settings":{"region":"test"},"credentials":[{"secret":"copy-only-secret","kind":"oauth","expires_at":"2030-01-01T00:00:00Z","max_concurrency":7,"fingerprint":{"user_agent":"stable-ua","tls_profile":"firefox"}}]}`, 200))
	exec(`UPDATE v3_catalog.channel_credentials SET status='disabled' WHERE channel_id=$1`, sourceID)
	w := call("POST", fmt.Sprintf("/api/channel/copy/%d?suffix=_duplicate&reset_balance=false", sourceID), "", 200)
	copyID := resultID(w)
	source, copied := read(sourceID), read(copyID)
	copied.ID, copied.Name = source.ID, source.Name
	if !reflect.DeepEqual(source, copied) {
		t.Fatal("copied channel settings or memberships changed")
	}
	if count(`SELECT count(*) FROM v3_catalog.channel_credentials a JOIN v3_catalog.channel_credentials b
	 ON a.channel_id=$1 AND b.channel_id=$2 AND a.id<>b.id AND a.secret=b.secret AND a.kind=b.kind
	 AND a.status=b.status AND a.expires_at=b.expires_at AND a.max_concurrency=b.max_concurrency AND a.fingerprint=b.fingerprint`, sourceID, copyID) != 1 {
		t.Fatal("credential ciphertext or metadata was not preserved")
	}
	var ciphertext []byte
	if err = pool.QueryRow(ctx, `SELECT secret FROM v3_catalog.channel_credentials WHERE channel_id=$1`, copyID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	plain, err := enc.Decrypt(ciphertext)
	if err != nil || string(plain) != "copy-only-secret" || bytes.Contains(ciphertext, plain) {
		t.Fatal("copied credential encryption invalid")
	}
	// A failure after inserting the channel and memberships rolls back the clone.
	exec(`CREATE FUNCTION v3_catalog.reject_bulk_copy() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test credential write rejected'; END $$`)
	exec(`CREATE TRIGGER reject_bulk_copy BEFORE INSERT ON v3_catalog.channel_credentials FOR EACH ROW EXECUTE FUNCTION v3_catalog.reject_bulk_copy()`)
	call("POST", fmt.Sprintf("/api/channel/copy/%d", sourceID), "", 503)
	if count(`SELECT count(*) FROM v3_catalog.channels`) != 2 {
		t.Fatal("failed credential copy left a partial channel")
	}
	exec(`DROP TRIGGER reject_bulk_copy ON v3_catalog.channel_credentials`)
	call("POST", "/api/channel/copy/999999", "", 404)
	call("POST", fmt.Sprintf("/api/channel/copy/%d?suffix=%s", sourceID, strings.Repeat("x", 250)), "", 400)
	call("POST", "/api/channel/batch/tag", fmt.Sprintf(`{"ids":[%d,%d],"tag":"combined"}`, sourceID, copyID), 200)
	call("POST", "/api/channel/tag/enabled", `{"tag":"combined"}`, 200)
	if count(`SELECT count(*) FROM v3_catalog.channels WHERE status='enabled'`) != 2 {
		t.Fatal("tag enabling did not update both channels")
	}
	call("POST", "/api/channel/tag/disabled", `{"tag":"combined"}`, 200)
	if count(`SELECT count(*) FROM v3_catalog.channels WHERE status='disabled'`) != 2 {
		t.Fatal("tag disabling did not update both channels")
	}
	// A missing group must roll back tag/config changes and removed memberships.
	call("PUT", "/api/channel/tag", `{"tag":"combined","new_tag":"failed","priority":0,"weight":0,"models":"replacement","groups":"missing"}`, 409)
	if got := read(sourceID); got.Tag == nil || *got.Tag != "combined" || got.Priority != 5 || len(got.Models) != 2 || got.Groups[0] != "default" {
		t.Fatal("failed tag membership edit partially persisted")
	}
	otherID := resultID(call("POST", "/api/catalog/channels", `{"name":"other","provider":"openai","tag":"renamed","models":["other"],"groups":["default"],"priority":9}`, 200))
	call("PUT", "/api/channel/tag", `{"tag":"combined","new_tag":"renamed","priority":0,"weight":0,"models":"b,c,c","groups":"alternate","model_mapping":"{}","param_override":"","header_override":"{}"}`, 200)
	got := read(sourceID)
	if got.Priority != 0 || got.Weight != 0 || len(got.ModelMapping) != 0 || string(got.ParamOverride) != "{}" || len(got.Models) != 2 || len(got.Groups) != 1 || got.Groups[0] != "alternate" {
		t.Fatal("zero/reset tag update or membership replacement failed")
	}
	if got = read(otherID); got.Priority != 9 || len(got.Models) != 1 || got.Models[0] != "other" {
		t.Fatal("renaming a tag changed unrelated destination-tag channels")
	}
	w = call("GET", "/api/channel/tag/models?tag=renamed", "", 200)
	if !strings.Contains(w.Body.String(), `"data":"b,c"`) {
		t.Fatal("tag models did not select the longest channel model list")
	}
	w = call("GET", "/api/channel/tag/models?tag=absent", "", 200)
	if !strings.Contains(w.Body.String(), `"data":""`) {
		t.Fatal("absent tag model response incompatible")
	}
	call("PUT", "/api/channel/tag", `{"tag":"renamed","models":"","groups":""}`, 200)
	if count(`SELECT count(*) FROM v3_catalog.channel_models`) != 5 || read(sourceID).Groups[0] != "alternate" {
		t.Fatal("empty legacy memberships changed existing groups or models")
	}
	call("POST", "/api/channel/batch/tag", fmt.Sprintf(`{"ids":[%d,%d],"tag":null}`, sourceID, copyID), 200)
	if read(sourceID).Tag != nil || read(copyID).Tag != nil {
		t.Fatal("batch null tag did not clear tags")
	}
	// A foreign-key conflict rolls back the entire batch, not just one member.
	var userID int64
	if err = pool.QueryRow(ctx, `INSERT INTO v3_identity.users(username) VALUES('bulk-user') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO v3_channelmarket.channel_user_blocks(channel_id,user_id) VALUES($1,$2)`, copyID, userID)
	call("POST", "/api/channel/batch", fmt.Sprintf(`{"ids":[%d,%d]}`, sourceID, copyID), 409)
	if count(`SELECT count(*) FROM v3_catalog.channels`) != 3 {
		t.Fatal("failed batch deletion removed an unblocked member")
	}
	exec(`DELETE FROM v3_channelmarket.channel_user_blocks WHERE channel_id=$1`, copyID)
	w = call("POST", "/api/channel/batch", fmt.Sprintf(`{"ids":[%d,%d,999999]}`, sourceID, sourceID), 200)
	if !strings.Contains(w.Body.String(), `"data":1`) || count(`SELECT count(*) FROM v3_catalog.channel_credentials WHERE channel_id=$1`, sourceID) != 0 {
		t.Fatal("batch delete count or cascading credential deletion failed")
	}
	exec(`UPDATE v3_catalog.channels SET status='auto_disabled' WHERE id=$1`, copyID)
	call("POST", "/api/catalog/channels", `{"name":"manually-disabled","provider":"openai","status":"disabled"}`, 200)
	w = call("DELETE", "/api/channel/disabled", "", 200)
	if !strings.Contains(w.Body.String(), `"data":2`) || count(`SELECT count(*) FROM v3_catalog.channels`) != 1 || read(otherID).Status != "enabled" {
		t.Fatal("disabled deletion failed or removed enabled channel")
	}
}
