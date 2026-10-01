package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/openai_video"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type channelPolicyTransport func(*http.Request) (*http.Response, error)

func (f channelPolicyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type channelPolicySettler struct {
	inner         workflow.Settler
	t             *testing.T
	original      *gateway.Request
	finalizations int
}

func (s *channelPolicySettler) Reserve(ctx context.Context, req *gateway.Request) (workflow.Reservation, error) {
	s.t.Helper()
	copyReq := *req
	copyReq.Body = append([]byte(nil), req.Body...)
	copyReq.PricingHeaders = make(map[string]string, len(req.PricingHeaders))
	for key, value := range req.PricingHeaders {
		copyReq.PricingHeaders[key] = value
	}
	s.original = &copyReq
	return s.inner.Reserve(ctx, req)
}
func (s *channelPolicySettler) Finalize(ctx context.Context, req *gateway.Request, reservation workflow.Reservation, result native.Result) (credits.Micro, error) {
	s.t.Helper()
	s.finalizations++
	if req.ID != s.original.ID || req.Model != s.original.Model || string(req.Body) != string(s.original.Body) || !reflect.DeepEqual(req.PricingHeaders, s.original.PricingHeaders) || req.Targets[0].Group != s.original.Targets[0].Group {
		s.t.Error("durable settlement inputs changed by native request policies")
	}
	if result.Status != "completed" || result.Units != 4 {
		s.t.Errorf("actual result lost at settlement: %+v", result)
	}
	return s.inner.Finalize(ctx, req, reservation, result)
}

func TestWorkflowChannelPoliciesUseSelectedClientAcrossSubmitPollAndContent(t *testing.T) {
	var upstreamCalls, selections, transports, fallbacks atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.Header.Get("User-Agent") != "stable-video-client" || r.Header.Get("X-Frozen") != "original-header" || r.Header.Get("X-Policy") != "configured" {
			t.Error("configured fingerprint or frozen client/header policy absent on wire")
		}
		if r.Header.Get("Authorization") != "Bearer upstream-only" || r.Header.Get("Cookie") != "" {
			t.Error("client authentication leaked or upstream auth lost")
		}
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["model"] != "native-video" || body["prompt"] != "configured prompt" || body["seconds"] != float64(4) {
				t.Errorf("native body override not applied: %+v", body)
			}
		} else {
			body, err := io.ReadAll(r.Body)
			if err != nil || len(body) != 0 {
				t.Errorf("body override applied to GET: %q %v", body, err)
			}
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/videos":
			_, _ = io.WriteString(w, `{"id":"policy-native-id","status":"queued"}`)
		case "GET /v1/videos/policy-native-id":
			_, _ = io.WriteString(w, `{"id":"policy-native-id","status":"completed","seconds":4}`)
		case "GET /v1/videos/policy-native-id/content":
			_, _ = io.WriteString(w, "native-video-content")
		default:
			t.Errorf("unexpected native request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer upstream.Close()
	target := gateway.Target{ChannelID: 4, CredentialID: 9, Provider: "openai_video", BaseURL: upstream.URL, Secret: "upstream-only", UpstreamModel: "native-video", Group: "selected-group",
		Fingerprint:    gateway.CredentialFingerprint{UserAgent: "stable-video-client", TLSProfile: "chrome"},
		HeaderOverride: map[string]string{"X-Frozen": "{client_header:X-Frozen}", "X-Policy": "configured"}, StatusCodeMapping: map[string]int{"503": 200}}
	if err := json.Unmarshal([]byte(`{"operations":[{"mode":"set","path":"prompt","value":"configured prompt","conditions":{"original_model":"video","model":"native-video","user_id":11,"key_id":111,"user_group":"default","using_group":"selected-group","request_headers.x-frozen":"original-header"}},{"mode":"set","path":"seconds","value":4}]}`), &target.ParamOverride); err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	configured := &http.Client{Transport: channelPolicyTransport(func(req *http.Request) (*http.Response, error) {
		transports.Add(1)
		copyReq := req.Clone(req.Context())
		copyReq.Header.Set("User-Agent", "stable-video-client")
		return base.RoundTrip(copyReq)
	})}
	fallback := &http.Client{Transport: channelPolicyTransport(func(*http.Request) (*http.Response, error) {
		fallbacks.Add(1)
		return nil, errors.New("fallback forbidden")
	})}
	var billing *channelPolicySettler
	f := newAPIFixture(t, func(c *workflow.Config) {
		c.Planner = apiPlanner{target}
		c.Providers = map[string]native.Adapter{"openai_video": openai_video.New(fallback)}
		c.ResolveTarget = func(_ context.Context, channel, credential int64) (gateway.Target, error) {
			if channel != 4 || credential != 9 {
				t.Error("resolver lost durable channel/credential")
			}
			restored := target
			restored.Group = ""
			return restored, nil
		}
		c.Clients = func(_ context.Context, selected gateway.Target) (*http.Client, error) {
			selections.Add(1)
			if selected.ChannelID != 4 || selected.CredentialID != 9 || selected.Fingerprint != target.Fingerprint || selected.Group != "selected-group" {
				t.Error("client factory lost selected credential/fingerprint")
			}
			return configured, nil
		}
		billing = &channelPolicySettler{inner: c.Settler, t: t}
		c.Settler = billing
	})
	req := httptest.NewRequest("POST", "/v1/videos", strings.NewReader(`{"model":"video","prompt":"original prompt","seconds":8}`))
	req.Header.Set("Authorization", "Bearer owner")
	req.Header.Set("Cookie", "session=private")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Frozen", "original-header")
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("mapped submit = %d %s", w.Code, w.Body.String())
	}
	task := f.repo.one(t)
	if task.Model != "video" || task.PricingHeaders["X-Frozen"] != "original-header" || task.PricingHeaders["Authorization"] != "" || task.PricingHeaders["Cookie"] != "" {
		t.Fatalf("original model/pricing headers changed: %+v", task)
	}
	var frozen map[string]any
	if err := json.Unmarshal(task.Body, &frozen); err != nil {
		t.Fatal(err)
	}
	if frozen["model"] != "video" || frozen["prompt"] != "original prompt" || frozen["seconds"] != float64(8) || !reflect.DeepEqual(task.PricingHeaders, billing.original.PricingHeaders) {
		t.Fatalf("outgoing overrides changed frozen billing body: %+v", frozen)
	}
	if n, err := f.handler.Reconcile(context.Background(), 10); err != nil || n != 1 {
		t.Fatalf("mapped poll = %d %v", n, err)
	}
	w = f.request("GET", "/v1/videos/"+task.ID+"/content", "owner", "", "")
	if w.Code != 200 || w.Body.String() != "native-video-content" {
		t.Fatalf("mapped content = %d %s", w.Code, w.Body.String())
	}
	if selections.Load() != 3 || transports.Load() != 3 || upstreamCalls.Load() != 3 || fallbacks.Load() != 0 || billing.finalizations != 1 {
		t.Fatalf("selected native transport counts: selections=%d transports=%d upstream=%d fallback=%d settlements=%d", selections.Load(), transports.Load(), upstreamCalls.Load(), fallbacks.Load(), billing.finalizations)
	}
	stored := f.repo.one(t)
	if string(stored.Body) != string(task.Body) || stored.Model != task.Model || !reflect.DeepEqual(stored.PricingHeaders, task.PricingHeaders) {
		t.Fatal("poll/content mutated persisted billing inputs")
	}
}

func TestWorkflowConfiguredClientFailureDoesNotFallbackOrSendUpstream(t *testing.T) {
	for _, stage := range []string{"submit", "poll", "content"} {
		for _, nilClient := range []bool{false, true} {
			t.Run(stage+"/nil="+map[bool]string{true: "true", false: "false"}[nilClient], func(t *testing.T) {
				var upstreamCalls, selections, fallbacks atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					upstreamCalls.Add(1)
					if r.Method == http.MethodPost {
						_, _ = io.WriteString(w, `{"id":"failure-native","status":"queued"}`)
					} else {
						_, _ = io.WriteString(w, `{"id":"failure-native","status":"completed","seconds":4}`)
					}
				}))
				defer upstream.Close()
				target := gateway.Target{ChannelID: 4, CredentialID: 9, Provider: "openai_video", BaseURL: upstream.URL, Secret: "upstream-only", UpstreamModel: "native-video"}
				var failClient atomic.Bool
				failClient.Store(stage == "submit")
				fallback := &http.Client{Transport: channelPolicyTransport(func(*http.Request) (*http.Response, error) {
					fallbacks.Add(1)
					return nil, errors.New("fallback forbidden")
				})}
				f := newAPIFixture(t, func(c *workflow.Config) {
					c.Planner = apiPlanner{target}
					c.Providers = map[string]native.Adapter{"openai_video": openai_video.New(fallback)}
					c.ResolveTarget = func(context.Context, int64, int64) (gateway.Target, error) { return target, nil }
					c.Clients = func(context.Context, gateway.Target) (*http.Client, error) {
						selections.Add(1)
						if failClient.Load() {
							if nilClient {
								return nil, nil
							}
							return nil, errors.New("fingerprint client unavailable")
						}
						return upstream.Client(), nil
					}
				})
				w := f.request("POST", "/v1/videos", "owner", `{"model":"video"}`, "")
				task := f.repo.one(t)
				if stage == "submit" {
					if w.Code != 502 || task.Status != "submission_unknown" || task.CostState != "reserved" || len(f.settler.calls) != 0 {
						t.Fatalf("client failure acknowledged or refunded: %d %+v", w.Code, task)
					}
				} else {
					if w.Code != 200 {
						t.Fatalf("setup submit = %d %s", w.Code, w.Body.String())
					}
					if stage == "content" {
						if _, err := f.handler.Reconcile(context.Background(), 10); err != nil {
							t.Fatal(err)
						}
					}
					before := upstreamCalls.Load()
					failClient.Store(true)
					if stage == "poll" {
						if n, err := f.handler.Reconcile(context.Background(), 10); n != 0 || err == nil {
							t.Fatal("poll client selection failure swallowed")
						}
						if current := f.repo.one(t); current.CostState != "reserved" || current.LeaseID != "" || len(f.settler.calls) != 0 {
							t.Fatal("failed client poll changed hold or retained lease")
						}
					} else {
						w = f.request("GET", "/v1/videos/"+task.ID+"/content", "owner", "", "")
						if w.Code != 502 {
							t.Fatalf("failed content client = %d", w.Code)
						}
					}
					if upstreamCalls.Load() != before {
						t.Fatal("client selection failure still reached upstream")
					}
				}
				wantSelections := int32(1)
				switch stage {
				case "poll":
					wantSelections = 2
				case "content":
					wantSelections = 3
				}
				if selections.Load() != wantSelections || fallbacks.Load() != 0 || (stage == "submit" && upstreamCalls.Load() != 0) {
					t.Fatalf("factory failure fell back: selections=%d fallback=%d wire=%d", selections.Load(), fallbacks.Load(), upstreamCalls.Load())
				}
			})
		}
	}
}
