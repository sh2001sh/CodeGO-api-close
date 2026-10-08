package replicate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/httpx"
)

type unsupportedStream struct {
	body io.ReadCloser
	done bool
}

func (s *unsupportedStream) Next() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	s.done = true
	return gateway.Event{Kind: gateway.EventError, Err: invalid("unsupported_protocol", "replicate supports image generation and image edits only")}, nil
}

func (s *unsupportedStream) Close() error { return s.body.Close() }

// DecodeImages converts a completed native prediction to OpenAI image JSON.
// The body returned by RoundTrip performs pending prediction polling first.
// No token usage is invented; the auxiliary endpoint accounts for images.
func DecodeImages(req *gateway.Request, resp *http.Response) gateway.EventStream {
	ctx := context.Background()
	client := http.Client{}
	trustedOrigin := ""
	if resp.Request != nil {
		ctx = resp.Request.Context()
	}
	if pending, ok := resp.Body.(*predictionBody); ok {
		ctx, client = pending.ctx, *pending.client
		trustedOrigin = pending.origin.String()
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	return &imageStream{body: resp.Body, request: req, client: &client, trustedOrigin: trustedOrigin, ctx: ctx, cancel: cancel}
}

type imageStream struct {
	body          io.ReadCloser
	request       *gateway.Request
	client        *http.Client
	trustedOrigin string
	ctx           context.Context
	cancel        context.CancelFunc
	done          bool
}

func (s *imageStream) Close() error {
	s.cancel()
	return s.body.Close()
}

func (s *imageStream) Next() (gateway.Event, error) {
	if s.done {
		return gateway.Event{}, io.EOF
	}
	s.done = true
	data, err := readBody(s.body)
	if err != nil {
		var reported *gateway.UpstreamError
		if errors.As(err, &reported) {
			return gateway.Event{Kind: gateway.EventError, Err: reported}, nil
		}
		return gateway.Event{}, err
	}
	var job prediction
	if !isObject(data) || json.Unmarshal(data, &job) != nil {
		return imageError("invalid_response", "replicate returned invalid prediction JSON"), nil
	}
	if ev, failed := jobStatusError(job); failed {
		return ev, nil
	}
	urls, errEvent := jobOutputURLs(job.Output)
	if errEvent != nil {
		return *errEvent, nil
	}
	images, errEvent, err := s.collectImages(urls)
	if err != nil {
		return gateway.Event{}, err
	}
	if errEvent != nil {
		return *errEvent, nil
	}
	if len(images) == 0 {
		return imageError("empty_response", "replicate prediction returned no images"), nil
	}
	result := struct {
		Created int64               `json:"created"`
		Data    []map[string]string `json:"data"`
	}{Created: time.Now().Unix(), Data: images}
	payload, err := json.Marshal(result)
	return gateway.Event{Kind: gateway.EventData, Payload: payload}, err
}

// jobStatusError maps a non-success prediction status to an OpenAI-shaped
// error event. failed is false only when the job succeeded.
func jobStatusError(job prediction) (gateway.Event, bool) {
	switch {
	case job.Status == "canceled":
		return imageError("prediction_canceled", "replicate prediction was canceled"), true
	case hasError(job.Error):
		return imageError("prediction_failed", predictionError(job.Error)), true
	case job.Status == "failed":
		return imageError("prediction_failed", "replicate prediction failed"), true
	case job.Status != "succeeded":
		return imageError("invalid_response", "replicate prediction has not completed"), true
	default:
		return gateway.Event{}, false
	}
}

// jobOutputURLs normalizes a succeeded prediction's output (a single URL or
// a URL array) into a slice.
func jobOutputURLs(output json.RawMessage) ([]string, *gateway.Event) {
	var urls []string
	var one string
	if json.Unmarshal(output, &one) == nil {
		return []string{one}, nil
	}
	if json.Unmarshal(output, &urls) != nil {
		ev := imageError("invalid_response", "replicate image output must be a URL or URL array")
		return nil, &ev
	}
	return urls, nil
}

// collectImages validates each output URL and, if the client requested
// base64 images, downloads and encodes them. A non-nil err (context
// cancellation) takes priority over errEvent and must be returned as-is by
// the caller.
func (s *imageStream) collectImages(urls []string) (images []map[string]string, errEvent *gateway.Event, err error) {
	var format struct {
		ResponseFormat string `json:"response_format"`
	}
	if json.Unmarshal(s.request.Body, &format) != nil {
		ev := imageError("invalid_response", "replicate image client request is malformed")
		return nil, &ev, nil
	}
	images = make([]map[string]string, 0, len(urls))
	for _, image := range urls {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		u, parseErr := url.Parse(image)
		if parseErr != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			ev := imageError("invalid_response", "replicate returned an invalid image URL")
			return nil, &ev, nil
		}
		if strings.EqualFold(format.ResponseFormat, "b64_json") {
			value, downloadErr := s.download(image)
			if downloadErr != nil {
				if s.ctx.Err() != nil {
					return nil, nil, s.ctx.Err()
				}
				ev := imageError("image_download_failed", "replicate image download failed")
				return nil, &ev, nil
			}
			images = append(images, map[string]string{"b64_json": value})
		} else {
			images = append(images, map[string]string{"url": image})
		}
	}
	return images, nil, nil
}

func (s *imageStream) download(image string) (string, error) {
	// File outputs often use a separate CDN. Never attach the prediction key.
	data, err := httpx.FetchMedia(s.ctx, image, httpx.MediaFetchConfig{Client: s.client, TrustedOrigin: s.trustedOrigin, MaxBytes: maxJSONBody})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func imageError(code, message string) gateway.Event {
	return gateway.Event{Kind: gateway.EventError, Err: upstream(code, message)}
}

func predictionError(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil && text != "" {
		return text
	}
	var fields struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
		Code    string `json:"code"`
	}
	if json.Unmarshal(raw, &fields) == nil {
		for _, value := range []string{fields.Message, fields.Detail, fields.Code} {
			if value != "" {
				return value
			}
		}
	}
	return "replicate prediction failed"
}
