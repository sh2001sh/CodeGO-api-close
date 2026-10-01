package catalogcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (s *Server) legacyFetchModels(w http.ResponseWriter, r *http.Request) {
	var target gateway.Target
	if r.Method == http.MethodPost {
		var input struct {
			BaseURL string `json:"base_url"`
			Type    int    `json:"type"`
			Key     string `json:"key"`
		}
		if !decode(w, r, &input) {
			return
		}
		provider := legacyProviderIDs()[input.Type]
		if provider == "" || len(input.Key) > 131072 {
			fail(w, 400, "invalid_provider", "Invalid provider or credential")
			return
		}
		target = gateway.Target{Provider: provider, BaseURL: input.BaseURL, Secret: strings.TrimSpace(strings.Split(input.Key, "\n")[0])}
	} else {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		_, t, err := s.loadProbeTarget(r.Context(), id)
		if err != nil {
			s.dbError(w, err)
			return
		}
		target = t
	}
	models, err := fetchProbeModels(r.Context(), target)
	if err != nil {
		fail(w, 502, "upstream_models_failed", err.Error())
		return
	}
	respond(w, 200, models)
}

// probeModelsURL resolves the provider-specific model listing endpoint,
// collapsing a duplicated /v1 or /v1beta base path segment.
func probeModelsURL(target gateway.Target) (*url.URL, error) {
	base := probeBaseURL(target)
	u, err := validateProbeURL(base, false)
	if err != nil {
		return nil, err
	}
	path := "/v1/models"
	switch target.Provider {
	case "ollama":
		path = "/api/tags"
	case "gemini":
		path = "/v1beta/models"
	}
	basePath := strings.TrimRight(u.Path, "/")
	for _, prefix := range []string{"/v1", "/v1beta"} {
		if strings.HasSuffix(basePath, prefix) && strings.HasPrefix(path, prefix+"/") {
			path = strings.TrimPrefix(path, prefix)
		}
	}
	u.Path, u.RawPath = basePath+path, ""
	return u, nil
}

// buildProbeModelsRequest builds the authenticated GET request used to list
// upstream models for target.
func buildProbeModelsRequest(ctx context.Context, target gateway.Target, u *url.URL) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errProbeURL
	}
	switch target.Provider {
	case "gemini":
		request.Header.Set("x-goog-api-key", target.Secret)
	case "claude", "anthropic":
		request.Header.Set("x-api-key", target.Secret)
		request.Header.Set("anthropic-version", "2023-06-01")
	default:
		if target.Secret != "" {
			request.Header.Set("Authorization", "Bearer "+target.Secret)
		}
	}
	for name, value := range target.HeaderOverride {
		request.Header.Set(name, value)
	}
	return request, nil
}

// probeModelsResponse is the shape of the two model-listing response formats
// handled: OpenAI-style {"data":[...]} and ollama/gemini-style {"models":[...]}.
type probeModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// parseProbeModelsBody decodes and validates the upstream model listing body
// for target, returning the sorted, deduplicated list of model identifiers.
func parseProbeModelsBody(target gateway.Target, data []byte) ([]string, error) {
	var result probeModelsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.New("invalid upstream model response")
	}
	if target.Provider != "ollama" && target.Provider != "gemini" && result.Data == nil {
		return nil, errors.New("upstream response does not contain models")
	}
	if (target.Provider == "ollama" || target.Provider == "gemini") && result.Models == nil {
		return nil, errors.New("upstream response does not contain models")
	}
	set := make(map[string]bool)
	for _, model := range result.Data {
		set[model.ID] = true
	}
	for _, model := range result.Models {
		set[strings.TrimPrefix(model.Name, "models/")] = true
	}
	models := make([]string, 0, len(set))
	for id := range set {
		if id == "" || len(id) > 255 || strings.ContainsAny(id, "\r\n\x00") || (target.Secret != "" && strings.Contains(id, target.Secret)) {
			return nil, errors.New("invalid upstream model identifier")
		}
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}

func fetchProbeModels(parent context.Context, target gateway.Target) ([]string, error) {
	u, err := probeModelsURL(target)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	request, err := buildProbeModelsRequest(ctx, target, u)
	if err != nil {
		return nil, err
	}
	client, closeIdle, err := probeClient(target.ProxyURL)
	if err != nil {
		return nil, err
	}
	defer closeIdle()
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("upstream model discovery failed or timed out")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("upstream rejected model discovery")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, probeBodyLimit+1))
	if err != nil || len(data) > probeBodyLimit {
		return nil, errors.New("invalid or oversized upstream model response")
	}
	return parseProbeModelsBody(target, data)
}

func (s *Server) legacyListModels(w http.ResponseWriter, r *http.Request) {
	enabled := strings.HasSuffix(r.URL.Path, "/models_enabled")
	rows, err := s.pool.Query(r.Context(), `SELECT DISTINCT cm.model FROM v3_catalog.channel_models cm JOIN v3_catalog.channels c ON c.id=cm.channel_id WHERE NOT $1::boolean OR (c.status='enabled' AND EXISTS(SELECT 1 FROM v3_catalog.channel_credentials cr WHERE cr.channel_id=c.id AND cr.status='enabled' AND (cr.expires_at IS NULL OR cr.expires_at>now()))) ORDER BY cm.model`, enabled)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	names := make([]string, 0)
	items := make([]map[string]any, 0)
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			s.dbError(w, err)
			return
		}
		names = append(names, name)
		items = append(items, map[string]any{"id": name, "object": "model", "created": 0, "owned_by": "catalog"})
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	if enabled {
		respond(w, 200, names)
	} else {
		respond(w, 200, items)
	}
}
