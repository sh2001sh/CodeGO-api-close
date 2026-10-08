package gateway

import (
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// ModelDiscoveryConfig uses the same current identity and route policies as
// generation. Snapshot and Planner must be served from memory, never SQL.
type ModelDiscoveryConfig struct {
	Authorizer     Authorizer
	Planner        Planner
	Snapshot       func() *catalog.Snapshot
	TrustedProxies []netip.Prefix
	AuthFailures   AuthFailureController
}

// ModelDiscovery serves only models the caller can currently route to.
type ModelDiscovery struct{ cfg ModelDiscoveryConfig }

func NewModelDiscovery(cfg ModelDiscoveryConfig) (*ModelDiscovery, error) {
	if cfg.Authorizer == nil || cfg.Planner == nil || cfg.Snapshot == nil {
		return nil, errors.New("gateway: model discovery requires authorizer, planner and snapshot")
	}
	return &ModelDiscovery{cfg: cfg}, nil
}

func (d *ModelDiscovery) Register(mux *http.ServeMux) {
	for _, prefix := range []string{"/v1/models", "/v1beta/models", "/v1beta/openai/models"} {
		mux.HandleFunc("GET "+prefix, d.serve)
		mux.HandleFunc("GET "+prefix+"/{model}", d.serve)
	}
}

func (d *ModelDiscovery) serve(w http.ResponseWriter, r *http.Request) {
	protocol := discoveryProtocol(r)
	w.Header().Set("X-Request-Id", newRequestID())
	w.Header().Set("Cache-Control", "no-store")
	principal, err := d.authorize(r)
	if err != nil {
		writeProtocolError(w, protocol, err.status, err.typ, err.code, err.message)
		return
	}
	snapshot := d.cfg.Snapshot()
	if snapshot == nil {
		writeProtocolError(w, protocol, http.StatusServiceUnavailable, "api_error", "catalog_unavailable", "model catalog is temporarily unavailable")
		return
	}
	candidates := discoveryCandidates(snapshot)
	if model := r.PathValue("model"); model != "" {
		if _, known := candidates[model]; known {
			if item, ok := d.visible(r, snapshot, principal, model); ok {
				discoveryJSON(w, discoveryItem(protocol, item))
				return
			}
		}
		writeProtocolError(w, protocol, http.StatusNotFound, "invalid_request_error", "model_not_found", "model does not exist or is not available to this API key")
		return
	}
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	slices.Sort(names)
	models := make([]discoveryModel, 0, len(names))
	for _, name := range names {
		if item, ok := d.visible(r, snapshot, principal, name); ok {
			models = append(models, item)
		}
	}
	discoveryJSON(w, discoveryList(protocol, models))
}

func (d *ModelDiscovery) authorize(r *http.Request) (Principal, *clientError) {
	address := (&Gateway{cfg: Config{TrustedProxies: d.cfg.TrustedProxies}}).clientAddress(r)
	if d.cfg.AuthFailures != nil && d.cfg.AuthFailures.Blocked(address) {
		return Principal{}, errAuthLimited
	}
	key := apiKeyFrom(r)
	if key == "" {
		d.failedAuth(address)
		return Principal{}, errMissingKey
	}
	principal, err := d.cfg.Authorizer.Authorize(r.Context(), key)
	if errors.Is(err, ErrAuthUnavailable) {
		return Principal{}, errAuthDown
	}
	if err != nil {
		d.failedAuth(address)
		return Principal{}, errInvalidKey
	}
	principal, err = requestedPrincipal(principal, r)
	if err != nil {
		var refusal *UpstreamError
		if errors.As(err, &refusal) {
			return Principal{}, &clientError{refusal.Status, refusal.Type, refusal.Code, refusal.Message}
		}
		return Principal{}, &clientError{http.StatusForbidden, "permission_error", "group_not_allowed", "API key does not allow this group"}
	}
	if err := ValidateRequestPolicy(principal, "", r, d.cfg.TrustedProxies); err != nil {
		var policy *UpstreamError
		if errors.As(err, &policy) {
			return Principal{}, &clientError{policy.Status, policy.Type, policy.Code, policy.Message}
		}
		return Principal{}, &clientError{http.StatusForbidden, "permission_error", "request_not_permitted", "API key does not permit this request"}
	}
	return principal, nil
}

func (d *ModelDiscovery) failedAuth(address string) {
	if d.cfg.AuthFailures != nil {
		d.cfg.AuthFailures.Failed(address)
	}
}

func (d *ModelDiscovery) visible(r *http.Request, snap *catalog.Snapshot, principal Principal, model string) (discoveryModel, bool) {
	if ValidateRequestPolicy(principal, model, r, d.cfg.TrustedProxies) != nil {
		return discoveryModel{}, false
	}
	// V2's explicit zero-hour model list excludes image-generation models.
	if principal.Group == "zero-hour" && discoveryImageModel(model) {
		return discoveryModel{}, false
	}
	// A metadata read must never reserve funds, consume a concurrency slot or
	// send an upstream request. Plan alone applies group/card/market policies.
	targets, err := d.cfg.Planner.Plan(r.Context(), &Request{Protocol: ProtocolOpenAIChat, Model: model, Principal: principal})
	if err != nil || len(targets) == 0 {
		return discoveryModel{}, false
	}
	for _, target := range targets {
		_, priced := snap.Prices[model]
		if market, exists := snap.Market.Channels[target.ChannelID]; exists && len(market.ModelPrices) > 0 {
			_, priced = market.ModelPrices[model]
		}
		if !priced {
			return discoveryModel{}, false
		}
	}
	item := discoveryModel{ID: model, Object: "model", Created: 1626777600, OwnedBy: targets[0].Provider}
	enrichDiscoveryMetadata(&item, snap.Metadata)
	return item, true
}

func discoveryCandidates(snap *catalog.Snapshot) map[string]struct{} {
	models := make(map[string]struct{}, len(snap.Prices))
	add := func(name string) {
		if name != "" && name != "*" && strings.TrimSpace(name) == name {
			models[name] = struct{}{}
		}
	}
	for _, routes := range snap.Routes {
		for model := range routes {
			add(model)
		}
	}
	// A wildcard route can serve named priced models, including market prices.
	for model := range snap.Prices {
		add(model)
	}
	for _, policy := range snap.Market.Channels {
		for model := range policy.ModelPrices {
			add(model)
		}
	}
	return models
}

func discoveryImageModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range []string{"dall-e-", "gpt-image-", "imagen-", "grok-imagine-image", "grok-2-image"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	for _, fragment := range []string{"flux-", "flux.1-", "image-generation"} {
		if strings.Contains(model, fragment) {
			return true
		}
	}
	return false
}
