//go:build pgintegration

package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetainedDesktopHTTPKeysSummaryLogsAndBoundaries(t *testing.T) {
	s, a, _ := desktopFixture(t)
	ctx := context.Background()
	start, err := s.Start(ctx, StartInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(ctx, a.ID, start.SessionID, true); err != nil {
		t.Fatal(err)
	}
	p, err := s.Poll(ctx, start.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	request := func(method, path, body string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+p.AccessToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s %s status=%d body=%s", method, path, w.Code, w.Body.String())
		}
		var out map[string]any
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || out["success"] != true {
			t.Fatal("failed protocol", path, w.Body.String())
		}
		return out
	}
	first := request("POST", "/api/desktop/tokens/ensure", `{"device_name":"Default","group":"default"}`)["data"].(map[string]any)
	second := request("POST", "/api/desktop/tokens/ensure", `{"device_name":"Default","group":"default"}`)["data"].(map[string]any)
	if first["created"] != true || second["created"] != false || first["full_key"] != second["full_key"] {
		t.Fatal("ensure did not preserve key")
	}
	k := first["token"].(map[string]any)
	if k["status"] != float64(1) {
		t.Fatal("legacy token status not preserved")
	}
	var kid int64
	if err := s.pool.QueryRow(ctx, `SELECT id FROM v3_identity.api_keys WHERE user_id=$1 LIMIT 1`, a.ID).Scan(&kid); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/desktop/tokens/%d/group", kid)
	request("PUT", path, `{"group":"default"}`)
	request("POST", fmt.Sprintf("/api/desktop/tokens/%d/key", kid), `{}`)
	for _, path := range []string{"/api/desktop/account/summary", "/api/desktop/usage/logs?p=2", "/api/desktop/usage/trends?days=30", "/api/desktop/groups", "/api/desktop/pricing", "/api/desktop/group-status", "/api/desktop/tokens", "/api/desktop/config/templates", "/api/desktop/config/template?tool=claude-code", "/api/desktop/service/status", fmt.Sprintf("/api/desktop/tokens/%d/config", kid)} {
		request("GET", path, "")
	}
	request("POST", "/api/desktop/diagnostics/report", `{"report_type":"crash","source":"desktop","summary":"test","payload":"panic with Bearer hidden-secret","app_version":"1","platform":"windows","locale":"zh","consent":true}`)
	request("POST", "/api/desktop/telemetry/events", `{"event_name":"auth_connected","source":"desktop","payload":{"api_key":"hidden-secret"},"app_version":"1","platform":"windows","locale":"zh","consent":true}`)
	for _, path := range []string{"/api/desktop/usage/trends?days=31", "/api/desktop/usage/logs?start_timestamp=broken"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+p.AccessToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid query %s status=%d", path, w.Code)
		}
	}
}
