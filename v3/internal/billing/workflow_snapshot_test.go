package billing

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func workflowTestRequest() *gateway.Request {
	return &gateway.Request{ID: "task-1", Model: "video", Body: []byte(`{"prompt":"credential-in-body","seconds":"4"}`),
		Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "default"},
		Targets:   []gateway.Target{{ChannelID: 3, CredentialID: 300, Secret: "upstream-secret", ProxyURL: "https://user:pass@proxy"}}}
}

func TestWorkflowSnapshotFreezesPricingWithoutCredentials(t *testing.T) {
	req := workflowTestRequest()
	h := &hold{account: 42, amount: 250, price: catalog.Price{Mode: "per_request", PerRequest: 9007199254740993,
		Rules: map[string]any{"tool_prices": map[string]any{"web_search": int64(9007199254740993)}}}, multiplier: 1.5,
		pricingInput: pricing.RequestInput{Body: req.Body, Now: time.Unix(123, 0), Headers: map[string]string{
			"X-Region": "test", "Authorization": "caller-secret", "X-Api-Key": "key-secret", "Cookie": "cookie-secret"}},
		cardMultiplier: .5, cardChannels: map[int64]bool{3: true}, budgetAccount: 44,
		funding: []fundingHold{{account: 43, amount: 100}, {account: 42, amount: 150}, {account: 44, amount: 250}}}
	reservation, err := encodeWorkflowHold(req, h)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"upstream-secret", "caller-secret", "key-secret", "cookie-secret", "user:pass", "credential-in-body"} {
		if strings.Contains(string(reservation.Data), secret) {
			t.Fatalf("credential leaked: %s", secret)
		}
	}
	restored, err := restoreWorkflowHold(req, reservation)
	if err != nil {
		t.Fatal(err)
	}
	if restored.price.PerRequest != 9007199254740993 || restored.price.Rules["tool_prices"].(map[string]any)["web_search"] != json.Number("9007199254740993") ||
		restored.multiplier != 1.5 || restored.cardMultiplier != .5 || !restored.cardChannels[3] || len(restored.funding) != 3 || restored.budgetAccount != 44 ||
		restored.pricingInput.Now.Unix() != 123 || restored.pricingInput.Headers["X-Region"] != "test" || restored.funding[0].keys.reservation != keysFor(43, req.ID).reservation {
		t.Fatalf("frozen state lost: %+v", restored)
	}
}

func TestWorkflowSnapshotBindsIdentityAndRequest(t *testing.T) {
	req := workflowTestRequest()
	reservation, err := encodeWorkflowHold(req, &hold{account: 42, amount: 5, price: catalog.Price{Mode: "per_request", PerRequest: 5}, multiplier: 1})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*gateway.Request){
		"request":    func(r *gateway.Request) { r.ID = "other" },
		"user":       func(r *gateway.Request) { r.Principal.UserID++ },
		"key":        func(r *gateway.Request) { r.Principal.KeyID++ },
		"group":      func(r *gateway.Request) { r.Principal.Group = "other" },
		"model":      func(r *gateway.Request) { r.Model = "other" },
		"body":       func(r *gateway.Request) { r.Body = []byte(`{}`) },
		"channel":    func(r *gateway.Request) { r.Targets[0].ChannelID++ },
		"credential": func(r *gateway.Request) { r.Targets[0].CredentialID++ },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			changed := workflowTestRequest()
			change(changed)
			if _, err := restoreWorkflowHold(changed, reservation); err == nil {
				t.Fatal("restored another request's hold")
			}
		})
	}
}
