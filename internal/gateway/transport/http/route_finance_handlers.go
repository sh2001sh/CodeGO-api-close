package http

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

type saveFundingPoliciesRequest struct {
	Policies []billingschema.FundingSourcePolicy `json:"policies"`
}

func GetFundingPolicies(c *gin.Context) {
	policies, err := billingapp.FundingAttributionPolicies()
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"items": policies})
}

func SaveFundingPolicies(c *gin.Context) {
	var request saveFundingPoliciesRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	if err := billingapp.SaveFundingAttributionPolicies(request.Policies); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func GetDailyFundingEconomics(c *gin.Context) {
	day := time.Now()
	if value := c.Query("date"); value != "" {
		parsed, err := time.ParseInLocation("2006-01-02", value, time.FixedZone("CST", 8*60*60))
		if err != nil {
			httpapi.ApiError(c, err)
			return
		}
		day = parsed
	}
	report, err := billingapp.DailyFundingEconomics(day, platformruntime.QuotaPerUnit)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, report)
}
