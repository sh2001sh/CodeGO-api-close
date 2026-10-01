//go:build pgintegration

package audit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHistoricalEventsRequestsAndAttemptsPreserveScopeMoneyAndPagination(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		id, user, key, amount int64
		typeID                int
	}{{1, 7, 11, 9007199254740993, 1}, {2, 7, 12, -33, 6}, {3, 7, 11, 0, 5}, {4, 8, 21, 66, 2}, {5, 0, 0, 0, 4}} {
		_, err := pool.Exec(ctx, `INSERT INTO v3_audit.events
 (id,user_id,key_id,created_at,event_type,content,username,token_name,model,amount,
 prompt_tokens,completion_tokens,duration_seconds,is_stream,channel_id,group_name,ip,request_id,upstream_request_id,metadata)
 VALUES($1,$2,$3,$4,$5,'=formula','private-user','private-token','model',$6,0,0,0,false,13,'default',
 'private-ip','same-request','upstream-secret','{"api_key":"hidden-secret"}')`, row.id, row.user, row.key, at, row.typeID, row.amount)
		if err != nil {
			t.Fatal(err)
		}
	}
	s := New(pool, Config{Authenticate: func(*http.Request) (Principal, error) { return Principal{UserID: 7}, nil }})
	page, err := s.ListEvents(ctx, Principal{UserID: 7}, EventQuery{Query: Query{Limit: 2}})
	if err != nil || len(page.Items) != 2 || page.Items[0].ID != 3 || page.Items[1].ID != 2 || !page.HasMore {
		t.Fatalf("events first page %+v %v", page, err)
	}
	next, err := s.ListEvents(ctx, Principal{UserID: 7}, EventQuery{Query: Query{Limit: 2, Cursor: page.NextCursor}})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != 1 || next.Items[0].Amount != 9007199254740993 || next.HasMore {
		t.Fatalf("events next page %+v %v", next, err)
	}
	key, err := s.ListEvents(ctx, Principal{UserID: 7, KeyID: 11, Admin: true}, EventQuery{})
	if err != nil || len(key.Items) != 2 || key.Items[1].ID != 1 {
		t.Fatalf("key scope %+v %v", key, err)
	}
	refundType := 6
	refunds, err := s.ListEvents(ctx, Principal{UserID: 7}, EventQuery{EventType: &refundType})
	if err != nil || len(refunds.Items) != 1 || refunds.Items[0].Amount != -33 {
		t.Fatalf("refund event %+v %v", refunds, err)
	}
	admin, err := s.ListEvents(ctx, Principal{UserID: 7, Admin: true}, EventQuery{})
	if err != nil || len(admin.Items) != 5 || admin.Items[0].UserID != 0 {
		t.Fatalf("admin/system events %+v %v", admin, err)
	}
	for _, path := range []string{"/api/audit/events", "/api/audit/events/export"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, "9007199254740993") || strings.Contains(body, "hidden-secret") || strings.Contains(body, "private-token") || strings.Contains(body, "private-ip") {
			t.Fatalf("event response %s %d %s", path, w.Code, body)
		}
		if strings.HasSuffix(path, "/export") && !strings.Contains(body, "'=formula") {
			t.Fatal("historical CSV formula was not escaped")
		}
	}
	for _, row := range []struct {
		request   string
		user, key int64
	}{{"request-a", 7, 11}, {"request-b", 7, 12}, {"request-c", 7, 11}, {"other", 8, 21}} {
		_, err := pool.Exec(ctx, `INSERT INTO v3_audit.request_audits
 (request_id,trace_id,user_id,key_id,model,group_name,protocol,request_type,status,counted_in_success_rate,
 billable,amount,prompt_tokens,completion_tokens,final_channel_id,attempts_count,retry_count,status_code,
 error_code,started_at,completed_at,created_at,updated_at)
 VALUES($1,'trace',$2,$3,'model','default','openai','chat','completed',true,true,9007199254740993,1,2,13,3,2,200,'',$4,$4,$4,$4)`,
			row.request, row.user, row.key, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		id, request string
		number      int
	}{{"attempt-a", "request-a", 0}, {"attempt-b", "request-a", 0}, {"attempt-c", "request-a", 1}, {"hidden", "other", 0}} {
		_, err := pool.Exec(ctx, `INSERT INTO v3_audit.request_attempt_audits
 (attempt_id,request_id,attempt_no,retry_index,channel_id,model,fault_domain,request_type,status,success,status_code,
 failure_class,stage,started_at,completed_at,duration_ms,created_at)
 VALUES($1,$2,$3,0,13,'model','provider','chat','completed',true,200,'','response',$4,$4,2,$4)`, row.id, row.request, row.number, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	requests, err := s.ListRequests(ctx, Principal{UserID: 7}, Query{Limit: 2})
	if err != nil || len(requests.Items) != 2 || !requests.HasMore || requests.Items[0].RequestID != "request-c" {
		t.Fatalf("request first page %+v %v", requests, err)
	}
	requestsNext, err := s.ListRequests(ctx, Principal{UserID: 7}, Query{Limit: 2, Cursor: requests.NextCursor})
	if err != nil || len(requestsNext.Items) != 1 || requestsNext.Items[0].RequestID != "request-a" || requestsNext.Items[0].Amount != 9007199254740993 {
		t.Fatalf("request next page %+v %v", requestsNext, err)
	}
	keyRequests, err := s.ListRequests(ctx, Principal{UserID: 7, KeyID: 11, Admin: true}, Query{})
	if err != nil || len(keyRequests.Items) != 2 {
		t.Fatalf("key requests %+v %v", keyRequests, err)
	}
	attempts, err := s.ListAttempts(ctx, Principal{UserID: 7, KeyID: 11}, "request-a", "", 1)
	if err != nil || len(attempts.Items) != 1 || attempts.Items[0].AttemptID != "attempt-a" || !attempts.HasMore {
		t.Fatalf("attempt first page %+v %v", attempts, err)
	}
	attemptNext, err := s.ListAttempts(ctx, Principal{UserID: 7}, "request-a", attempts.NextCursor, 2)
	if err != nil || len(attemptNext.Items) != 2 || attemptNext.Items[0].AttemptID != "attempt-b" || attemptNext.HasMore {
		t.Fatalf("attempt next page %+v %v", attemptNext, err)
	}
	for _, check := range []struct {
		p       Principal
		request string
	}{{Principal{UserID: 7}, "other"}, {Principal{UserID: 7, KeyID: 12, Admin: true}, "request-a"}, {Principal{UserID: 7}, "missing"}} {
		if _, err := s.ListAttempts(ctx, check.p, check.request, "", 50); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign/missing attempts leaked %+v: %v", check, err)
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/audit/requests/other/attempts", nil))
	if w.Code != 404 {
		t.Fatalf("foreign attempt HTTP %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_billing.ledger_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("historical facts posted money %d %v", count, err)
	}
}
