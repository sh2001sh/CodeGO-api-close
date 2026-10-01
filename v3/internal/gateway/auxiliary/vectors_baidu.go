package auxiliary

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type vectorBaiduToken struct {
	value   string
	expires time.Time
}

// Native embeddings use the same OAuth contract as Wenxin Chat. The cache is
// local to the adapter and never emits credentials in a request error.
type vectorBaiduAdapter struct {
	Client   *http.Client
	TokenURL string
	mu       sync.Mutex
	tokens   map[string]vectorBaiduToken
}

func (a *vectorBaiduAdapter) Build(ctx context.Context, req *gateway.Request, target gateway.Target, in Input) (*http.Request, error) {
	if in.Operation != Embeddings {
		return nil, unsupported(in.Operation)
	}
	if req == nil {
		return nil, vectorInvalid("request is required")
	}
	fields, err := vectorFields(in.Body, "model", "input", "encoding_format")
	if err != nil {
		return nil, err
	}
	inputs, err := vectorStrings(fields["input"])
	if err != nil {
		return nil, err
	}
	if format := fields["encoding_format"]; len(format) > 0 && string(format) != `"float"` {
		return nil, vectorInvalid("Baidu only returns float embeddings")
	}
	model := upstreamModel(req, target)
	if model == "" || strings.ContainsAny(model, "/\\?#%") {
		return nil, vectorInvalid("invalid Baidu embedding model")
	}
	path := strings.ToLower(model)
	switch model {
	case "Embedding-V1":
		path = "embedding-v1"
	case "bge-large-zh":
		path = "bge_large_zh"
	case "bge-large-en":
		path = "bge_large_en"
	case "tao-8k":
		path = "tao_8k"
	}
	base := target.BaseURL
	if base == "" {
		base = "https://aip.baidubce.com"
	}
	address, err := endpoint(base, "/rpc/2.0/ai_custom/v1/wenxinworkshop/embeddings/"+path)
	if err != nil {
		return nil, err
	}
	token, err := a.accessToken(ctx, target.Secret)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	query.Set("access_token", token)
	u.RawQuery = query.Encode()
	return jsonRequest(ctx, u.String(), "", map[string]any{"input": inputs})
}

func (a *vectorBaiduAdapter) Decode(_ context.Context, req *gateway.Request, _ gateway.Target, in Input, resp *http.Response) (Response, error) {
	if in.Operation != Embeddings {
		return Response{}, unsupported(in.Operation)
	}
	data, err := readResponse(resp)
	if err != nil {
		return Response{}, err
	}
	if err = vectorResponseError(data, resp.StatusCode); err != nil {
		return Response{}, err
	}
	return decodeCompatibleVectors(req, data, resp.Header)
}

func (a *vectorBaiduAdapter) accessToken(ctx context.Context, secret string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	parts := strings.Split(secret, "|")
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], nil
	}
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", vectorInvalid("Baidu requires access_token or apiKey|secret")
	}
	address := a.TokenURL
	if address == "" {
		address = "https://aip.baidubce.com/oauth/2.0/token"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cacheKey := address + "\x00" + secret
	if entry := a.tokens[cacheKey]; time.Now().Before(entry.expires) {
		return entry.value, nil
	}
	result, err := fetchBaiduOAuthToken(ctx, a.Client, address, parts[0], parts[1])
	if err != nil {
		return "", err
	}
	lifetime := time.Duration(result.Expires) * time.Second
	if a.tokens == nil {
		a.tokens = make(map[string]vectorBaiduToken)
	}
	a.tokens[cacheKey] = vectorBaiduToken{value: result.Token, expires: time.Now().Add(lifetime - min(time.Minute, lifetime/10))}
	return result.Token, nil
}

type baiduOAuthResult struct {
	Token   string `json:"access_token"`
	Expires int64  `json:"expires_in"`
	Error   string `json:"error"`
}

// fetchBaiduOAuthToken performs the client-credentials OAuth exchange against
// Baidu's token endpoint and validates the returned token/expiry.
func fetchBaiduOAuthToken(ctx context.Context, client *http.Client, address, clientID, clientSecret string) (baiduOAuthResult, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return baiduOAuthResult{}, vectorInvalid("invalid Baidu OAuth endpoint")
	}
	query := u.Query()
	query.Set("grant_type", "client_credentials")
	query.Set("client_id", clientID)
	query.Set("client_secret", clientSecret)
	u.RawQuery = query.Encode()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return baiduOAuthResult{}, vectorInvalid("invalid Baidu OAuth request")
	}
	r.Header.Set("Content-Type", "application/json")
	if client == nil {
		copyClient := *upstreamClient(ctx)
		copyClient.Timeout = 15 * time.Second
		client = &copyClient
	}
	resp, err := client.Do(r)
	if err != nil {
		var network *url.Error
		if errors.As(err, &network) {
			err = network.Err
		}
		return baiduOAuthResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return baiduOAuthResult{}, err
	}
	if len(data) > 1<<20 {
		return baiduOAuthResult{}, vectorUpstream("oauth_failed", "Baidu OAuth response too large")
	}
	var result baiduOAuthResult
	if json.Unmarshal(data, &result) != nil || resp.StatusCode != http.StatusOK || result.Error != "" || result.Token == "" || result.Expires < 1 || result.Expires > 365*24*3600 {
		return baiduOAuthResult{}, vectorUpstream("oauth_failed", "Baidu OAuth exchange failed")
	}
	return result, nil
}
