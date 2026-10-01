package bedrock

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (p Provider) inlineRemoteImages(ctx context.Context, body []byte, target gateway.Target) ([]byte, error) {
	var transport http.RoundTripper
	selected := false
	for messageIndex, message := range gjson.GetBytes(body, "messages").Array() {
		for contentIndex, content := range message.Get("content").Array() {
			if content.Get("type").Str != "image" || content.Get("source.type").Str != "url" {
				continue
			}
			if !selected {
				var err error
				transport, err = p.imageTransport(ctx, target)
				if err != nil {
					return nil, err
				}
				selected = true
			}
			image, err := httpx.FetchImage(ctx, content.Get("source.url").Str, httpx.ImageFetchConfig{Transport: transport})
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}
				if errors.Is(err, httpx.ErrImageTransportPolicy) {
					return nil, imageConfigError()
				}
				return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
					Code: "image_fetch_failed", Message: "bedrock: could not download a supported public image"}
			}
			source := map[string]string{"type": "base64", "media_type": image.MIMEType, "data": base64.StdEncoding.EncodeToString(image.Data)}
			path := fmt.Sprintf("messages.%d.content.%d.source", messageIndex, contentIndex)
			body, err = sjson.SetBytes(body, path, source)
			if err != nil {
				return nil, err
			}
			if len(body) > maxBody {
				return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
					Code: "image_input_too_large", Message: "bedrock: combined image input exceeds body limit"}
			}
		}
	}
	return body, nil
}

func (p Provider) imageTransport(ctx context.Context, target gateway.Target) (http.RoundTripper, error) {
	if p.ImageTransport != nil {
		transport, err := p.ImageTransport(ctx, target)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if err != nil || transport == nil {
			return nil, imageConfigError()
		}
		return transport, nil
	}
	if target.Fingerprint.TLSProfile != "" {
		return nil, imageConfigError()
	}
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ForceAttemptHTTP2: true}
	if target.ProxyURL != "" {
		proxy, err := url.Parse(target.ProxyURL)
		if err != nil || proxy.Host == "" || (proxy.Scheme != "http" && proxy.Scheme != "https" && proxy.Scheme != "socks5" && proxy.Scheme != "socks5h") {
			return nil, imageConfigError()
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	return transport, nil
}

func imageConfigError() *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error",
		Code: "invalid_channel_configuration", Message: "bedrock: image transport cannot preserve channel network policy"}
}
