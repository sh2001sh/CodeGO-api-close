package tencent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type signingTimestampKey struct{}

// FinalizeRequest re-signs the exact native request after channel body/header
// overrides. TC3 authentication always uses the channel credential.
func (Provider) FinalizeRequest(ctx context.Context, out *http.Request, _ *gateway.Request, target gateway.Target) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if out == nil || out.URL == nil || out.Body == nil {
		return upstream("invalid_signing_request", "tencent: missing final request")
	}
	secretID, secretKey, err := credentials(target.Secret)
	if err != nil {
		return err
	}
	if out.Header == nil {
		out.Header = make(http.Header)
	}
	if out.Header.Get("Content-Type") == "" {
		out.Header.Set("Content-Type", "application/json")
	}
	if out.Header.Get("X-TC-Action") == "" {
		out.Header.Set("X-TC-Action", "ChatCompletions")
	}
	if out.Header.Get("X-TC-Version") == "" {
		out.Header.Set("X-TC-Version", "2023-09-01")
	}
	timestamp, err := finalTimestamp(out)
	if err != nil {
		return err
	}
	body, err := finalBody(out)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	out.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	out.Header.Set("Authorization", signature(out, body, secretID, secretKey, timestamp))
	return nil
}

func finalTimestamp(out *http.Request) (int64, error) {
	if value := out.Header.Get("X-TC-Timestamp"); value != "" {
		timestamp, err := strconv.ParseInt(value, 10, 64)
		if err == nil && timestamp > 0 {
			return timestamp, nil
		}
		return 0, upstream("invalid_signing_timestamp", "tencent: invalid final signing timestamp")
	}
	if timestamp, ok := out.Context().Value(signingTimestampKey{}).(int64); ok && timestamp > 0 {
		return timestamp, nil
	}
	return 0, upstream("invalid_signing_timestamp", "tencent: final signing timestamp is unavailable")
}

func finalBody(out *http.Request) ([]byte, error) {
	reader := out.Body
	var err error
	if out.GetBody != nil {
		reader, err = out.GetBody()
		if err != nil || reader == nil {
			return nil, upstream("signing_body_unavailable", "tencent: final request body is unavailable")
		}
	}
	body, readErr := io.ReadAll(io.LimitReader(reader, maxJSONBody+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(body) > maxJSONBody {
		return nil, upstream("signing_body_unavailable", "tencent: final request body is unavailable or too large")
	}
	if out.GetBody == nil {
		out.Body = io.NopCloser(bytes.NewReader(body))
		out.ContentLength = int64(len(body))
		out.Header.Del("Content-Length")
		out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	return body, nil
}
