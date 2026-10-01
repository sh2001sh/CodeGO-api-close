package boot

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

type MarketBatchConfig struct {
	GatewayBaseURL string
	SigningKey     []byte
	HTTPClient     *http.Client
}

// NewMarketBatchIdentity constructs only the normal key lifecycle service for
// workers, without requiring a session endpoint or a separate session secret.
func NewMarketBatchIdentity(pool *pgxpool.Pool, encodedSecret string, log *slog.Logger) (*identity.Control, error) {
	secret, err := base64.StdEncoding.DecodeString(encodedSecret)
	if pool == nil || err != nil || len(secret) != 32 {
		return nil, fmt.Errorf("market batch identity: %w", channelmarket.ErrUnavailable)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("market-batch-unused-session-secret"))
	return identity.NewControl(pool, identity.ControlConfig{EncryptionKey: secret, SessionSecret: mac.Sum(nil)}, log)
}

// NewMarketBatchRelay uses the ordinary authenticated gateway; it never probes
// an upstream directly or bypasses user policy, reservation or settlement.
func NewMarketBatchRelay(pool *pgxpool.Pool, keys *identity.Control, cfg MarketBatchConfig) (func(context.Context, channelmarket.BatchRelayRequest) (channelmarket.BatchReceipt, error), error) {
	endpoint, err := validateMarketBatchGatewayConfig(pool, keys, cfg)
	if err != nil {
		return nil, err
	}
	client := newMarketBatchHTTPClient(cfg)
	secret := bytes.Clone(cfg.SigningKey)
	return func(ctx context.Context, input channelmarket.BatchRelayRequest) (channelmarket.BatchReceipt, error) {
		receipt := channelmarket.BatchReceipt{RequestID: input.RequestID}
		if input.UserID <= 0 || input.Group == "" || input.Model == "" || !marketBatchID(input.RequestID) {
			return receipt, channelmarket.ErrInvalid
		}
		if err := claimMarketBatchDispatch(ctx, pool, input); err != nil {
			return receipt, err
		}
		keyID, err := dispatchMarketBatchRequest(ctx, pool, keys, client, endpoint, secret, input)
		if err != nil {
			return receipt, err
		}
		return waitMarketBatchReceipt(ctx, pool, input, keyID)
	}, nil
}

// validateMarketBatchGatewayConfig rejects any gateway base URL that could
// bypass the ordinary authenticated gateway (query/fragment/userinfo, or a
// path other than the server root), and returns the chat completions
// endpoint derived from it.
func validateMarketBatchGatewayConfig(pool *pgxpool.Pool, keys *identity.Control, cfg MarketBatchConfig) (string, error) {
	u, err := url.Parse(cfg.GatewayBaseURL)
	if pool == nil || keys == nil || len(cfg.SigningKey) < 32 || err != nil || u.Host == "" || u.Hostname() == "" ||
		(u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("market batch gateway configuration: %w", channelmarket.ErrUnavailable)
	}
	return strings.TrimRight(cfg.GatewayBaseURL, "/") + "/v1/chat/completions", nil
}

