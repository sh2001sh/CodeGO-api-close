package gemini

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ImageTransport selects a safe image network transport using the channel's
// proxy/TLS identity. Custom TLS wrappers must implement httpx.PinnedImageTransport.
type ImageTransport func(context.Context, gateway.Target) (http.RoundTripper, error)

func (p Provider) inlineRemoteImages(ctx context.Context, body []byte, target gateway.Target) ([]byte, error) {
	var transport http.RoundTripper
	selected := false
	for messageIndex, message := range gjson.GetBytes(body, "messages").Array() {
		for contentIndex, item := range message.Get("content").Array() {
			if item.Get("type").Str != "image_url" {
				continue
			}
			rawURL := item.Get("image_url.url").Str
			if strings.HasPrefix(rawURL, "data:") {
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
			image, err := httpx.FetchImage(ctx, rawURL, httpx.ImageFetchConfig{Transport: transport})
			if err != nil {
				if errors.Is(err, httpx.ErrImageTransportPolicy) {
					return nil, channelConfigError("image network transport cannot guarantee address pinning and TLS identity")
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, err
				}
				return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
					Code: "image_fetch_failed", Message: "gemini: could not download a supported public image"}
			}
			inline := "data:" + image.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(image.Data)
			path := fmt.Sprintf("messages.%d.content.%d.image_url.url", messageIndex, contentIndex)
			body, err = sjson.SetBytes(body, path, inline)
			if err != nil {
				return nil, err
			}
			if len(body) > maxJSONBody {
				return nil, &gateway.UpstreamError{Status: http.StatusBadRequest, Type: "invalid_request_error",
					Code: "image_input_too_large", Message: "gemini: combined inline image input exceeds body limit"}
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
			return nil, channelConfigError("image transport is unavailable")
		}
		return transport, nil
	}
	if target.Fingerprint.TLSProfile != "" {
		return nil, channelConfigError("remote images with custom TLS require a pinned image transport")
	}
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ForceAttemptHTTP2: true}
	if target.ProxyURL != "" {
		proxy, err := url.Parse(target.ProxyURL)
		if err != nil || proxy.Host == "" || (proxy.Scheme != "http" && proxy.Scheme != "https" && proxy.Scheme != "socks5" && proxy.Scheme != "socks5h") {
			return nil, channelConfigError("invalid image proxy configuration")
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	return transport, nil
}

func channelConfigError(message string) *gateway.UpstreamError {
	return &gateway.UpstreamError{Status: http.StatusBadGateway, Type: "upstream_error",
		Code: "invalid_channel_configuration", Message: "gemini: " + message}
}
