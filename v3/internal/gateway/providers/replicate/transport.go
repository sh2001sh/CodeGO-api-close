package replicate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type prediction struct {
	ID      string          `json:"id"`
	Status  string          `json:"status"`
	Output  json.RawMessage `json:"output"`
	Error   json.RawMessage `json:"error"`
	Metrics json.RawMessage `json:"metrics"`
	URLs    struct {
		Get string `json:"get"`
	} `json:"urls"`
}

// WithTransport keeps prediction create, polling, and image downloads on the
// auxiliary handler's channel transport, including its proxy and TLS settings.
func (p Provider) WithTransport(transport http.RoundTripper) http.RoundTripper {
	client := http.Client{}
	if p.Client != nil {
		client = *p.Client
	}
	client.Transport = transport
	p.Client, p.injected = &client, true
	return p
}

// RoundTrip creates a prediction and returns headers promptly. Its body polls
// lazily until a terminal job, so the gateway's header timer does not limit the
// prediction lifetime. The gateway must dispatch this optional transport.
func (p Provider) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.Body == nil || req.Method != http.MethodPost {
		return nil, errors.New("replicate: invalid transport request")
	}
	client := http.Client{}
	if p.Client != nil {
		client = *p.Client
	}
	// Never forward an API credential to an upstream-supplied redirect.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	opts, ok := req.Context().Value(optionsKey{}).(transportOptions)
	if !ok {
		return nil, errors.New("replicate: transport requires a built prediction request")
	}
	closeTransport, err := applyChannelProxy(&client, opts, p.injected)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Minute)
	initial, err := client.Do(req.Clone(ctx))
	if err != nil {
		contextErr := ctx.Err()
		cancel()
		if closeTransport != nil {
			closeTransport()
		}
		if contextErr != nil {
			return nil, contextErr
		}
		return nil, errors.New("replicate: prediction request failed")
	}
	rejectRedirect(initial)
	interval := p.PollInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	body := &predictionBody{ctx: ctx, cancel: cancel, initial: initial.Body, client: &client,
		origin: req.URL, pollPath: opts.pollPath, authorization: req.Header.Get("Authorization"),
		interval: interval, closeTransport: closeTransport}
	initial.Body = body
	initial.Request = req
	if initial.StatusCode >= 200 && initial.StatusCode < 300 {
		// Replicate normally returns 201; the relay's successful response is 200.
		initial.StatusCode, initial.Status = http.StatusOK, "200 OK"
		initial.ContentLength = -1
		initial.Header.Set("Content-Type", "application/json")
		initial.Header.Del("Content-Length")
	} else {
		// Preserve status and Retry-After, without treating an HTTP error as a job.
		body.loaded = true
		body.reader = body.initial
	}
	return initial, nil
}

// rejectRedirect rewrites a 3xx prediction-create response as a 502. An
// outer http.Client may call this RoundTripper too; it must not follow a
// create redirect and forward the native body or credential elsewhere.
func rejectRedirect(initial *http.Response) {
	if initial.StatusCode < 300 || initial.StatusCode >= 400 {
		return
	}
	_ = initial.Body.Close()
	initial.StatusCode, initial.Status = http.StatusBadGateway, "502 Bad Gateway"
	initial.Header = http.Header{"Content-Type": {"application/json"}}
	initial.Body = io.NopCloser(strings.NewReader(`{"error":{"message":"Replicate upstream redirects are unsupported","code":"redirect_rejected"}}`))
}

// applyChannelProxy clones client's transport to apply opts.proxy, unless a
// custom transport was already injected via WithTransport. The returned
// func, if non-nil, must be called to release the cloned transport's idle
// connections once the request completes.
func applyChannelProxy(client *http.Client, opts transportOptions, injected bool) (func(), error) {
	if opts.proxy == nil || injected {
		return nil, nil
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	base, ok := transport.(*http.Transport)
	if !ok {
		return nil, errors.New("replicate: custom transport cannot configure a channel proxy")
	}
	cloned := base.Clone()
	cloned.Proxy = http.ProxyURL(opts.proxy)
	client.Transport = cloned
	return cloned.CloseIdleConnections, nil
}

type predictionBody struct {
	ctx            context.Context
	cancel         context.CancelFunc
	initial        io.ReadCloser
	client         *http.Client
	origin         *url.URL
	pollPath       string
	authorization  string
	interval       time.Duration
	closeTransport func()
	closeOnce      sync.Once
	loaded         bool
	reader         io.Reader
}

func (b *predictionBody) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if !b.loaded {
		b.loaded = true
		data, err := b.await()
		if err != nil {
			if contextErr := b.ctx.Err(); contextErr != nil {
				return 0, contextErr
			}
			return 0, err
		}
		b.reader = bytes.NewReader(data)
	}
	if b.reader == nil {
		return 0, io.ErrUnexpectedEOF
	}
	n, err := b.reader.Read(dst)
	if err != nil {
		if contextErr := b.ctx.Err(); contextErr != nil {
			return n, contextErr
		}
	}
	return n, err
}