func newMarketBatchHTTPClient(cfg MarketBatchConfig) *http.Client {
	client := &http.Client{Timeout: 110 * time.Second}
	if cfg.HTTPClient != nil {
		*client = *cfg.HTTPClient
		if client.Timeout == 0 || client.Timeout > 110*time.Second {
			client.Timeout = 110 * time.Second
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

// claimMarketBatchDispatch commits the one dispatch before a key is
// generated or any request is sent, so a crash, uncertain response, or
// repeated callback cannot regenerate it.
func claimMarketBatchDispatch(ctx context.Context, pool *pgxpool.Pool, input channelmarket.BatchRelayRequest) error {
	tag, err := pool.Exec(ctx, `UPDATE v3_channelmarket.batch_test_items i SET dispatched_at=now()
	FROM v3_channelmarket.batch_tests b WHERE b.id=i.batch_id AND i.request_id=$1 AND b.owner_user_id=$2
	AND i.internal_group_name=$3 AND b.model=$4 AND i.status='running' AND i.dispatched_at IS NULL`, input.RequestID, input.UserID, input.Group, input.Model)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return channelmarket.ErrConflict
	}
	return nil
}

// dispatchMarketBatchRequest issues a short-lived scoped key, sends the
// signed probe request through the ordinary gateway, and revokes the key
// again regardless of outcome. It returns the key ID so the caller can look
// up the resulting usage log once the key no longer exists.
func dispatchMarketBatchRequest(ctx context.Context, pool *pgxpool.Pool, keys *identity.Control, client *http.Client,
	endpoint string, secret []byte, input channelmarket.BatchRelayRequest) (keyID int64, returned error) {
	expires := time.Now().UTC().Add(3 * time.Minute)
	key, raw, err := keys.CreateKey(ctx, input.UserID, identity.KeyInput{Name: "Marketplace batch test", Group: &input.Group, AllowedModels: []string{input.Model}, ExpiresAt: &expires})
	if err != nil {
		return 0, fmt.Errorf("market batch key issuance: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := keys.DeleteKey(cleanup, input.UserID, key.ID); err != nil {
			returned = errors.Join(returned, fmt.Errorf("market batch key revocation: %w", err))
		}
	}()
	if err := sendMarketBatchProbe(ctx, client, endpoint, secret, raw, input); err != nil {
		return 0, err
	}
	return key.ID, nil
}

// sendMarketBatchProbe sends the one signed, non-retried probe request and
// validates the gateway accepted it; it does not inspect the model's reply.
func sendMarketBatchProbe(ctx context.Context, client *http.Client, endpoint string, secret []byte, raw string, input channelmarket.BatchRelayRequest) error {
	body, err := json.Marshal(map[string]any{"model": input.Model, "messages": []map[string]string{{"role": "user", "content": "Reply OK."}}, "max_tokens": 16, "stream": false})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return channelmarket.ErrInvalid
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+raw)
	request.Header.Set("X-Market-Batch-Request-ID", input.RequestID)
	request.Header.Set("X-Market-Batch-Signature", marketBatchSignature(secret, input.RequestID, request.Header.Get("Authorization"), body))
	// No retry, including on timeout, 429, redirects or connection failure.
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("market batch gateway request failed: %w", channelmarket.ErrUnavailable)
	}
	readBytes, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || readBytes > 1<<20 {
		return fmt.Errorf("market batch gateway response incomplete: %w", channelmarket.ErrUnavailable)
	}
	if response.Header.Get("X-Request-Id") != input.RequestID {
		return fmt.Errorf("market batch gateway request ID mismatch: %w", channelmarket.ErrConflict)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("market batch gateway status %d: %w", response.StatusCode, channelmarket.ErrUnavailable)
	}
	return nil
}

func waitMarketBatchReceipt(ctx context.Context, pool *pgxpool.Pool, input channelmarket.BatchRelayRequest, keyID int64) (channelmarket.BatchReceipt, error) {
	receipt := channelmarket.BatchReceipt{RequestID: input.RequestID}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var amount *int64
		var completed bool
		err := pool.QueryRow(ctx, `SELECT sum(l.amount)::bigint,
		coalesce(string_agg(DISTINCT coalesce(s.billing_source,a.kind),'+' ORDER BY coalesce(s.billing_source,a.kind)),''),
		coalesce(bool_and(l.terminal IN ('completed','completed_no_usage')),false)
		FROM v3_billing.usage_logs l JOIN v3_billing.accounts a ON a.id=l.account_id
		LEFT JOIN v3_channelmarket.settlements s ON s.request_id=l.request_id
		WHERE l.request_id=$1 AND l.user_id=$2 AND l.model=$3 AND l.key_id=$4 AND a.kind IN ('wallet','subscription')`, input.RequestID, input.UserID, input.Model, keyID).Scan(&amount, &receipt.BillingSource, &completed)
		if err != nil {
			return receipt, err
		}
		if amount != nil {
			receipt.LogCreated, receipt.AmountMicro = true, *amount
			if !completed || *amount < 0 {
				return receipt, fmt.Errorf("market batch usage did not complete: %w", channelmarket.ErrConflict)
			}
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return receipt, fmt.Errorf("market batch settlement unavailable: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func marketBatchID(id string) bool {
	if !strings.HasPrefix(id, "market-test-") || len(id) != len("market-test-")+32 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "market-test-"))
	return err == nil && id == strings.ToLower(id)
}

func marketBatchSignature(secret []byte, id, authorization string, body []byte) string {
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%x", id, authorization, digest)
	return hex.EncodeToString(mac.Sum(nil))
}

// MarketBatchRequestID is a gateway Config.RequestID callback. A server-signed
// small POST may use its durable job ID; ordinary requests retain generated IDs.
// Signature grants no authentication, policy or billing privileges.
func MarketBatchRequestID(r *http.Request, secret []byte) string {
	if r == nil || r.URL == nil || len(secret) < 32 || r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Body == nil {
		return ""
	}
	id, signature := r.Header.Get("X-Market-Batch-Request-ID"), r.Header.Get("X-Market-Batch-Signature")
	if !marketBatchID(id) || len(signature) != 64 || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return ""
	}
	original := r.Body
	body, err := io.ReadAll(io.LimitReader(original, (1<<20)+1))
	r.Body = &marketBatchBody{Reader: io.MultiReader(bytes.NewReader(body), original), Closer: original}
	if err != nil || len(body) > 1<<20 {
		return ""
	}
	expected := marketBatchSignature(secret, id, r.Header.Get("Authorization"), body)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return ""
	}
	return id
}

type marketBatchBody struct {
	io.Reader
	io.Closer
}
