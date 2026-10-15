package app

import (
	"errors"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"strings"
)

// CreatePendingBlindBoxOrder persists a pending blind-box order.
func CreatePendingBlindBoxOrder(order *commerceschema.BlindBoxOrder) error {
	if order == nil {
		return errors.New("order is nil")
	}
	if order.UserId <= 0 || order.Quantity <= 0 || strings.TrimSpace(order.TradeNo) == "" {
		return errors.New("invalid blind box order")
	}
	if order.CreateTime == 0 {
		order.CreateTime = platformruntime.GetTimestamp()
	}
	return platformdb.DB.Create(order).Error
}
