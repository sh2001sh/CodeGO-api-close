package auxiliary

import (
	"context"
	"net/http"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func buildJimengMedia(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	if in.Operation != Images {
		return nil, unsupported(in.Operation)
	}
	format := gjson.GetBytes(req.Body, "response_format").String()
	payload := map[string]any{"req_key": upstreamModel(req, target), "prompt": gjson.GetBytes(req.Body, "prompt").String(), "return_url": format == "" || format == "url"}
	if err := mergeMediaMetadata(payload, req.Body, "extra_fields"); err != nil {
		return nil, err
	}
	payload["req_key"] = upstreamModel(req, target)
	address, err := endpoint(mediaBase(target, "https://visual.volcengineapi.com"), "/")
	if err != nil {
		return nil, err
	}
	r, err := jsonRequest(ctx, address, "", payload)
	if err != nil {
		return nil, err
	}
	query := r.URL.Query()
	query.Set("Action", "CVProcess")
	query.Set("Version", "2022-08-31")
	r.URL.RawQuery = query.Encode()
	if err := signJimengMedia(r, target.Secret, time.Now().UTC()); err != nil {
		return nil, err
	}
	return r, nil
}

func decodeJimengMedia(req *gateway.Request, data []byte) (Response, error) {
	if gjson.GetBytes(data, "code").Int() != 10000 {
		return Response{}, mediaError("jimeng", "jimeng_image_error")
	}
	var images []mediaImage
	for _, value := range gjson.GetBytes(data, "data.binary_data_base64").Array() {
		images = append(images, mediaImage{Base64: value.String()})
	}
	for _, value := range gjson.GetBytes(data, "data.image_urls").Array() {
		images = append(images, mediaImage{URL: value.String()})
	}
	return imageMediaResponse(req, images, parseUsage(data), nil)
}
