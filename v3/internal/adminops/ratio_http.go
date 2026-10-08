package adminops

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
)

type UpstreamDTO struct {
	ID       int64  `json:"id,omitempty"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Endpoint string `json:"endpoint"`
}
type UpstreamRequest struct {
	ChannelIDs []int64       `json:"channel_ids"`
	Upstreams  []UpstreamDTO `json:"upstreams"`
	Timeout    int           `json:"timeout"`
}
type TestResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
type DifferenceItem struct {
	Current    any             `json:"current"`
	Upstreams  map[string]any  `json:"upstreams"`
	Confidence map[string]bool `json:"confidence"`
}
type SyncableChannel struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Status  int    `json:"status"`
	Type    int    `json:"type"`
}

func (s *Server) registerRatio(mux *http.ServeMux, auth Authenticate) {
	mux.HandleFunc("GET /api/ratio_sync/channels", s.protected(auth, "root", s.ratioChannelsHTTP))
	mux.HandleFunc("POST /api/ratio_sync/fetch", s.protected(auth, "root", s.ratioFetchHTTP))
	mux.HandleFunc("POST /api/ratio_sync/apply", s.protected(auth, "root", s.ratioApplyHTTP))
}
func (s *Server) ratioChannelsHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	rows, err := s.pool.Query(r.Context(), `SELECT id,name,CASE WHEN provider='openrouter' AND base_url='' THEN 'https://openrouter.ai/api' ELSE base_url END,CASE status WHEN 'enabled' THEN 1 WHEN 'disabled' THEN 2 ELSE 3 END,CASE WHEN provider='openrouter' THEN 20 ELSE 0 END FROM v3_catalog.channels WHERE base_url<>'' OR provider='openrouter' ORDER BY id`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	items := make([]SyncableChannel, 0)
	for rows.Next() {
		var in SyncableChannel
		if err = rows.Scan(&in.ID, &in.Name, &in.BaseURL, &in.Status, &in.Type); err != nil {
			s.dbError(w, err)
			return
		}
		items = append(items, in)
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	items = append(items, SyncableChannel{ID: officialRatioPresetID, Name: officialRatioPresetName, BaseURL: officialRatioPresetBaseURL, Status: 1}, SyncableChannel{ID: modelsDevPresetID, Name: modelsDevPresetName, BaseURL: modelsDevPresetBaseURL, Status: 1})
	respond(w, items)
}
func (s *Server) ratioFetchHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	var in UpstreamRequest
	if !decode(w, r, &in) {
		return
	}
	if in.Timeout == 0 {
		in.Timeout = 10
	}
	if in.Timeout < 1 || in.Timeout > 60 || len(in.ChannelIDs)+len(in.Upstreams) > 100 || len(in.ChannelIDs)+len(in.Upstreams) == 0 {
		fail(w, 400, "invalid_upstream", "Select 1–100 upstreams with timeout 1–60 seconds")
		return
	}
	ups, err := s.collectUpstreams(r.Context(), in)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if len(ups) == 0 {
		fail(w, 400, "invalid_upstream", "No valid upstreams selected")
		return
	}
	for _, up := range ups {
		if _, e := ratioURL(up); e != nil {
			fail(w, 400, "invalid_upstream", "Invalid upstream URL or endpoint")
			return
		}
	}
	local, err := s.localRatioData(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	sem := make(chan struct{}, maxConcurrentFetches)
	results := make([]ratioSyncUpstreamResult, len(ups))
	var wg sync.WaitGroup
	for i, up := range ups {
		wg.Add(1)
		go func(i int, up UpstreamDTO) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-r.Context().Done():
				results[i] = ratioSyncUpstreamResult{Name: up.Name, Err: "Upstream request cancelled"}
				return
			}
			defer func() { <-sem }()
			results[i] = s.fetchRatio(r.Context(), up, in.Timeout)
		}(i, up)
	}
	wg.Wait()
	tests := make([]TestResult, 0, len(ups))
	channels := make([]ratioSyncChannelData, 0, len(ups))
	for _, v := range results {
		if v.Err != "" {
			tests = append(tests, TestResult{Name: v.Name, Status: "error", Error: v.Err})
		} else {
			tests = append(tests, TestResult{Name: v.Name, Status: "success"})
			channels = append(channels, ratioSyncChannelData{name: v.Name, data: v.Data})
		}
	}
	respond(w, RatioSyncFetchResult{Differences: buildDifferences(local, channels), TestResults: tests})
}
func (s *Server) collectUpstreams(ctx context.Context, in UpstreamRequest) ([]UpstreamDTO, error) {
	if len(in.Upstreams) > 0 {
		return in.Upstreams, nil
	}
	ups := make([]UpstreamDTO, 0, len(in.ChannelIDs))
	seen := map[int64]bool{}
	for _, id := range in.ChannelIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		switch id {
		case officialRatioPresetID:
			ups = append(ups, UpstreamDTO{ID: id, Name: officialRatioPresetName, BaseURL: officialRatioPresetBaseURL, Endpoint: officialRatioPresetEndpoint})
		case modelsDevPresetID:
			ups = append(ups, UpstreamDTO{ID: id, Name: modelsDevPresetName, BaseURL: modelsDevPresetBaseURL, Endpoint: "/api.json"})
		default:
			var up UpstreamDTO
			if err := s.pool.QueryRow(ctx, `SELECT id,name,CASE WHEN provider='openrouter' AND base_url='' THEN 'https://openrouter.ai/api' ELSE base_url END,CASE WHEN provider='openrouter' THEN 'openrouter' ELSE '' END FROM v3_catalog.channels WHERE id=$1`, id).Scan(&up.ID, &up.Name, &up.BaseURL, &up.Endpoint); err != nil {
				return nil, err
			}
			ups = append(ups, up)
		}
	}
	return ups, nil
}
func ratioURL(up UpstreamDTO) (string, error) {
	if strings.TrimSpace(up.Name) == "" || len(up.Name) > 255 {
		return "", errors.New("invalid name")
	}
	endpoint := up.Endpoint
	if endpoint == "openrouter" {
		endpoint = "/v1/models"
	}
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	raw := strings.TrimRight(up.BaseURL, "/") + "/" + strings.TrimLeft(endpoint, "/")
	if strings.HasPrefix(endpoint, "https://") || strings.HasPrefix(endpoint, "http://") {
		raw = endpoint
	}
	u, err := validateToolURL(raw)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
