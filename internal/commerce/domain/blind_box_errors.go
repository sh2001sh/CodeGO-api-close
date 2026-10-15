package domain

import "errors"

var (
	ErrBlindBoxDisabled             = errors.New("blind box is disabled")
	ErrBlindBoxOrderNotFound        = errors.New("blind box order not found")
	ErrBlindBoxOrderStatusInvalid   = errors.New("blind box order status invalid")
	ErrBlindBoxInsufficientStock    = errors.New("blind box stock is insufficient")
	ErrBlindBoxInsufficientQuota    = errors.New("blind box quota insufficient")
	ErrBlindBoxSiteOpenLimitReached = errors.New("blind box daily open limit reached")
)
