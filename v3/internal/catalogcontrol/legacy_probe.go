package catalogcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type probeResult struct {
	ID        int64   `json:"id,omitempty"`
	Success   bool    `json:"success"`
	Message   string  `json:"message"`
	Time      float64 `json:"time"`
	ErrorCode string  `json:"error_code,omitempty"`
}

func (s *Server) loadProbeTarget(ctx context.Context, id int64) (Channel, gateway.Target, error) {
	c, err := scanChannel(s.pool.QueryRow(ctx, channelSelect+` WHERE c.id=$1`, id))
	if err != nil {
		return c, gateway.Target{}, err
	}
	var encrypted []byte
	var credentialID int64
	var fingerprint catalog.CredentialFingerprint
	err = s.pool.QueryRow(ctx, `SELECT id,secret,fingerprint FROM v3_catalog.channel_credentials WHERE channel_id=$1 AND status='enabled' AND (expires_at IS NULL OR expires_at>now()) ORDER BY id LIMIT 1`, id).Scan(&credentialID, &encrypted, &fingerprint)
	if err != nil {
		return c, gateway.Target{}, err
	}
	decrypter, ok := s.enc.(catalog.Decrypter)
	if !ok {
		return c, gateway.Target{}, errors.New("credential decryption unavailable")
	}
	secret, err := decrypter.Decrypt(encrypted)
	if err != nil {
		return c, gateway.Target{}, errors.New("credential decryption failed")
	}
	target := gateway.Target{ChannelID: id, CredentialID: credentialID, Provider: c.Provider, BaseURL: c.BaseURL, ProxyURL: c.ProxyURL, Secret: string(secret), HeaderOverride: c.HeaderOverride,
		Fingerprint: gateway.CredentialFingerprint{UserAgent: fingerprint.UserAgent, TLSProfile: fingerprint.TLSProfile}}
	for _, item := range []struct {
		raw []byte
		dst any
	}{{c.Settings, &target.Settings}, {c.ParamOverride, &target.ParamOverride}} {
		if len(item.raw) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(item.raw))
			decoder.UseNumber()
			if decoder.Decode(item.dst) != nil {
				return c, target, errors.New("invalid channel configuration")
			}
		}
	}
	target.StatusCodeMapping, err = catalog.ParseStatusCodeMapping(c.StatusCodeMapping)
	if err != nil {
		return c, target, errors.New("invalid channel configuration")
	}
	return c, target, nil
}

func (s *Server) testStoredChannel(ctx context.Context, id int64, model, endpoint string, stream bool) probeResult {
	result := probeResult{ID: id}
	start := time.Now()
	c, target, err := s.loadProbeTarget(ctx, id)
	if err == nil {
		if model == "" && len(c.Models) > 0 {
			model = c.Models[0]
		}
		if mapped := c.ModelMapping[model]; mapped != "" {
			target.UpstreamModel = mapped
		} else {
			target.UpstreamModel = model
		}
		err = runChannelProbe(ctx, target, model, endpoint, stream)
	}
	result.Time = time.Since(start).Seconds()
	if err != nil {
		result.Message = "Channel test failed: no usable credential, invalid configuration, or upstream failure"
		result.ErrorCode = "channel_test_failed"
		return result
	}
	result.Success = true
	return result
}

func (s *Server) legacyTestChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	stream, err := strconv.ParseBool(r.URL.Query().Get("stream"))
	if r.URL.Query().Get("stream") == "" {
		err = nil
	}
	if err != nil || len(r.URL.Query().Get("model")) > 255 {
		fail(w, 400, "invalid_probe", "Invalid probe model or streaming flag")
		return
	}
	result := s.testStoredChannel(r.Context(), id, r.URL.Query().Get("model"), r.URL.Query().Get("endpoint_type"), stream)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) legacyTestChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT id FROM v3_catalog.channels WHERE status='enabled' ORDER BY id LIMIT 10001`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			s.dbError(w, err)
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	if len(ids) > 10000 {
		fail(w, 413, "too_many_channels", "Test channels individually when the catalog exceeds 10000 channels")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	results := make([]probeResult, len(ids))
	jobs := make(chan int, len(ids))
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(4, len(ids)) {
		wg.Go(func() {
			for i := range jobs {
				results[i] = s.testStoredChannel(ctx, ids[i], "", "", false)
			}
		})
	}
	wg.Wait()
	failed := 0
	for _, result := range results {
		if !result.Success {
			failed++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": failed == 0, "message": "", "data": results, "tested": len(results), "failed": failed})
}
