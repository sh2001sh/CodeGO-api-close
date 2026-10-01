package bedrock

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// FinalizeRequest signs the final converted request after channel overrides.
// Signing only BuildRequest's body would invalidate SigV4 when overrides change it.
func (Provider) FinalizeRequest(ctx context.Context, out *http.Request, _ *gateway.Request, target gateway.Target) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if out == nil || out.URL == nil || out.Body == nil {
		return fmt.Errorf("bedrock: missing request body")
	}
	cred, err := credentials(target.Secret, target.BaseURL)
	if err != nil {
		return err
	}
	if cred.Bearer != "" {
		return nil
	}
	reader := out.Body
	if out.GetBody != nil {
		reader, err = out.GetBody()
		if err != nil || reader == nil {
			return fmt.Errorf("bedrock: request body unavailable")
		}
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxBody+1))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || len(body) > maxBody {
		return fmt.Errorf("bedrock: request body unavailable or too large")
	}
	if out.GetBody == nil {
		out.Body = io.NopCloser(bytes.NewReader(body))
		out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	out.ContentLength = int64(len(body))
	if out.Header == nil {
		out.Header = make(http.Header)
	}
	out.Header.Del("Content-Length")
	if err := signRequest(out, body, cred, time.Now()); err != nil {
		return fmt.Errorf("bedrock: final request signing failed")
	}
	return nil
}
