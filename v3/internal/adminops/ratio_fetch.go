package adminops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Server) fetchRatio(ctx context.Context, up UpstreamDTO, timeout int) ratioSyncUpstreamResult {
	name := up.Name
	if up.ID != 0 {
		name = fmt.Sprintf("%s(%d)", up.Name, up.ID)
	}
	result := ratioSyncUpstreamResult{Name: name}
	raw, err := ratioURL(up)
	if err != nil {
		result.Err = "Invalid upstream URL"
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		result.Err = "Invalid upstream request"
		return result
	}
	if up.Endpoint == "openrouter" {
		// Credential access is tied to the configured channel origin, preventing an arbitrary URL from receiving its secret.
		var base string
		var ciphertext []byte
		err = s.pool.QueryRow(ctx, `SELECT CASE WHEN c.provider='openrouter' AND c.base_url='' THEN 'https://openrouter.ai/api' ELSE c.base_url END,k.secret FROM v3_catalog.channels c JOIN v3_catalog.channel_credentials k ON k.channel_id=c.id AND k.status='enabled' AND k.kind='api_key' WHERE c.id=$1 AND (k.expires_at IS NULL OR k.expires_at>now()) ORDER BY k.id LIMIT 1`, up.ID).Scan(&base, &ciphertext)
		expected, _ := url.Parse(base)
		actual, _ := url.Parse(raw)
		if err != nil || s.crypto == nil || expected == nil || actual.Scheme != expected.Scheme || actual.Host != expected.Host {
			result.Err = "OpenRouter requires a configured channel credential and matching origin"
			return result
		}
		key, e := s.crypto.Decrypt(ciphertext)
		if e != nil {
			result.Err = "Channel credential unavailable"
			return result
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(key)))
	}
	client := *s.cfg.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		result.Err = "Upstream request failed or timed out"
		return result
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		result.Err = fmt.Sprintf("Upstream returned HTTP %d", resp.StatusCode)
		return result
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRatioConfigBytes+1))
	if err != nil || len(data) > maxRatioConfigBytes {
		result.Err = "Upstream response could not be read or exceeds size limit"
		return result
	}
	converted, err := decodeRatio(data, up.Endpoint == "openrouter", isModelsDevAPIEndpoint(raw))
	if err != nil {
		result.Err = "Upstream returned invalid pricing data"
		return result
	}
	result.Data = converted
	return result
}
func decodeRatio(data []byte, openrouter, modelsdev bool) (map[string]any, error) {
	if openrouter {
		out, err := convertOpenRouterToRatioData(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return validateRatioFields(out)
	}
	if modelsdev {
		out, err := convertModelsDevToRatioData(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return validateRatioFields(out)
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || !envelope.Success {
		return nil, errors.New("upstream rejected pricing request")
	}
	var obj map[string]any
	if json.Unmarshal(envelope.Data, &obj) == nil {
		for _, field := range pricingSyncFields {
			if valueMap(obj[field]) != nil {
				return validateRatioFields(obj)
			}
		}
	}
	var items []pricingItem
	if json.Unmarshal(envelope.Data, &items) != nil || len(items) == 0 {
		return nil, errors.New("invalid pricing items")
	}
	return validateRatioFields(convertPricingItemsToRatioData(items))
}
func validateRatioFields(data map[string]any) (map[string]any, error) {
	out := make(map[string]any)
	for _, field := range pricingSyncFields {
		if raw, ok := data[field]; ok {
			values := valueMap(raw)
			if values == nil {
				return nil, errors.New("invalid ratio map")
			}
			for model, v := range values {
				if model == "" || len(model) > 255 {
					return nil, errors.New("invalid model")
				}
				if numericPricingSyncFields[field] {
					n, ok := asFloat64(v)
					if !ok || n < 0 {
						return nil, errors.New("invalid numeric ratio")
					}
				} else {
					if _, ok := v.(string); !ok {
						return nil, errors.New("invalid billing string")
					}
				}
			}
			out[field] = values
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty pricing data")
	}
	return out, nil
}
func (s *Server) localRatioData(ctx context.Context) (map[string]any, error) {
	rows, err := s.pool.Query(ctx, `SELECT model,mode,input_per_mtok,output_per_mtok,cache_read_per_mtok,cache_write_per_mtok,per_request,rules FROM v3_catalog.model_prices ORDER BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]any{}
	for _, field := range pricingSyncFields {
		out[field] = map[string]any{}
	}
	for rows.Next() {
		var model, mode string
		var input, output, read, write, per int64
		var rawRules []byte
		if err = rows.Scan(&model, &mode, &input, &output, &read, &write, &per, &rawRules); err != nil {
			return nil, err
		}
		var rules map[string]any
		decoder := json.NewDecoder(bytes.NewReader(rawRules))
		decoder.UseNumber()
		if err = decoder.Decode(&rules); err != nil {
			return nil, err
		}
		if mode == "expression" {
			out["billing_mode"].(map[string]any)[model] = "tiered_expr"
			source, _ := rules["expression"].(string)
			if source == "" {
				source, _ = rules["expr"].(string)
			}
			out["billing_expr"].(map[string]any)[model] = source
		}
		for _, field := range []string{"image_ratio", "audio_ratio", "audio_completion_ratio"} {
			if value, ok := rules[field]; ok {
				out[field].(map[string]any)[model] = value
			}
		}
		if mode == "per_request" {
			out["model_price"].(map[string]any)[model] = float64(per) / 1000000
		} else {
			out["model_ratio"].(map[string]any)[model] = float64(input) / 2000000
			if input > 0 {
				out["completion_ratio"].(map[string]any)[model] = float64(output) / float64(input)
				out["cache_ratio"].(map[string]any)[model] = float64(read) / float64(input)
				out["create_cache_ratio"].(map[string]any)[model] = float64(write) / float64(input)
			}
		}
	}
	return out, rows.Err()
}