func (b *predictionBody) Close() error {
	var closeErr error
	b.closeOnce.Do(func() {
		b.cancel()
		closeErr = b.initial.Close()
		if b.closeTransport != nil {
			b.closeTransport()
		}
	})
	return closeErr
}

func (b *predictionBody) await() ([]byte, error) {
	data, err := readBody(b.initial)
	_ = b.initial.Close()
	if err != nil {
		return nil, err
	}
	var expectedID string
	for {
		var job prediction
		if json.Unmarshal(data, &job) != nil || !isObject(data) {
			return nil, upstream("invalid_response", "replicate returned invalid prediction JSON")
		}
		if expectedID != "" && job.ID != "" && job.ID != expectedID {
			return nil, upstream("invalid_response", "replicate changed the prediction ID while polling")
		}
		switch job.Status {
		case "succeeded", "failed", "canceled":
			return data, nil
		case "starting", "processing":
		default:
			if hasError(job.Error) {
				return data, nil
			}
			return nil, upstream("invalid_response", "replicate returned an unknown prediction status")
		}
		pollURL, id, err := b.pollURL(job, expectedID)
		if err != nil {
			return nil, err
		}
		expectedID = id
		timer := time.NewTimer(b.interval)
		select {
		case <-b.ctx.Done():
			timer.Stop()
			return nil, b.ctx.Err()
		case <-timer.C:
		}
		req, err := http.NewRequestWithContext(b.ctx, http.MethodGet, pollURL, nil)
		if err != nil {
			return nil, upstream("invalid_response", "replicate prediction polling URL is invalid")
		}
		req.Header.Set("Authorization", b.authorization)
		req.Header.Set("Accept", "application/json")
		resp, err := b.client.Do(req)
		if err != nil {
			if b.ctx.Err() != nil {
				return nil, b.ctx.Err()
			}
			return nil, upstream("poll_failed", "replicate prediction polling failed")
		}
		data, err = readBody(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, upstream("poll_failed", "replicate prediction polling returned HTTP "+http.StatusText(resp.StatusCode))
		}
	}
}

func (b *predictionBody) pollURL(job prediction, expectedID string) (string, string, error) {
	var u *url.URL
	var err error
	if job.URLs.Get == "" {
		if !validID(job.ID) {
			return "", "", upstream("invalid_response", "replicate prediction has no polling URL or ID")
		}
		copy := *b.origin
		copy.Path, copy.RawPath, copy.RawQuery = b.pollPath+job.ID, "", ""
		u = &copy
	} else {
		u, err = url.Parse(job.URLs.Get)
		if err != nil {
			return "", "", upstream("invalid_response", "replicate prediction polling URL is invalid")
		}
		u = b.origin.ResolveReference(u)
	}
	if u.Scheme != b.origin.Scheme || !strings.EqualFold(u.Host, b.origin.Host) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, b.pollPath) {
		return "", "", upstream("invalid_response", "replicate prediction polling URL is outside the configured upstream")
	}
	id := strings.TrimPrefix(u.Path, b.pollPath)
	if !validID(id) || (job.ID != "" && job.ID != id) || (expectedID != "" && expectedID != id) {
		return "", "", upstream("invalid_response", "replicate prediction polling URL does not match its ID")
	}
	return u.String(), id, nil
}

func readBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxJSONBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if len(data) > maxJSONBody {
		return nil, upstream("invalid_response", "replicate prediction exceeds the response size limit")
	}
	return data, nil
}

func isObject(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func hasError(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte(`""`))
}
