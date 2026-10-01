package commerce_test

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestJianPayRefundSignsIntegerAmountsAndPreservesProviderState(t *testing.T) {
	for _, state := range []struct {
		value int
		want  string
	}{{1, "processing"}, {2, "success"}, {3, "failed"}} {
		t.Run(state.want, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in map[string]any
				decoder := json.NewDecoder(r.Body)
				decoder.UseNumber()
				if err := decoder.Decode(&in); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				var keys, parts []string
				for k := range in {
					if k != "sign" && k != "sign_type" {
						keys = append(keys, k)
					}
				}
				sort.Strings(keys)
				for _, k := range keys {
					parts = append(parts, k+"="+fmt.Sprint(in[k]))
				}
				digest := md5.Sum([]byte(strings.Join(parts, "&") + "test-secret"))
				if in["sign"] != hex.EncodeToString(digest[:]) || in["clientNo"] != "merchant" || in["refundNo"] != "RFtest" {
					t.Error("wrong signed merchant request")
					w.WriteHeader(403)
					return
				}
				if strings.HasSuffix(r.URL.Path, "create") && (in["refundAmount"] != json.Number("294") || in["orderId"] != "remote-order") {
					t.Error("amount or provider order changed")
					w.WriteHeader(400)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 1000, "data": map[string]any{"refundId": "remote-refund", "refundNo": "RFtest", "refundAmount": 294, "status": state.value}})
			}))
			defer server.Close()
			provider := commerce.NewEpayRefunds(commerce.EpayConfig{BaseURL: server.URL, MerchantID: "merchant", Secret: "test-secret"}, server.Client())
			created, err := provider.CreateRefund(context.Background(), commerce.RefundPayment{OrderID: "remote-order", RefundNo: "RFtest", AmountMinor: 294})
			if err != nil || created.State != state.want || created.AmountMinor != 294 {
				t.Fatalf("create: %+v %v", created, err)
			}
			queried, err := provider.QueryRefund(context.Background(), created.RefundID, "RFtest")
			if err != nil || queried != created {
				t.Fatalf("query: %+v %v", queried, err)
			}
		})
	}
}

func TestJianPayRefundRejectsUnconfirmedAndMalformedResponses(t *testing.T) {
	for _, body := range []string{`{"code":403}`, `{"code":1000,"data":{"status":2}}`, `{"code":1000,"data":{"refundNo":"RFtest","refundAmount":2,"status":9}}`, `not-json`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		p := commerce.NewEpayRefunds(commerce.EpayConfig{BaseURL: server.URL, MerchantID: "merchant", Secret: "test-secret"}, server.Client())
		if _, err := p.CreateRefund(context.Background(), commerce.RefundPayment{OrderID: "remote-order", RefundNo: "RFtest", AmountMinor: 2}); err == nil {
			t.Errorf("accepted %q", body)
		}
		server.Close()
	}
}

func TestJianPayRefundDoesNotForwardSignedBodyOnRedirect(t *testing.T) {
	var forwarded atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { forwarded.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	p := commerce.NewEpayRefunds(commerce.EpayConfig{BaseURL: server.URL, MerchantID: "merchant", Secret: "test-secret"}, server.Client())
	if _, err := p.CreateRefund(context.Background(), commerce.RefundPayment{OrderID: "remote-order", RefundNo: "RFtest", AmountMinor: 2}); err == nil || forwarded.Load() != 0 {
		t.Fatalf("signed request followed redirect: forwarded=%d err=%v", forwarded.Load(), err)
	}
}
