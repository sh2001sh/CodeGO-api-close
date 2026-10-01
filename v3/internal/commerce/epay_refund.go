package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// JianPay's refund API is available only when the configured Epay merchant
// actually supports it. The adapter never treats a generic Epay checkout as a
// successful refund without the authenticated Create/Query response.
type EpayRefunds struct {
	cfg    EpayConfig
	client *http.Client
	now    func() time.Time
}

func NewEpayRefunds(cfg EpayConfig, client *http.Client) *EpayRefunds {
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout <= 0 || copyClient.Timeout > 15*time.Second {
		copyClient.Timeout = 15 * time.Second
	}
	return &EpayRefunds{cfg: cfg, client: &copyClient, now: time.Now}
}

func (p *EpayRefunds) CreateRefund(ctx context.Context, in RefundPayment) (RefundProviderResult, error) {
	if in.OrderID == "" || in.RefundNo == "" || in.AmountMinor <= 0 {
		return RefundProviderResult{}, ErrInvalid
	}
	return p.call(ctx, "/open/payment/refund/create", map[string]any{"clientNo": p.cfg.MerchantID, "orderId": in.OrderID, "refundNo": in.RefundNo,
		"refundAmount": in.AmountMinor, "reason": "用户申请未使用额度退款（扣除2%手续费）"})
}

func (p *EpayRefunds) QueryRefund(ctx context.Context, id, no string) (RefundProviderResult, error) {
	if id == "" && no == "" {
		return RefundProviderResult{}, ErrInvalid
	}
	params := map[string]any{"clientNo": p.cfg.MerchantID}
	if id != "" {
		params["refundId"] = id
	}
	if no != "" {
		params["refundNo"] = no
	}
	return p.call(ctx, "/open/payment/refund/query", params)
}

func (p *EpayRefunds) call(ctx context.Context, path string, params map[string]any) (RefundProviderResult, error) {
	if p.cfg.BaseURL == "" || p.cfg.MerchantID == "" || p.cfg.Secret == "" {
		return RefundProviderResult{}, ErrProviderUnavailable
	}
	req, err := p.buildRefundRequest(ctx, path, params)
	if err != nil {
		return RefundProviderResult{}, err
	}
	response, err := p.client.Do(req)
	if err != nil {
		return RefundProviderResult{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return RefundProviderResult{}, fmt.Errorf("JianPay refund HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return RefundProviderResult{}, err
	}
	if len(data) > 1<<20 {
		return RefundProviderResult{}, ErrInvalid
	}
	return parseEpayRefundReply(data)
}

// buildRefundRequest resolves the signed target URL and request body for a
// JianPay refund API call.
func (p *EpayRefunds) buildRefundRequest(ctx context.Context, path string, params map[string]any) (*http.Request, error) {
	base, err := url.Parse(p.cfg.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil {
		return nil, ErrInvalid
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = ""
	base.Fragment = ""
	params["timestamp"] = strconv.FormatInt(p.now().Unix(), 10)
	values := url.Values{}
	for key, value := range params {
		values.Set(key, fmt.Sprint(value))
	}
	params["sign"] = epaySign(values, p.cfg.Secret)
	params["sign_type"] = "MD5"
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// parseEpayRefundReply validates the JianPay response envelope and maps its
// numeric status onto the provider-neutral processing/success/failed state.
func parseEpayRefundReply(data []byte) (RefundProviderResult, error) {
	var payload struct {
		Code int `json:"code"`
		Data struct {
			RefundID string `json:"refundId"`
			RefundNo string `json:"refundNo"`
			Amount   int64  `json:"refundAmount"`
			Status   int    `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return RefundProviderResult{}, err
	}
	if payload.Code != 1000 {
		return RefundProviderResult{}, fmt.Errorf("JianPay refund rejected with code %d", payload.Code)
	}
	state := "processing"
	switch payload.Data.Status {
	case 2:
		state = "success"
	case 3:
		state = "failed"
	case 0, 1:
	default:
		return RefundProviderResult{}, ErrInvalid
	}
	if payload.Data.RefundNo == "" || payload.Data.Amount <= 0 {
		return RefundProviderResult{}, ErrInvalid
	}
	return RefundProviderResult{RefundNo: payload.Data.RefundNo, RefundID: payload.Data.RefundID, AmountMinor: payload.Data.Amount, State: state}, nil
}
