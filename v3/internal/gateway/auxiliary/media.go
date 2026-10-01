package auxiliary

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
	"github.com/tidwall/gjson"
)

type mediaAdapter struct {
	provider     string
	client       *http.Client
	transports   *httpx.Pool
	pollInterval time.Duration
}

func mediaAdapters() map[string]Adapter {
	pool := httpx.NewPool(httpx.TransportConfig{})
	adapters := make(map[string]Adapter)
	for _, provider := range []string{"minimax", "volcengine", "ali", "jimeng", "zhipu_4v"} {
		adapters[provider] = &mediaAdapter{provider: provider, transports: pool}
	}
	adapters["replicate"] = replicateMediaAdapter{}
	return adapters
}

func (a *mediaAdapter) Build(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	switch a.provider {
	case "minimax":
		return buildMiniMaxMedia(ctx, req, target, in)
	case "volcengine":
		return buildVolcMedia(ctx, req, target, in)
	case "ali":
		return buildAliMedia(ctx, req, target, in)
	case "jimeng":
		return buildJimengMedia(ctx, req, target, in)
	case "zhipu_4v":
		return buildZhipuMedia(ctx, req, target, in)
	default:
		return nil, unsupported(in.Operation)
	}
}

func (a *mediaAdapter) Decode(ctx context.Context, req *gateway.Request, target gateway.Target, in Input, resp *http.Response) (Response, error) {
	data, err := readResponse(resp)
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, mediaError(a.provider, "upstream_http_error")
	}
	if !gjson.ValidBytes(data) {
		return Response{}, mediaError(a.provider, "invalid_response")
	}
	if err := mediaUsageError(parseUsage(data)); err != nil {
		return Response{}, err
	}
	switch a.provider {
	case "minimax":
		return a.decodeMiniMax(ctx, req, target, in, data)
	case "volcengine":
		return decodeVolcMedia(req, in, data)
	case "ali":
		return a.decodeAli(ctx, req, target, in, data)
	case "jimeng":
		return decodeJimengMedia(req, data)
	case "zhipu_4v":
		return a.decodeZhipu(ctx, req, target, data)
	default:
		return Response{}, unsupported(in.Operation)
	}
}

func mediaError(provider, code string) error {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error", Code: code, Message: provider + " media request failed"}
}

func mediaBase(target gateway.Target, fallback string) string {
	if target.BaseURL != "" {
		return strings.TrimRight(target.BaseURL, "/")
	}
	return fallback
}

func mediaFields(body []byte) (map[string]any, error) {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errors.New("auxiliary: invalid media request")
	}
	return value, nil
}

func mergeMediaMetadata(payload map[string]any, body []byte, key string) error {
	raw := gjson.GetBytes(body, key)
	if !raw.Exists() {
		return nil
	}
	value, err := mediaFields([]byte(raw.Raw))
	if err != nil {
		return err
	}
	mergeMediaFields(payload, value)
	return nil
}

func mergeMediaFields(payload, value map[string]any) {
	for name, field := range value {
		current, currentOK := payload[name].(map[string]any)
		nested, nestedOK := field.(map[string]any)
		if currentOK && nestedOK {
			mergeMediaFields(current, nested)
		} else {
			payload[name] = field
		}
	}
}

type mediaImage struct {
	URL           string `json:"url,omitempty"`
	Base64        string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

func imageMediaResponse(req *gateway.Request, images []mediaImage, usage *gateway.Usage, metadata any) (Response, error) {
	if err := mediaUsageError(usage); err != nil {
		return Response{}, err
	}
	if len(images) == 0 {
		return Response{}, mediaError("image", "empty_images")
	}
	for _, image := range images {
		if image.URL == "" && image.Base64 == "" {
			return Response{}, mediaError("image", "empty_images")
		}
	}
	created := req.Received.Unix()
	if req.Received.IsZero() {
		created = time.Now().Unix()
	}
	payload := map[string]any{"created": created, "data": images}
	if metadata != nil {
		payload["metadata"] = metadata
	}
	body, err := json.Marshal(payload)
	return Response{Body: body, Header: http.Header{"Content-Type": {"application/json"}, "X-Codego-Image-Count": {strconv.Itoa(len(images))}}, Usage: usage}, err
}

func mediaUsageError(usage *gateway.Usage) error {
	if usage == nil {
		return nil
	}
	for _, count := range []int64{usage.PromptTokens, usage.CompletionTokens, usage.CachedTokens, usage.CacheWriteTokens, usage.CacheWrite1hTokens, usage.ImageInputTokens, usage.ImageOutputTokens, usage.AudioInputTokens, usage.AudioOutputTokens} {
		if count < 0 {
			return mediaError("media", "invalid_usage")
		}
	}
	return nil
}

func speechMediaResponse(audio []byte, format string, usage *gateway.Usage) (Response, error) {
	if len(audio) == 0 {
		return Response{}, mediaError("speech", "empty_audio")
	}
	contentType := map[string]string{"mp3": "audio/mpeg", "wav": "audio/wav", "flac": "audio/flac", "aac": "audio/aac", "pcm": "audio/pcm", "opus": "audio/ogg", "ogg_opus": "audio/ogg"}[format]
	if contentType == "" {
		contentType = "audio/mpeg"
	}
	return Response{Body: audio, Header: http.Header{"Content-Type": {contentType}}, Usage: usage}, nil
}

func (a *mediaAdapter) httpClient(ctx context.Context, target gateway.Target) (*http.Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	if _, ok := ctx.Value(clientKey{}).(*http.Client); ok {
		return upstreamClient(ctx), nil
	}
	pool := a.transports
	if pool == nil {
		pool = httpx.NewPool(httpx.TransportConfig{})
	}
	transport, err := pool.Transport(target.ProxyURL)
	if err != nil {
		return nil, errors.New("auxiliary: invalid media proxy")
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func (a *mediaAdapter) fetch(ctx context.Context, target gateway.Target, address string) ([]byte, error) {
	return a.fetchBounded(ctx, target, address, 64<<20)
}

func (a *mediaAdapter) fetchBounded(ctx context.Context, target gateway.Target, address string, maximum int64) ([]byte, error) {
	// Only response URLs are used, with no provider credential on the download.
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, mediaError(a.provider, "invalid_media_url")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, mediaError(a.provider, "invalid_media_url")
	}
	client, err := a.httpClient(ctx, target)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, mediaError(a.provider, "media_download_failed")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, mediaError(a.provider, "media_download_failed")
	}
	resp.Body = http.MaxBytesReader(nil, resp.Body, maximum)
	return readResponse(resp)
}

func (a *mediaAdapter) encodeImages(ctx context.Context, target gateway.Target, images []mediaImage) error {
	remaining := 64 << 20
	for _, image := range images {
		remaining -= len(image.Base64)
	}
	if remaining < 0 {
		return mediaError(a.provider, "images_too_large")
	}
	for i := range images {
		if images[i].Base64 != "" {
			continue
		}
		maximum := remaining / 4 * 3
		if maximum <= 0 {
			return mediaError(a.provider, "images_too_large")
		}
		data, err := a.fetchBounded(ctx, target, images[i].URL, int64(maximum))
		if err != nil {
			return err
		}
		images[i].Base64 = base64.StdEncoding.EncodeToString(data)
		remaining -= len(images[i].Base64)
	}
	return nil
}
