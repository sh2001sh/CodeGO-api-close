package auxiliary

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/routing"
)

func TestAuxiliaryVirtualGroupsUseActualCardPlannerBeforeReserve(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":1}}`)
	}))
	defer server.Close()
	now := time.Now()
	snapshot := &catalog.Snapshot{
		Groups: map[string]catalog.Group{"card-group": {Name: "card-group", Multiplier: 1}},
		Channels: map[int64]*catalog.Channel{1: {ID: 1, Provider: "openai", BaseURL: server.URL, Weight: 1, MultiplierCardUserEnabled: true,
			Credentials: []catalog.Credential{{ID: 1, ChannelID: 1, Secret: "upstream-secret"}}}},
		Routes:          map[string]map[string][]catalog.Route{"card-group": {"alias": {{ChannelID: 1, Weight: 1, Strategy: "fill_first"}}}},
		Settings:        map[string]json.RawMessage{"blind_box_setting.multiplier_card_route_group": json.RawMessage(`"card-group"`)},
		AccountProfiles: map[int64]catalog.AccountProfile{1: {Cards: []catalog.MultiplierCard{{ID: 1, PropType: "zero_hour_multiplier", MultiplierPPM: 0, ExpiresAt: now.Add(time.Second)}}}},
	}
	h, _, settle, _ := testHandler(t, server.URL)
	h.cfg.Planner = routing.New(func() *catalog.Snapshot { return snapshot }, routing.Config{Now: func() time.Time { return now }})
	for _, group := range []string{"zero-hour", "monthly-pass"} {
		h.cfg.Authorizer = policyAuth{gateway.Principal{UserID: 1, KeyID: 2, Group: group, AllowedGroups: []string{"default"}}}
		w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"hello"}`)
		if w.Code != 200 || !settle.out.Charge || settle.out.Target.Group != "card-group" {
			t.Fatalf("group=%s status=%d outcome=%+v body=%s", group, w.Code, settle.out, w.Body.String())
		}
	}
	beforeCalls, beforeReserves := calls.Load(), settle.reserves
	now = now.Add(time.Second)
	h.cfg.Authorizer = policyAuth{gateway.Principal{UserID: 1, KeyID: 2, Group: "zero-hour", AllowedGroups: []string{"default"}}}
	w := invoke(h, "/v1/embeddings", `{"model":"alias","input":"hello"}`)
	if w.Code != 503 || calls.Load() != beforeCalls || settle.reserves != beforeReserves {
		t.Fatalf("expired zero-hour admitted status=%d calls=%d reserves=%d", w.Code, calls.Load(), settle.reserves)
	}
}
