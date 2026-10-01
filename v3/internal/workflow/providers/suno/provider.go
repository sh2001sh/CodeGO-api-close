package suno

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type Provider struct{ client *http.Client }

func New(client *http.Client) *Provider { return &Provider{native.Client(client)} }

var _ native.Adapter = (*Provider)(nil)

func (p *Provider) Submit(ctx context.Context, target gateway.Target, input native.Submit) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	action, err := resolveSunoAction(input)
	if err != nil {
		return native.Result{}, err
	}
	body, err := sunoSubmitBody(input, target, action)
	if err != nil {
		return native.Result{}, err
	}
	data, err := p.request(ctx, target, "/suno/submit/"+action, body)
	if err != nil {
		return native.Result{}, err
	}
	return decodeSubmitResponse(data)
}

// resolveSunoAction normalizes the requested action to MUSIC or LYRICS.
func resolveSunoAction(input native.Submit) (string, error) {
	action := strings.ToUpper(input.Action)
	if action == "" || action == "TEXTGENERATE" {
		if input.Model == "suno_lyrics" {
			action = "LYRICS"
		} else {
			action = "MUSIC"
		}
	}
	if action != "MUSIC" && action != "LYRICS" {
		return "", invalid("invalid suno action")
	}
	return action, nil
}

// sunoSubmitBody validates the request fields for the resolved action and
// marshals the upstream submit body, resolving the "mv" model for MUSIC
// requests.
func sunoSubmitBody(input native.Submit, target gateway.Target, action string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input.Body, &fields); err != nil || fields == nil {
		return nil, invalid("invalid suno request")
	}
	var prompt string
	if v := fields["prompt"]; len(v) != 0 && json.Unmarshal(v, &prompt) != nil {
		return nil, invalid("invalid suno prompt")
	}
	if action == "LYRICS" && strings.TrimSpace(prompt) == "" {
		return nil, invalid("suno lyrics prompt required")
	}
	if action == "MUSIC" {
		model := target.UpstreamModel
		if model == "" || model == "suno_music" || model == "suno_lyrics" {
			if v := fields["mv"]; len(v) > 0 && json.Unmarshal(v, &model) != nil {
				return nil, invalid("invalid suno mv")
			}
			if model == "" || model == "suno_music" || model == "suno_lyrics" {
				model = "chirp-v3-0"
			}
		}
		fields["mv"], _ = json.Marshal(model)
	}
	delete(fields, "model")
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	return body, nil
}

// decodeSubmitResponse parses the suno submit response envelope.
func decodeSubmitResponse(data []byte) (native.Result, error) {
	var response struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return native.Result{}, errors.New("invalid suno submit response")
	}
	if response.Code != "success" {
		return native.Result{Status: "failed", Error: response.Message, Data: data}, nil
	}
	if response.Data == "" {
		return native.Result{}, errors.New("suno task ID missing")
	}
	return native.Result{ID: response.Data, Status: "queued", Data: data}, nil
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if task.UpstreamID == "" {
		return native.Result{}, invalid("missing upstream task ID")
	}
	body, _ := json.Marshal(map[string]any{"ids": []string{task.UpstreamID}})
	data, err := p.request(ctx, target, "/suno/fetch", body)
	if err != nil {
		return native.Result{}, err
	}
	var response struct {
		Code string            `json:"code"`
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return native.Result{}, errors.New("invalid suno fetch response")
	}
	if response.Code != "success" {
		return native.Result{}, errors.New("suno fetch rejected")
	}
	for _, item := range response.Data {
		var result struct {
			ID     string          `json:"task_id"`
			Status string          `json:"status"`
			Error  string          `json:"fail_reason"`
			Data   json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(item, &result); err != nil {
			return native.Result{}, errors.New("invalid suno task")
		}
		if result.ID != task.UpstreamID {
			continue
		}
		status, err := taskStatus(result.Status)
		if err != nil {
			return native.Result{}, err
		}
		r := native.Result{ID: result.ID, Status: status, Error: result.Error, Data: item}
		if status == "completed" {
			var songs []struct {
				URL      string `json:"audio_url"`
				Metadata struct {
					Duration json.RawMessage `json:"duration"`
				} `json:"metadata"`
			}
			if json.Unmarshal(result.Data, &songs) == nil {
				for _, song := range songs {
					if r.URL == "" {
						r.URL = song.URL
					}
					duration := positiveNumber(song.Metadata.Duration)
					r.Units += duration
				}
			}
		}
		return r, nil
	}
	return native.Result{}, errors.New("suno task not present in fetch response")
}

func (p *Provider) Content(context.Context, gateway.Target, native.Task) (*http.Response, error) {
	return nil, native.ErrContentUnsupported
}

func (p *Provider) request(ctx context.Context, target gateway.Target, path string, body []byte) ([]byte, error) {
	endpoint, err := native.Endpoint(target, "", path)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, http.MethodPost, endpoint, target.Secret, body)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	req.Header.Set("Accept", "application/json")
	return native.JSON(p.client, req)
}

func taskStatus(s string) (string, error) {
	switch strings.ToLower(s) {
	case "submitted", "queueing", "queued":
		return "queued", nil
	case "processing", "running":
		return "in_progress", nil
	case "success", "succeeded", "completed":
		return "completed", nil
	case "failed", "failure", "error", "cancelled", "canceled":
		return "failed", nil
	default:
		return "", errors.New("unknown suno task status")
	}
}

func positiveNumber(raw json.RawMessage) float64 {
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return 0
		}
		n, _ = strconv.ParseFloat(s, 64)
	}
	if n > 0 && n < 1e9 {
		return n
	}
	return 0
}

func invalid(message string) error { return &native.InvalidRequest{Err: errors.New(message)} }
