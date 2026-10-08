package live

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

type changingSocketAuth struct {
	revoked    atomic.Bool
	restricted atomic.Bool
}

func (a *changingSocketAuth) Authorize(context.Context, string) (gateway.Principal, error) {
	if a.revoked.Load() {
		return gateway.Principal{}, gateway.ErrInvalidKey
	}
	p := gateway.Principal{UserID: 1, KeyID: 11, Group: "default"}
	if a.restricted.Load() {
		p.AllowedModels = []string{"other"}
	}
	return p, nil
}

func TestResponsesSocketRechecksRevocationAndPolicyBetweenTurns(t *testing.T) {
	for _, policy := range []bool{false, true} {
		t.Run(map[bool]string{false: "revoked", true: "model_permission_changed"}[policy], func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				testResponseSSE(w, "resp_first")
			}))
			defer upstream.Close()
			ledger := &liveLedger{finalized: make(chan gateway.Outcome, 4)}
			h, server, _ := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 2, Provider: "openai", BaseURL: upstream.URL}}, ledger)
			auth := &changingSocketAuth{}
			h.cfg.Auth = auth
			conn := dialSocket(t, server.URL, "/responses")
			if err := websocket.Message.Send(conn, `{"type":"response.create","model":"gpt-test","input":"first"}`); err != nil {
				t.Fatal(err)
			}
			readSocketCompleted(t, conn)
			<-ledger.finalized
			if policy {
				auth.restricted.Store(true)
			} else {
				auth.revoked.Store(true)
			}
			if err := websocket.Message.Send(conn, `{"type":"response.append","input":"second"}`); err != nil {
				t.Fatal(err)
			}
			var message wireFrame
			if err := frameCodec.Receive(conn, &message); err != nil {
				t.Fatal(err)
			}
			want := int64(401)
			if policy {
				want = 403
			}
			if gjson.GetBytes(message.data, "status").Int() != want || calls.Load() != 1 || ledger.reserves.Load() != 1 {
				t.Fatalf("second turn admitted: message=%s upstream=%d reserves=%d", message.data, calls.Load(), ledger.reserves.Load())
			}
		})
	}
}
