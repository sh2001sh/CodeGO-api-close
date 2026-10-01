package billing

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/gemini"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func veoPriceFixture(body, model string, unitPrice int64) (*gateway.Request, *catalog.Snapshot) {
	req := &gateway.Request{ID: "veo-task", Model: "video-alias", Body: []byte(body),
		Principal: gateway.Principal{UserID: 7, KeyID: 70, Group: "default"},
		Targets: []gateway.Target{{Provider: "gemini", ChannelID: 3, CredentialID: 300,
			Group: "default", MultiplierPPM: 1_000_000, UpstreamModel: model}}}
	snapshot := &catalog.Snapshot{Channels: map[int64]*catalog.Channel{3: {ID: 3}},
		Groups:          map[string]catalog.Group{"default": {Multiplier: 1}},
		AccountProfiles: map[int64]catalog.AccountProfile{7: {WalletAccountID: 42}},
		Prices: map[string]catalog.Price{"video-alias": {Mode: "per_request", PerRequest: unitPrice,
			Rules: map[string]any{"billing_unit": "video_second", "money_quantum": 2}}}}
	return req, snapshot
}

// Compare source-shaped requests, the actual native payload and parsed result
// with literal v2 old-unit totals converted x2. No fabricated token accounting.
func TestVeoNativeParametersPricingParity(t *testing.T) {
	for _, row := range []struct {
		name, body, model, resolution string
		ppm, price, want              int64
	}{
		{"720", `{"prompt":"cat","seconds":"8","size":"1280x720"}`, "veo-3.1-fast-generate-preview", "720p", 1_000_000, 10_000_002, 80_000_016},
		{"fast4k", `{"prompt":"cat","seconds":8,"size":"3840x2160"}`, "veo-3.1-fast-generate-preview", "4k", 2_333_333, 10_000_002, 186_666_678},
		{"generate4k", `{"prompt":"cat","duration":8,"size":"4k"}`, "veo-3.1-generate-preview", "4k", 1_500_000, 10_000_002, 120_000_024},
		{"metadataWins", `{"prompt":"cat","seconds":4,"size":"4k","metadata":{"durationSeconds":8,"resolution":"720P"}}`, "veo-3.1-fast-generate-preview", "720p", 1_000_000, 10_000_002, 80_000_016},
		{"multipartBillingView", `{"prompt":"cat","seconds":"4","metadata":"{\"durationSeconds\":8,\"resolution\":\"4K\"}"}`, "veo-3.1-fast-generate-preview", "4k", 2_333_333, 2, 38},
		{"native", `{"instances":[{"prompt":"cat"}],"parameters":{"durationSeconds":8,"resolution":"4k"}}`, "veo-3.1-fast-generate-preview", "4k", 2_333_333, 2, 38},
		{"default8", `{"prompt":"cat","size":"4k"}`, "veo-3.1-generate-preview", "4k", 1_500_000, 2, 24},
		{"veo3", `{"prompt":"cat","seconds":8,"size":"4k"}`, "veo-3.0-generate-001", "4k", 1_000_000, 2, 16},
	} {
		t.Run(row.name, func(t *testing.T) {
			req, snapshot := veoPriceFixture(row.body, row.model, row.price)
			payload, err := gemini.Payload(native.Submit{Model: req.Model, Body: req.Body})
			var outgoing struct {
				Parameters map[string]any `json:"parameters"`
			}
			if err != nil || json.Unmarshal(payload, &outgoing) != nil || outgoing.Parameters["resolution"] != row.resolution || outgoing.Parameters["durationSeconds"] != float64(8) {
				t.Fatalf("native params=%s err=%v", payload, err)
			}
			s := &Settler{}
			frozen, price, factor, err := s.freezeTargetPrices(req, snapshot)
			if err != nil || price.Rules["veo_resolution_multiplier_ppm"] != row.ppm {
				t.Fatalf("frozen=%#v %v", price, err)
			}
			amount, err := s.estimateTargetPrices(req, frozen, price, factor, pricing.RequestInput{}, nil, nil)
			if err != nil || amount != credits.Micro(row.want) {
				t.Fatalf("admission=%d %v want%d", amount, err, row.want)
			}
			result, err := gemini.ParseResult([]byte(`{"name":"models/veo/operations/task","done":true,"response":{"videos":[{"uri":"https://video.test/movie","durationSeconds":8}]}}`), "")
			if err != nil || result.Status != "completed" || result.Units != 8 {
				t.Fatalf("native result=%+v %v", result, err)
			}
			usage, err := pricing.WithMediaUnits(result.Usage, "video_second", "8")
			if err != nil {
				t.Fatal(err)
			}
			h := &hold{price: price, multiplier: factor, targetPrices: frozen}
			actual, err := s.settlementPrice(h, gateway.Outcome{Charge: true, Target: &req.Targets[0], Usage: usage})
			if err != nil || actual != amount || usage.VideoDurationMicros != 8_000_000 {
				t.Fatalf("actual=%d %v usage=%+v", actual, err, usage)
			}
			if _, exists := snapshot.Prices[req.Model].Rules["veo_resolution_multiplier_ppm"]; exists {
				t.Fatal("admission mutated published catalog price")
			}
		})
	}
}

func TestVeoInvalidParametersAndOverflowRefuseAdmission(t *testing.T) {
	for _, body := range []string{
		`{"size":"garbage"}`, `{"size":"3840x0"}`, `{"size":"3840x2160junk"}`, `{"size":"999999999999999999999x1"}`,
		`{"metadata":{"resolution":"8k"}}`, `{"metadata":{"resolution":4}}`, `{"metadata":[]}`, `{"seconds":1e30}`,
	} {
		req, snapshot := veoPriceFixture(body, "veo-3.1-fast-generate-preview", 2)
		if err := (&Settler{snapshot: func() *catalog.Snapshot { return snapshot }}).Reserve(context.Background(), req); err == nil {
			t.Fatalf("invalid request reached Redis: %s", body)
		}
	}
	req, snapshot := veoPriceFixture(`{"seconds":8,"size":"4k"}`, "veo-3.1-fast-generate-preview", math.MaxInt64)
	s := &Settler{cfg: Config{}.withDefaults(), snapshot: func() *catalog.Snapshot { return snapshot }}
	frozen, price, factor, err := s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.estimateTargetPrices(req, frozen, price, factor, pricing.RequestInput{}, nil, nil); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow=%v", err)
	}
	if err := s.Reserve(context.Background(), req); err == nil {
		t.Fatal("overflow reached Redis")
	}
}

func TestVeoOtherMediaAndPerCallUnchanged(t *testing.T) {
	req, snapshot := veoPriceFixture(`{"seconds":8,"size":"4k"}`, "other-video", 2)
	req.Targets[0].Provider = "doubao_video"
	s := &Settler{}
	frozen, price, factor, err := s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.estimateTargetPrices(req, frozen, price, factor, pricing.RequestInput{}, nil, nil)
	if err != nil || got != 16 {
		t.Fatalf("ordinary video=%d %v", got, err)
	}
	p := snapshot.Prices[req.Model]
	p.Rules = map[string]any{"money_quantum": 2}
	snapshot.Prices[req.Model] = p
	req.Targets[0].Provider, req.Targets[0].UpstreamModel = "gemini", "veo-3.1-fast-generate-preview"
	frozen, price, factor, err = s.freezeTargetPrices(req, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.estimateTargetPrices(req, frozen, price, factor, pricing.RequestInput{}, nil, nil)
	if err != nil || got != 2 {
		t.Fatalf("per-call task=%d %v", got, err)
	}
}
