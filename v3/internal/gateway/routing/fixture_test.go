package routing

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// chanSpec describes one test channel: id, priority, weight and how many
// credentials it has (credential ids are id*100+i).
type chanSpec struct {
	id, priority, weight, creds int
	mapping                     map[string]string
}

func snapshotOf(strategy string, specs ...chanSpec) *catalog.Snapshot {
	snap := &catalog.Snapshot{
		Version:  1,
		Channels: map[int64]*catalog.Channel{},
		Routes:   map[string]map[string][]catalog.Route{"default": {}},
	}
	var routes []catalog.Route
	for _, s := range specs {
		ch := &catalog.Channel{ID: int64(s.id), Provider: "openai", BaseURL: "http://up", ModelMapping: s.mapping}
		for i := 0; i < s.creds; i++ {
			ch.Credentials = append(ch.Credentials, catalog.Credential{ID: int64(s.id*100 + i), ChannelID: int64(s.id), Secret: "s"})
		}
		snap.Channels[ch.ID] = ch
		routes = append(routes, catalog.Route{ChannelID: ch.ID, Priority: s.priority, Weight: s.weight, Strategy: strategy})
	}
	snap.Routes["default"]["gpt"] = routes
	return snap
}

// fakeClock is advanced manually so cooldown tests never sleep.
type fakeClock struct{ ns atomic.Int64 }

func newClock() *fakeClock {
	c := &fakeClock{}
	c.ns.Store(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC).UnixNano())
	return c
}

func (c *fakeClock) now() time.Time          { return time.Unix(0, c.ns.Load()) }
func (c *fakeClock) advance(d time.Duration) { c.ns.Add(int64(d)) }

type env struct {
	planner *Planner
	clock   *fakeClock
	mu      sync.Mutex
	snap    *catalog.Snapshot
}

func newEnv(snap *catalog.Snapshot, cfg Config) *env {
	e := &env{clock: newClock(), snap: snap}
	cfg.Now = e.clock.now
	e.planner = New(e.current, cfg)
	return e
}

func (e *env) current() *catalog.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snap
}

func (e *env) swap(s *catalog.Snapshot) {
	e.mu.Lock()
	e.snap = s
	e.mu.Unlock()
}

func request(body string) *gateway.Request {
	if body == "" {
		body = `{"model":"gpt"}`
	}
	return &gateway.Request{Model: "gpt", Body: []byte(body), Principal: gateway.Principal{UserID: 1, KeyID: 9, Group: "default"}}
}

func credIDs(ts []gateway.Target) []int64 {
	out := make([]int64, len(ts))
	for i, t := range ts {
		out[i] = t.CredentialID
	}
	return out
}

var (
	credFail   = gateway.AttemptResult{Retryable: true, Scope: gateway.ScopeCredential, Status: 500}
	modelFail  = gateway.AttemptResult{Retryable: true, Scope: gateway.ScopeModel, Status: 404}
	requestErr = gateway.AttemptResult{Scope: gateway.ScopeRequest, Status: 400}
	ok         = gateway.AttemptResult{OK: true, Status: 200}
)
