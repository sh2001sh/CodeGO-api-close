package auxiliary

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/replicate"
	"github.com/tidwall/gjson"
)

type replicateMediaAdapter struct{ pollInterval time.Duration }

var _ gateway.TransportProvider = replicateMediaAdapter{}

func (a replicateMediaAdapter) UpstreamTransport(_ *gateway.Request, fallback http.RoundTripper) http.RoundTripper {
	return (replicate.Provider{PollInterval: a.pollInterval}).WithTransport(fallback)
}

func (a replicateMediaAdapter) Build(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	if in.Operation != Images && in.Operation != ImageEdits {
		return nil, unsupported(in.Operation)
	}
	if in.Operation == ImageEdits && strings.HasPrefix(in.ContentType, "multipart/") {
		fields, err := object(req.Body)
		if err != nil {
			return nil, failure(400, "invalid_request", "invalid image edit request")
		}
		// Validate client fields and channel configuration before any file upload.
		for name := range fields {
			if strings.HasSuffix(name, "_bytes") {
				delete(fields, name)
			}
		}
		fields["image_prompt"] = json.RawMessage(`"https://replicate.invalid/pending-upload"`)
		clone := *req
		clone.Body, err = json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		preview, err := replicate.BuildImageRequest(ctx, &clone, target)
		if err != nil {
			return nil, err
		}
		if err = gateway.ApplyUpstreamRequest(preview, req, target); err != nil {
			_ = preview.Body.Close()
			return nil, err
		}
		_ = preview.Body.Close()
		image, err := uploadReplicateMedia(ctx, target, in, req)
		if err != nil {
			return nil, err
		}
		fields["image_prompt"], _ = json.Marshal(image)
		clone.Body, err = json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		return replicate.BuildImageRequest(ctx, &clone, target)
	}
	if in.Operation == ImageEdits {
		image := gjson.GetBytes(req.Body, "image_prompt").String()
		if image == "" {
			image = gjson.GetBytes(req.Body, "input.image_prompt").String()
		}
		if image == "" {
			image = gjson.GetBytes(req.Body, "extra_fields.image_prompt").String()
		}
		if image == "" {
			return nil, failure(400, "missing_image", "image edit requires an image")
		}
	}
	return replicate.BuildImageRequest(ctx, req, target)
}

func (a replicateMediaAdapter) Decode(ctx context.Context, req *gateway.Request, target gateway.Target, _ Input, resp *http.Response) (Response, error) {
	// The gateway wraps resp.Body to enforce its size limit. Decode native URL
	// output first; downloads then use the gateway's selected proxy transport.
	fields, err := object(req.Body)
	if err != nil {
		return Response{}, err
	}
	fields["response_format"] = json.RawMessage(`"url"`)
	clone := *req
	clone.Body, err = json.Marshal(fields)
	if err != nil {
		return Response{}, err
	}
	stream := replicate.DecodeImages(&clone, resp)
	defer func() { _ = stream.Close() }()
	event, err := stream.Next()
	if err != nil {
		return Response{}, err
	}
	if event.Kind == gateway.EventError {
		code := "replicate_prediction_failed"
		if event.Err != nil {
			code = event.Err.Code
		}
		return Response{}, mediaError("replicate", code)
	}
	if event.Kind != gateway.EventData {
		return Response{}, mediaError("replicate", "invalid_prediction")
	}
	var payload struct {
		Data []mediaImage `json:"data"`
	}
	if json.Unmarshal(event.Payload, &payload) != nil {
		return Response{}, mediaError("replicate", "invalid_prediction")
	}
	if strings.EqualFold(gjson.GetBytes(req.Body, "response_format").String(), "b64_json") {
		if err := (&mediaAdapter{provider: "replicate"}).encodeImages(ctx, target, payload.Data); err != nil {
			return Response{}, err
		}
		for i := range payload.Data {
			payload.Data[i].URL = ""
		}
	}
	return imageMediaResponse(req, payload.Data, event.Usage, nil)
}
