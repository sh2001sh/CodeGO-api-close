package http

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/assert"
)

func TestMultiplierCardSlotPolicy(t *testing.T) {
	tests := []struct {
		name        string
		zeroHour    bool
		monthlyPass bool
		keyPrefix   string
		cardName    string
		enabled     bool
	}{
		{name: "disabled"},
		{name: "zero hour", zeroHour: true, keyPrefix: "codego:zero-hour:concurrency", cardName: "0 倍率卡", enabled: true},
		{name: "monthly pass", monthlyPass: true, keyPrefix: "codego:monthly-pass:concurrency", cardName: "0.1 倍率卡", enabled: true},
		{name: "zero hour precedence", zeroHour: true, monthlyPass: true, keyPrefix: "codego:zero-hour:concurrency", cardName: "0 倍率卡", enabled: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			httpctx.SetContextKey(ctx, constant.ContextKeyZeroHourActive, test.zeroHour)
			httpctx.SetContextKey(ctx, constant.ContextKeyMonthlyPassActive, test.monthlyPass)

			keyPrefix, cardName, enabled := multiplierCardSlotPolicy(ctx)
			assert.Equal(t, test.keyPrefix, keyPrefix)
			assert.Equal(t, test.cardName, cardName)
			assert.Equal(t, test.enabled, enabled)
		})
	}
}
