package catalogcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type metadataSyncSource struct {
	Locale     string `json:"locale"`
	ModelsURL  string `json:"models_url"`
	VendorsURL string `json:"vendors_url"`
}

type metadataUpstreamModel struct {
	ModelName   string          `json:"model_name"`
	Description string          `json:"description"`
	Icon        string          `json:"icon"`
	Tags        string          `json:"tags"`
	Endpoints   json.RawMessage `json:"endpoints"`
	VendorName  string          `json:"vendor_name"`
	NameRule    int             `json:"name_rule"`
	Status      int             `json:"status"`
}

type metadataUpstreamVendor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Status      int    `json:"status"`
}

func metadataSource(locale string) (metadataSyncSource, error) {
	base := strings.TrimRight(os.Getenv("SYNC_UPSTREAM_BASE"), "/")
	if base == "" {
		base = "https://basellm.github.io/llm-metadata"
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return metadataSyncSource{}, fmt.Errorf("invalid configured metadata source")
	}
	source := metadataSyncSource{Locale: locale}
	switch value := strings.ToLower(strings.TrimSpace(locale)); value {
	case "en", "zh-cn", "zh-tw", "ja":
		base += "/api/i18n/" + value
	default:
		base += "/api"
	}
	source.ModelsURL, source.VendorsURL = base+"/newapi/models.json", base+"/newapi/vendors.json"
	return source, nil
}

func fetchMetadataJSON[T any](ctx context.Context, client *http.Client, address string) ([]T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metadata source unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("metadata source returned HTTP %d", response.StatusCode)
	}
	const limit = 10 << 20
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(raw) > limit {
		return nil, fmt.Errorf("metadata response incomplete or oversized")
	}
	raw = bytes.TrimSpace(raw)
	var items []T
	if len(raw) > 0 && raw[0] == '[' {
		err = json.Unmarshal(raw, &items)
	} else {
		var envelope struct {
			Success *bool `json:"success"`
			Data    []T   `json:"data"`
		}
		err = json.Unmarshal(raw, &envelope)
		if err == nil && envelope.Success != nil && !*envelope.Success {
			return nil, fmt.Errorf("metadata source reported failure")
		}
		items = envelope.Data
	}
	if err != nil || items == nil {
		return nil, fmt.Errorf("invalid metadata source document")
	}
	return items, nil
}

func fetchMetadata(ctx context.Context, source metadataSyncSource) (map[string]metadataUpstreamModel, map[string]metadataUpstreamVendor, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	models, err := fetchMetadataJSON[metadataUpstreamModel](ctx, client, source.ModelsURL)
	if err != nil {
		return nil, nil, err
	}
	vendors, err := fetchMetadataJSON[metadataUpstreamVendor](ctx, client, source.VendorsURL)
	if err != nil {
		return nil, nil, err
	}
	modelMap, vendorMap := make(map[string]metadataUpstreamModel), make(map[string]metadataUpstreamVendor)
	for _, model := range models {
		if strings.TrimSpace(model.ModelName) == "" || model.NameRule < 0 || model.NameRule > 3 || model.Status < 0 || model.Status > math.MaxInt32 {
			return nil, nil, fmt.Errorf("invalid upstream model fields")
		}
		if _, duplicate := modelMap[model.ModelName]; duplicate {
			return nil, nil, fmt.Errorf("duplicate upstream model")
		}
		modelMap[model.ModelName] = model
	}
	for _, vendor := range vendors {
		if strings.TrimSpace(vendor.Name) == "" || vendor.Status < 0 || vendor.Status > math.MaxInt32 {
			return nil, nil, fmt.Errorf("invalid upstream vendor fields")
		}
		if _, duplicate := vendorMap[vendor.Name]; duplicate {
			return nil, nil, fmt.Errorf("duplicate upstream vendor")
		}
		vendorMap[vendor.Name] = vendor
	}
	return modelMap, vendorMap, nil
}
