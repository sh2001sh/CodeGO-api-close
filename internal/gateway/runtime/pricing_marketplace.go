package runtime

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
)

func requiredMarketplaceImagePrice(c *gin.Context, modelName string) (float64, bool, error) {
	if httpctx.GetContextKeyString(c, constant.ContextKeyMarketplaceGroupID) == "" ||
		!gatewaycontract.IsImageGenerationModel(modelName) {
		return 0, false, nil
	}
	price, ok := marketplaceChannelModelPrice(c, modelName)
	if !ok || price.EffectiveBillingMode() != marketplacedomain.ChannelBillingModePerCall || price.PricePerCall <= 0 {
		return 0, true, fmt.Errorf("市场生图模型 %s 未配置有效的按次价格", modelName)
	}
	return price.PricePerCall, true, nil
}
