package auxiliary

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
)

func (a *mediaAdapter) decodeAli(ctx context.Context, req *gateway.Request, target gateway.Target, in Input, data []byte) (Response, error) {
	if gjson.GetBytes(data, "code").String() != "" || gjson.GetBytes(data, "message").String() != "" {
		return Response{}, mediaError("ali", "ali_image_error")
	}
	if aliMediaAsync(upstreamModel(req, target), in.Operation) {
		var err error
		data, err = a.pollAli(ctx, target, gjson.GetBytes(data, "output.task_id").String())
		if err != nil {
			return Response{}, err
		}
	}
	var images []mediaImage
	for _, image := range gjson.GetBytes(data, "output.results").Array() {
		if image.Get("code").String() != "" {
			return Response{}, mediaError("ali", "ali_image_error")
		}
		images = append(images, mediaImage{URL: image.Get("url").String(), Base64: image.Get("b64_image").String()})
	}
	for _, choice := range gjson.GetBytes(data, "output.choices").Array() {
		var prompt string
		start := len(images)
		for _, item := range choice.Get("message.content").Array() {
			if text := item.Get("text").String(); text != "" {
				prompt = text
			}
			value := item.Get("image").String()
			if value == "" {
				continue
			}
			image := mediaImage{URL: value}
			if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
				image.URL, image.Base64 = "", value
			}
			images = append(images, image)
		}
		for i := start; i < len(images); i++ {
			images[i].RevisedPrompt = prompt
		}
	}
	if gjson.GetBytes(req.Body, "response_format").String() == "b64_json" {
		if err := a.encodeImages(ctx, target, images); err != nil {
			return Response{}, err
		}
	}
	return imageMediaResponse(req, images, parseUsage(data), nil)
}

func (a *mediaAdapter) pollAli(ctx context.Context, target gateway.Target, taskID string) ([]byte, error) {
	if taskID == "" || taskID == "." || taskID == ".." || strings.ContainsAny(taskID, "/?#%\\") {
		return nil, mediaError("ali", "invalid_task_id")
	}
	address, err := endpoint(mediaBase(target, "https://dashscope.aliyuncs.com"), "/api/v1/tasks/"+taskID)
	if err != nil {
		return nil, err
	}
	client, err := a.httpClient(ctx, target)
	if err != nil {
		return nil, err
	}
	interval := a.pollInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	for step := 0; step < 20; step++ {
		if step > 0 {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		r.Header.Set("Authorization", "Bearer "+target.Secret)
		resp, err := client.Do(r)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, mediaError("ali", "task_poll_failed")
		}
		data, err := readResponse(resp)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK || !gjson.ValidBytes(data) || gjson.GetBytes(data, "code").String() != "" {
			return nil, mediaError("ali", "task_poll_failed")
		}
		switch gjson.GetBytes(data, "output.task_status").String() {
		case "SUCCEEDED":
			return data, nil
		case "PENDING", "RUNNING":
			continue
		default:
			return nil, mediaError("ali", "task_failed")
		}
	}
	return nil, mediaError("ali", "task_poll_timeout")
}
