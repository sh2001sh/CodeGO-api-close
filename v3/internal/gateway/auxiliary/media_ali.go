package auxiliary

import (
	"context"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func aliSyncImage(model string) bool {
	for _, part := range []string{"z-image", "qwen-image", "wan2.6", "wan2.7"} {
		if strings.Contains(strings.ToLower(model), part) {
			return true
		}
	}
	return false
}

func aliOldWan(model string) bool {
	return strings.Contains(model, "wan") && !strings.Contains(model, "wan2.6") && !strings.Contains(model, "wan2.7")
}

func aliMediaAsync(model string, op Operation) bool {
	if op == ImageEdits {
		return strings.Contains(model, "wan") || !aliSyncImage(model)
	}
	return !aliSyncImage(model)
}

func buildAliMedia(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	if in.Operation != Images && in.Operation != ImageEdits {
		return nil, unsupported(in.Operation)
	}
	model := upstreamModel(req, target)
	async := aliMediaAsync(model, in.Operation)
	payload, err := aliMediaPayload(req, in, model, async)
	if err != nil {
		return nil, err
	}
	if in.Operation == ImageEdits && strings.HasPrefix(in.ContentType, "multipart/") {
		if err := applyAliMediaEditImages(payload, req, in, model); err != nil {
			return nil, err
		}
	}
	path := aliMediaPath(in.Operation, model, async)
	address, err := endpoint(mediaBase(target, "https://dashscope.aliyuncs.com"), path)
	if err != nil {
		return nil, err
	}
	r, err := jsonRequest(ctx, address, target.Secret, payload)
	if err != nil {
		return nil, err
	}
	if async {
		r.Header.Set("X-DashScope-Async", "enable")
	}
	return r, nil
}

// aliMediaPayload builds the base DashScope request body (model, generation
// parameters, response format, and default prompt input) shared by image
// generation and edit requests, before any multipart edit images are mixed in.
func aliMediaPayload(req *gateway.Request, in Input, model string, async bool) (map[string]any, error) {
	payload := map[string]any{"model": model}
	parameters := map[string]any{"n": max(gjson.GetBytes(req.Body, "n").Int(), 1)}
	if size := gjson.GetBytes(req.Body, "size").String(); size != "" {
		parameters["size"] = strings.ReplaceAll(size, "x", "*")
	}
	if watermark := gjson.GetBytes(req.Body, "watermark"); watermark.Exists() {
		parameters["watermark"] = watermark.Bool()
	}
	if raw := gjson.GetBytes(req.Body, "parameters"); raw.Exists() {
		value, err := mediaFields([]byte(raw.Raw))
		if err != nil {
			return nil, err
		}
		parameters = value
	}
	payload["parameters"] = parameters
	if format := gjson.GetBytes(req.Body, "response_format").String(); format != "" {
		payload["response_format"] = format
	}
	if raw := gjson.GetBytes(req.Body, "input"); raw.Exists() {
		value, err := mediaFields([]byte(raw.Raw))
		if err != nil {
			return nil, err
		}
		payload["input"] = value
	} else if async {
		payload["input"] = map[string]any{"prompt": gjson.GetBytes(req.Body, "prompt").String()}
	} else {
		payload["input"] = aliImageMessages(nil, gjson.GetBytes(req.Body, "prompt").String())
	}
	return payload, nil
}

// applyAliMediaEditImages decodes the client's multipart edit images and
// overwrites payload["input"] with the model-appropriate shape (legacy "wan"
// models use a flat prompt/images/negative_prompt object; others use chat
// messages).
func applyAliMediaEditImages(payload map[string]any, req *gateway.Request, in Input, model string) error {
	images, err := mediaMultipartImages(in)
	if err != nil {
		return err
	}
	prompt := gjson.GetBytes(req.Body, "prompt").String()
	if aliOldWan(model) {
		input := map[string]any{"prompt": prompt, "images": images}
		if negative := gjson.GetBytes(req.Body, "negative_prompt").String(); negative != "" {
			input["negative_prompt"] = negative
		}
		payload["input"] = input
	} else {
		payload["input"] = aliImageMessages(images, prompt)
	}
	return nil
}

// aliMediaPath resolves the DashScope API path for the given operation,
// model, and sync/async mode.
func aliMediaPath(op Operation, model string, async bool) string {
	path := "/api/v1/services/aigc/multimodal-generation/generation"
	if op == Images && async {
		path = "/api/v1/services/aigc/text2image/image-synthesis"
	}
	if op == ImageEdits {
		switch {
		case aliOldWan(model):
			path = "/api/v1/services/aigc/image2image/image-synthesis"
		case strings.Contains(model, "wan"):
			path = "/api/v1/services/aigc/image-generation/generation"
		}
	}
	return path
}

func aliImageMessages(images []string, prompt string) map[string]any {
	content := make([]map[string]any, 0, len(images)+1)
	for _, image := range images {
		content = append(content, map[string]any{"image": image})
	}
	content = append(content, map[string]any{"text": prompt})
	return map[string]any{"messages": []map[string]any{{"role": "user", "content": content}}}
}
