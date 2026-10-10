package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Sample struct {
	RequestID string          `json:"request_id"`
	UserID    int64           `json:"user_id"`
	Model     string          `json:"model"`
	CreatedAt time.Time       `json:"created_at"`
	Request   json.RawMessage `json:"request"`
	Response  json.RawMessage `json:"response"`
}

// ShouldSample makes a stable decision across retries and gateway instances.
// Sampling is disabled by default and never consumes randomness on a request.
func (s *Service) ShouldSample(requestID string) bool {
	if requestID == "" || s.cfg.SampleRatePPM == 0 {
		return false
	}
	digest := sha256.Sum256([]byte(requestID))
	return int(binary.BigEndian.Uint32(digest[:4])%1_000_000) < s.cfg.SampleRatePPM
}

// RecordSample is an opt-in control/background operation. A gateway should
// enqueue it after settlement rather than add a synchronous PG round trip.
// Credentials are removed even when present under nested JSON objects.
func (s *Service) RecordSample(ctx context.Context, sample Sample) error {
	if !s.ShouldSample(sample.RequestID) {
		return nil
	}
	if len(sample.RequestID) > 256 || sample.UserID <= 0 || len(sample.Model) > 256 || sample.CreatedAt.IsZero() {
		return ErrInvalid
	}
	request, err := redact(sample.Request, s.cfg.MaxSampleBytes)
	if err != nil {
		return err
	}
	response, err := redact(sample.Response, s.cfg.MaxSampleBytes)
	if err != nil {
		return err
	}
	if s.pool == nil {
		return ErrUnavailable
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO v3_audit.request_samples
 (request_id, user_id, model, created_at, request_body, response_body)
 VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (request_id) DO NOTHING`, sample.RequestID,
		sample.UserID, sample.Model, sample.CreatedAt, request, response)
	return err
}

func redact(raw json.RawMessage, max int) ([]byte, error) {
	if len(raw) == 0 {
		return []byte("null"), nil
	}
	if len(raw) > max {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, ErrInvalid
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}
	redactValue(value)
	result, err := json.Marshal(value)
	if err != nil || len(result) > max {
		return nil, ErrInvalid
	}
	return result, nil
}

func redactValue(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			normal := strings.ReplaceAll(strings.ToLower(key), "-", "_")
			switch normal {
			case "authorization", "proxy_authorization", "cookie", "set_cookie", "api_key", "apikey", "x_api_key", "x_goog_api_key", "key", "token", "secret", "credential", "password", "password_hash", "passwordhash", "access_token", "accesstoken", "refresh_token", "refreshtoken", "client_secret", "clientsecret", "key_ciphertext":
				v[key] = "[redacted]"
			default:
				redactValue(child)
			}
		}
	case []any:
		for _, child := range v {
			redactValue(child)
		}
	}
}

func (s *Service) sampleHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	if !p.Admin || p.KeyID > 0 {
		reportError(w, ErrForbidden)
		return
	}
	id := r.PathValue("request")
	if id == "" || len(id) > 256 {
		reportError(w, ErrInvalid)
		return
	}
	if s.pool == nil {
		reportError(w, ErrUnavailable)
		return
	}
	var sample Sample
	err := s.pool.QueryRow(r.Context(), `SELECT request_id, user_id, model, created_at,
 request_body, response_body FROM v3_audit.request_samples WHERE request_id=$1`, id).Scan(
		&sample.RequestID, &sample.UserID, &sample.Model, &sample.CreatedAt, &sample.Request, &sample.Response)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Request sample does not exist")
		return
	}
	if err != nil {
		reportError(w, err)
		return
	}
	writeData(w, sample)
}

func (s *Service) DeleteSamplesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, ErrInvalid
	}
	if s.pool == nil {
		return 0, ErrUnavailable
	}
	var deleted int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout='200ms'; SET LOCAL statement_timeout='5s'"); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `WITH expired AS(SELECT s.request_id FROM v3_audit.request_samples s WHERE s.created_at<$1 AND NOT `+pendingRequest("s")+` AND NOT EXISTS(SELECT 1 FROM v3_audit.request_audits r WHERE r.request_id=s.request_id AND (`+terminalAudit("r")+`) IS NOT TRUE) ORDER BY s.created_at LIMIT 5000 FOR UPDATE OF s SKIP LOCKED) DELETE FROM v3_audit.request_samples s USING expired e WHERE s.request_id=e.request_id`, cutoff)
		deleted = tag.RowsAffected()
		return err
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}
