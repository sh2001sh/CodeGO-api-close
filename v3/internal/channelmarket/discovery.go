package channelmarket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type FetchModelsRequest struct {
	Provider string `json:"provider_type"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
}

func FetchModels(ctx context.Context, input FetchModelsRequest) ([]string, error) {
	r := CreateRequest{Provider: input.Provider, BaseURL: input.BaseURL, APIKey: input.APIKey, Models: []string{"validation"}}
	if _, err := r.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := buildModelsListRequest(ctx, r)
	if err != nil {
		return nil, err
	}
	body, err := sendModelsListRequest(req)
	if err != nil {
		return nil, err
	}
	return parseModelsListResponse(body)
}

// buildModelsListRequest builds the provider-specific GET request used to
// list upstream models.
func buildModelsListRequest(ctx context.Context, r CreateRequest) (*http.Request, error) {
	secret, err := upstreamSecret(r.APIKey)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(r.BaseURL, "/")
	path := "/v1/models"
	if strings.HasSuffix(base, "/v1") || strings.HasSuffix(base, "/v1beta") {
		path = "/models"
	} else if r.Provider == "gemini" {
		path = "/v1beta/models"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	switch nativeProvider(r.Provider) {
	case "gemini":
		req.Header.Set("x-goog-api-key", secret)
	case "anthropic", "claude":
		req.Header.Set("x-api-key", secret)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "azure":
		req.Header.Set("api-key", secret)
	default:
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	return req, nil
}

// sendModelsListRequest issues req over a pinned-DNS transport and returns
// the (size-capped) response body.
func sendModelsListRequest(req *http.Request) ([]byte, error) {
	transport := &http.Transport{DialContext: PublicDialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return nil, ErrInvalid
	}
	return body, nil
}

// parseModelsListResponse decodes an OpenAI- or Gemini-shaped models listing
// into a deduplicated, sorted slice of model names.
func parseModelsListResponse(body []byte) ([]string, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	models := []string{}
	add := func(raw string) {
		model := strings.TrimSpace(raw)
		if model != "" && len(model) <= 255 && !seen[model] {
			seen[model] = true
			models = append(models, model)
		}
	}
	for _, m := range payload.Data {
		add(m.ID)
	}
	for _, m := range payload.Models {
		add(strings.TrimPrefix(m.Name, "models/"))
	}
	if len(models) == 0 || len(models) > 1000 {
		return nil, ErrInvalid
	}
	sort.Strings(models)
	return models, nil
}
func (s *Service) httpFetchModels(w http.ResponseWriter, r *http.Request, _ Actor) {
	var input FetchModelsRequest
	if !decode(w, r, &input) {
		return
	}
	models, err := FetchModels(r.Context(), input)
	s.result(w, models, err)
}
