package auxiliary

import (
	"context"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func buildZhipuMedia(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	if in.Operation != Images {
		return nil, unsupported(in.Operation)
	}
	payload, err := mediaFields(req.Body)
	if err != nil {
		return nil, err
	}
	payload["model"] = upstreamModel(req, target)
	base := mediaBase(target, "https://open.bigmodel.cn")
	path := "/api/paas/v4/images/generations"
	if strings.HasSuffix(base, "/api/paas/v4") || strings.HasSuffix(base, "/api/coding/paas/v4") {
		path = "/images/generations"
	}
	address, err := endpoint(base, path)
	if err != nil {
		return nil, err
	}
	return jsonRequest(ctx, address, target.Secret, payload)
}

func (a *mediaAdapter) decodeZhipu(ctx context.Context, req *gateway.Request, target gateway.Target, data []byte) (Response, error) {
	if gjson.GetBytes(data, "error").Exists() {
		return Response{}, mediaError("zhipu_4v", "zhipu_image_error")
	}
	var images []mediaImage
	for _, image := range gjson.GetBytes(data, "data").Array() {
		url, b64 := image.Get("url").String(), image.Get("b64_json").String()
		if url == "" {
			url = image.Get("image_url").String()
		}
		if b64 == "" {
			b64 = image.Get("b64_image").String()
		}
		if url == "" && b64 == "" {
			return Response{}, mediaError("zhipu_4v", "empty_images")
		}
		images = append(images, mediaImage{URL: url, Base64: b64})
	}
	// v2 always returned base64 for GLM image generation.
	if err := a.encodeImages(ctx, target, images); err != nil {
		return Response{}, err
	}
	for i := range images {
		images[i].URL = ""
	}
	return imageMediaResponse(req, images, parseUsage(data), nil)
}
