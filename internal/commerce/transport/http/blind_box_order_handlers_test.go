package http

import (
	stdhttp "net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
)

func TestCancelBlindBoxOrderExpiresCurrentUsersPendingOrder(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	if err := db.AutoMigrate(&commerceschema.BlindBoxOrder{}); err != nil {
		t.Fatalf("failed to migrate blind box orders: %v", err)
	}
	order := &commerceschema.BlindBoxOrder{
		UserId: 702, Quantity: 2, Money: 5, TradeNo: "blind-box-http-cancel",
		PaymentMethod: "test", PaymentProvider: "test", Status: constant.TopUpStatusPending,
		CreateTime: time.Now().Unix(),
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("failed to seed blind box order: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodPost, "/api/blind-box/orders/blind-box-http-cancel/cancel", nil, 702)
	ctx.Params = gin.Params{{Key: "trade_no", Value: order.TradeNo}}
	cancelBlindBoxOrder(ctx)
	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected cancellation success, got %#v", response)
	}
	var saved commerceschema.BlindBoxOrder
	if err := db.First(&saved, order.Id).Error; err != nil {
		t.Fatalf("failed to reload blind box order: %v", err)
	}
	if saved.Status != constant.TopUpStatusExpired {
		t.Fatalf("expected expired order, got %q", saved.Status)
	}
}
