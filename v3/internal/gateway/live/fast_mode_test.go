package live

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"golang.org/x/net/websocket"
)

func TestFastModeWebSocketForwardsTierAndKeepsDowngrade(t *testing.T) {
	requests := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_tier\",\"status\":\"completed\",\"service_tier\":\"default\",\"usage\":{\"input_tokens\":9,\"output_tokens\":2}}}\n\n")
	}))
	defer upstream.Close()
	ledger := &liveLedger{finalized: make(chan gateway.Outcome, 1)}
	_, server, _ := socketFixture(t, []gateway.Target{{ChannelID: 1, CredentialID: 2, Provider: "openai", BaseURL: upstream.URL, Secret: "fixture"}}, ledger)
	conn := dialSocket(t, server.URL, "/responses")
	if err := frameCodec.Send(conn, wireFrame{kind: websocket.TextFrame, data: []byte(`{"type":"response.create","model":"gpt-test","input":"hi","service_tier":"priority"}`)}); err != nil {
		t.Fatal(err)
	}
	readSocketCompleted(t, conn)
	if body := <-requests; gjson.GetBytes(body, "service_tier").Str != "priority" {
		t.Fatalf("Fast removed from socket request: %s", body)
	}
	if out := <-ledger.finalized; !out.Charge || out.Usage.ServiceTier != "default" || out.Usage.PromptTokens != 9 {
		t.Fatalf("actual tier lost: %+v", out)
	}
}

func TestFastModeBackgroundPersistsTierWithAndWithoutUsage(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, usageReported := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "codex", true: "native"}[native], map[bool]string{false: "estimate", true: "usage"}[usageReported]}, "/"), func(t *testing.T) {
				requests := make(chan []byte, 1)
				usage := ""
				if usageReported {
					usage = `,"usage":{"input_tokens":5,"output_tokens":3}`
				}
				response := `{"id":"resp_up","status":"completed","service_tier":"default","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]` + usage + `}`
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					requests <- body
					if native {
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, response)
					} else {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":"+response+"}\n\n")
					}
				}))
				defer upstream.Close()
				provider := "codex"
				if native {
					provider = "openai"
				}
				h, _, wrapped, repo, billing := backgroundJobsFixture(t, upstream.URL, provider)
				id := createBackgroundForTest(t, wrapped, "/responses", `{"model":"gpt-test","input":"hello","background":true,"service_tier":"fast"}`)
				if err := h.Reconcile(context.Background(), 10); err != nil {
					t.Fatal(err)
				}
				if body := <-requests; gjson.GetBytes(body, "service_tier").Str != "fast" {
					t.Fatalf("Fast removed from background request: %s", body)
				}
				job, err := repo.GetOwned(context.Background(), id, 1, 11)
				out, count, _ := billing.result(id)
				if err != nil || !job.Billed || job.Usage.ServiceTier != "default" || !out.Charge || out.Usage.ServiceTier != "default" || out.Usage.Estimated == usageReported || count != 1 {
					t.Fatalf("job=%+v outcome=%+v count=%d err=%v", job, out, count, err)
				}
			})
		}
	}
}
