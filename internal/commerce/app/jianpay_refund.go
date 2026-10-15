package app

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
)

type jianPayRefundResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		RefundID     string `json:"refundId"`
		RefundNo     string `json:"refundNo"`
		OrderID      string `json:"orderId"`
		RefundAmount int64  `json:"refundAmount"`
		Status       int    `json:"status"`
		StatusText   string `json:"statusText"`
		ErrorMessage string `json:"errorMessage"`
	} `json:"data"`
}

type JianPayRefundResult struct {
	RefundID     string
	RefundNo     string
	RefundAmount int64
	Status       int
	StatusText   string
	ErrorMessage string
}

func JianPayRefundCreate(orderID, refundNo string, amountCents int64, reason string) (*JianPayRefundResult, error) {
	return callJianPayRefund("/open/payment/refund/create", map[string]any{
		"clientNo":     commercestore.EpayId,
		"orderId":      orderID,
		"refundNo":     refundNo,
		"refundAmount": amountCents,
		"reason":       reason,
	})
}

func JianPayRefundQuery(refundID, refundNo string) (*JianPayRefundResult, error) {
	params := map[string]any{"clientNo": commercestore.EpayId}
	if strings.TrimSpace(refundID) != "" {
		params["refundId"] = refundID
	}
	if strings.TrimSpace(refundNo) != "" {
		params["refundNo"] = refundNo
	}
	return callJianPayRefund("/open/payment/refund/query", params)
}

func callJianPayRefund(endpoint string, params map[string]any) (*JianPayRefundResult, error) {
	if strings.TrimSpace(commercestore.PayAddress) == "" || strings.TrimSpace(commercestore.EpayId) == "" || strings.TrimSpace(commercestore.EpayKey) == "" {
		return nil, errors.New("JianPay payment settings are incomplete")
	}
	params["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	params["sign_type"] = "MD5"
	params["sign"] = signJianPayParams(params, commercestore.EpayKey)
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(strings.TrimRight(commercestore.PayAddress, "/"))
	if err != nil {
		return nil, err
	}
	base.Path = strings.TrimRight(base.Path, "/") + endpoint
	req, err := http.NewRequest(http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("JianPay refund HTTP %d", resp.StatusCode)
	}
	var payload jianPayRefundResponse
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return nil, err
	}
	if payload.Code != 1000 {
		return nil, errors.New(payload.Message)
	}
	return &JianPayRefundResult{
		RefundID: payload.Data.RefundID, RefundNo: payload.Data.RefundNo,
		RefundAmount: payload.Data.RefundAmount, Status: payload.Data.Status,
		StatusText: payload.Data.StatusText, ErrorMessage: payload.Data.ErrorMessage,
	}, nil
}

func signJianPayParams(params map[string]any, key string) string {
	keys := make([]string, 0, len(params))
	for name, value := range params {
		if name == "sign" || name == "sign_type" || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, name := range keys {
		value := params[name]
		encoded, ok := value.(string)
		if !ok {
			data, _ := json.Marshal(value)
			encoded = string(data)
		}
		parts = append(parts, name+"="+encoded)
	}
	digest := md5.Sum([]byte(strings.Join(parts, "&") + key))
	return hex.EncodeToString(digest[:])
}
