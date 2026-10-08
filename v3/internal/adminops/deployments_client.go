package adminops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/adminops/ionet"
)

var providerID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,255}$`)

func validProviderID(id string) bool { return providerID.MatchString(id) }
func (s *Server) deploymentAPIKey(ctx context.Context) (key string, enabled, configured bool, err error) {
	v, e := s.settings.Get(ctx, "model_deployment.ionet.enabled")
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return "", false, false, e
	}
	if e == nil {
		if json.Unmarshal(v, &enabled) != nil {
			var raw string
			if json.Unmarshal(v, &raw) != nil || raw != "true" && raw != "false" {
				return "", false, false, errors.New("invalid deployment enablement setting")
			}
			enabled = raw == "true"
		}
	}
	v, e = s.settings.Get(ctx, "model_deployment.ionet.api_key")
	if errors.Is(e, pgx.ErrNoRows) {
		return "", enabled, false, nil
	}
	if e != nil {
		return "", enabled, false, e
	}
	if json.Unmarshal(v, &key) != nil {
		return "", false, false, errors.New("invalid encrypted deployment credential")
	}
	key = strings.TrimSpace(key)
	return key, enabled, key != "", nil
}
func (s *Server) deploymentClient(ctx context.Context, public bool) (*ionet.Client, error) {
	key, enabled, configured, err := s.deploymentAPIKey(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled || !configured {
		return nil, ErrDeploymentProviderUnavailable
	}
	return s.makeDeploymentClient(ctx, key, public), nil
}
func (s *Server) makeDeploymentClient(ctx context.Context, key string, public bool) *ionet.Client {
	base := s.cfg.DeploymentEnterpriseURL
	if base == "" {
		base = ionet.DefaultEnterpriseBaseURL
	}
	if public {
		base = s.cfg.DeploymentPublicURL
		if base == "" {
			base = ionet.DefaultBaseURL
		}
	}
	c := ionet.NewClientWithConfig(key, strings.TrimRight(base, "/"), providerHTTP{client: s.cfg.HTTPClient, credential: key})
	c.Context = ctx
	return c
}

type providerHTTP struct {
	client     *http.Client
	credential string
}

func (h providerHTTP) Do(in *ionet.HTTPRequest) (*ionet.HTTPResponse, error) {
	_, err := validateToolURL(in.URL)
	if err != nil {
		return nil, errors.New("invalid provider URL")
	}
	ctx, cancel := context.WithTimeout(in.Context, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, in.Method, in.URL, bytes.NewReader(in.Body))
	if err != nil {
		return nil, errors.New("invalid provider request")
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}
	// A provider redirect must never receive deployment API credentials.
	client := *h.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("deployment provider request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, errors.New("deployment provider response failed")
	}
	if len(body) > 10<<20 {
		return nil, errors.New("deployment provider response exceeds size limit")
	}
	if h.credential != "" {
		body = redactCredentialBody(body, h.credential)
	}
	return &ionet.HTTPResponse{StatusCode: resp.StatusCode, Body: body}, nil
}

func redactCredentialBody(body []byte, credential string) []byte {
	var v any
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return bytes.ReplaceAll(body, []byte(credential), []byte("[REDACTED]"))
	}
	v = replaceCredential(v, credential)
	clean, err := json.Marshal(v)
	if err != nil {
		return bytes.ReplaceAll(body, []byte(credential), []byte("[REDACTED]"))
	}
	return clean
}
func replaceCredential(v any, credential string) any {
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, credential, "[REDACTED]")
	case map[string]any:
		for k, value := range x {
			x[k] = replaceCredential(value, credential)
		}
	case []any:
		for i, value := range x {
			x[i] = replaceCredential(value, credential)
		}
	}
	return v
}
func (s *Server) deploymentReply(w http.ResponseWriter, data any, err error) {
	if err == nil {
		respond(w, redact(data))
		return
	}
	switch {
	case errors.Is(err, ErrDeploymentProviderUnavailable), errors.Is(err, ErrDeploymentAPIKeyRequired):
		fail(w, 503, "provider_unavailable", "Deployment provider is disabled or unconfigured")
	case errors.Is(err, ErrDeploymentNameUnavailable):
		fail(w, 409, "name_unavailable", err.Error())
	case errors.Is(err, ErrDeploymentIDRequired), errors.Is(err, ErrDeploymentContainerIDRequired), errors.Is(err, ErrDeploymentNameRequired), errors.Is(err, ErrDeploymentNameQueryRequired), errors.Is(err, ErrDeploymentHardwareIDRequired), errors.Is(err, ErrDeploymentHardwareIDInvalid), errors.Is(err, ErrDeploymentContainerQueryMiss):
		fail(w, 400, "invalid_deployment", err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		fail(w, 504, "provider_timeout", "Deployment provider timed out")
	default:
		var apiErr *ionet.APIError
		if errors.As(err, &apiErr) {
			fail(w, 502, "provider_error", fmt.Sprintf("Deployment provider returned HTTP %d", apiErr.Code))
		} else {
			fail(w, 502, "provider_error", "Deployment provider request failed")
		}
	}
}
func redact(data any) any {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	var v any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&v) != nil {
		return nil
	}
	redactValue(v)
	return v
}
func redactValue(v any) {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			key := strings.ToLower(k)
			if strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "token") || key == "api_key" || key == "authorization" {
				delete(v, k)
				continue
			}
			redactValue(x)
		}
	case []any:
		for _, x := range v {
			redactValue(x)
		}
	}
}
