package http

import (
	"strconv"
	"strings"
	"time"

	"fmt"
	"github.com/gin-gonic/gin"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func ListGroups(c *gin.Context) {
	query := marketplaceapp.GroupQuery{
		ViewerUserID: c.GetInt("id"),
		Search:       c.Query("search"), Model: c.Query("model"), Source: c.Query("source"),
		Provider: c.Query("provider"), Status: c.Query("status"),
		IncludeAccess: c.Query("include_access") == "true",
		Verification:  c.Query("verification"), Sort: c.Query("sort"), Direction: c.Query("direction"),
		WindowHours: queryInt(c, "window_hours", 24), Page: queryInt(c, "page", 1),
		PageSize: queryInt(c, "page_size", 20), MinMultiplier: queryFloat(c, "min_multiplier"),
		MaxMultiplier: queryFloat(c, "max_multiplier"),
	}
	result, err := marketplaceapp.ListMarketplaceGroups(query)
	respond(c, result, err)
}

func ListMultiplierTrends(c *gin.Context) {
	result, err := marketplaceapp.ListMultiplierTrends(marketplaceapp.MultiplierTrendQuery{
		RangeHours: queryInt(c, "range_hours", 24),
		Model:      c.Query("model"),
	})
	respond(c, result, err)
}

func GetGroup(c *gin.Context) {
	result, err := marketplaceapp.GetMarketplaceGroup(c.Param("slug"), queryInt(c, "window_hours", 24), c.GetInt("id"))
	respond(c, result, err)
}

func SubmitChannelFeedback(c *gin.Context) {
	var req marketplaceapp.ChannelFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.SubmitChannelFeedback(c.GetInt("id"), c.Param("id"), req)
	respond(c, result, err)
}

func GetAutoRoutePool(c *gin.Context) {
	result, err := marketplaceapp.ListAutoRoutePool(c.GetInt("id"))
	respond(c, result, err)
}

func UpdateAutoRoutePool(c *gin.Context) {
	var req marketplaceapp.AutoRoutePoolUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.ReplaceAutoRoutePool(c.GetInt("id"), req)
	respond(c, result, err)
}

func ListRoutePools(c *gin.Context) {
	result, err := marketplaceapp.ListRoutePools(c.GetInt("id"))
	respond(c, result, err)
}

func CreateRoutePool(c *gin.Context) {
	var req marketplaceapp.RoutePoolCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.CreateRoutePool(c.GetInt("id"), req)
	respond(c, result, err)
}

func GetRoutePool(c *gin.Context) {
	result, err := marketplaceapp.ListRoutePool(c.GetInt("id"), c.Param("id"))
	respond(c, result, err)
}

func UpdateRoutePool(c *gin.Context) {
	var req marketplaceapp.RoutePoolUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.UpdateRoutePool(c.GetInt("id"), c.Param("id"), req)
	respond(c, result, err)
}

func DeleteRoutePool(c *gin.Context) {
	respond(c, nil, marketplaceapp.DeleteRoutePool(c.GetInt("id"), c.Param("id")))
}

func StartBatchTest(c *gin.Context) {
	var req marketplaceapp.BatchTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.StartBatchMarketplaceTest(c.GetInt("id"), req)
	respond(c, result, err)
}

func GetBatchTest(c *gin.Context) {
	result, err := marketplaceapp.GetBatchMarketplaceTest(c.GetInt("id"), c.Param("id"))
	respond(c, result, err)
}

func CreateChannel(c *gin.Context) {
	var req marketplaceapp.CreateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.CreateMarketplaceChannel(c.GetInt("id"), req)
	respond(c, result, err)
}

func FetchModels(c *gin.Context) {
	var req marketplaceapp.FetchModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	models, err := marketplaceapp.FetchUpstreamModels(req)
	respond(c, models, err)
}

func ListMyChannels(c *gin.Context) {
	result, err := marketplaceapp.ListOwnerChannels(c.GetInt("id"))
	respond(c, result, err)
}

func ListMyUsageLogs(c *gin.Context) {
	result, err := marketplaceapp.ListOwnerUsageLogs(c.GetInt("id"), marketplaceapp.OwnerUsageLogQuery{
		ChannelID:         c.Query("channel_id"),
		Status:            c.Query("status"),
		ModelName:         c.Query("model_name"),
		RequestID:         c.Query("request_id"),
		UpstreamRequestID: c.Query("upstream_request_id"),
		ExternalUserID:    c.Query("external_user_id"),
		Search:            c.Query("search"),
		StartTimestamp:    queryInt64(c, "start_timestamp"),
		EndTimestamp:      queryInt64(c, "end_timestamp"),
		Page:              queryInt(c, "page", 1),
		PageSize:          queryInt(c, "page_size", 20),
	})
	respond(c, result, err)
}

func ListMyObservability(c *gin.Context) {
	result, err := marketplaceapp.ListMarketplaceObservability(c.GetInt("id"), queryInt64(c, "start_timestamp"), queryInt64(c, "end_timestamp"))
	respond(c, result, err)
}
func ListMyChannelUserUsage(c *gin.Context) {
	r, e := marketplaceapp.ListOwnerChannelUserUsage(c.GetInt("id"), marketplaceapp.OwnerUserUsageQuery{ChannelID: c.Query("channel_id"), Page: queryInt(c, "page", 1), PageSize: queryInt(c, "page_size", 20)})
	respond(c, r, e)
}
func SetUserMultiplier(c *gin.Context) {
	var r struct {
		UserID     int      `json:"user_id"`
		Multiplier *float64 `json:"multiplier"`
	}
	if e := c.ShouldBindJSON(&r); e != nil {
		respond(c, nil, e)
		return
	}
	if e := marketplaceapp.EnsureOwnerChannel(c.GetInt("id"), c.Param("id")); e != nil {
		respond(c, nil, e)
		return
	}
	e := marketplaceapp.SetUserMultiplier(c.GetInt("id"), c.Param("id"), r.UserID, r.Multiplier)
	respond(c, gin.H{"multiplier": r.Multiplier}, e)
}
func GetUserUsageTimeSeries(c *gin.Context) {
	r, e := marketplaceapp.UserUsageTimeSeries(c.GetInt("id"), c.Param("id"), c.Param("userId"), queryInt(c, "range_hours", 24))
	respond(c, r, e)
}
func BatchWelfare(c *gin.Context) {
	if e := marketplaceapp.EnsureOwnerChannel(c.GetInt("id"), c.Param("id")); e != nil {
		respond(c, nil, e)
		return
	}
	var r struct {
		UserIDs []string `json:"user_ids"`
		Type    string   `json:"type"`
		Amount  int64    `json:"amount"`
	}
	if e := c.ShouldBindJSON(&r); e != nil {
		respond(c, nil, e)
		return
	}
	if r.Type == "blind_box" {
		if r.Amount <= 0 {
			respond(c, nil, fmt.Errorf("盲盒数量必须大于 0"))
			return
		}
		d := make([]any, 0, len(r.UserIDs))
		success := 0
		for _, externalID := range r.UserIDs {
			e := commerceapp.GiftBlindBoxPropsBatch(c.GetInt("id"), externalID, int(r.Amount), fmt.Sprintf("marketplace-welfare-blindbox:%s:%s:%d", c.Param("id"), externalID, r.Amount))
			if e != nil {
				d = append(d, gin.H{"user_id": externalID, "status": "failed", "error": e.Error()})
			} else {
				success++
				d = append(d, gin.H{"user_id": externalID, "status": "success"})
			}
		}
		respond(c, gin.H{"success_count": success, "failed_count": len(d) - success, "details": d}, nil)
		return
	}
	if r.Type != "transfer" || r.Amount <= 0 {
		respond(c, nil, fmt.Errorf("仅支持有效额度转账"))
		return
	}
	d := make([]any, 0, len(r.UserIDs))
	success := 0
	for _, id := range r.UserIDs {
		uid, e := strconv.Atoi(id)
		if e != nil || uid <= 0 {
			d = append(d, gin.H{"user_id": id, "status": "failed", "error": "invalid user id"})
			continue
		}
		_, e = billingapp.GrantBonusWalletQuotaTx(nil, uid, r.Amount, "marketplace_welfare", c.Param("id"), fmt.Sprintf("marketplace-welfare:%s:%d:%d", c.Param("id"), uid, time.Now().UnixNano()))
		if e != nil {
			d = append(d, gin.H{"user_id": id, "status": "failed", "error": e.Error()})
		} else {
			success++
			d = append(d, gin.H{"user_id": id, "status": "success"})
		}
	}
	respond(c, gin.H{"success_count": success, "failed_count": len(d) - success, "details": d}, nil)
}
func ListTimeRangeMultipliers(c *gin.Context) {
	r, e := marketplaceapp.ListTimeRangeMultipliers(c.GetInt("id"), c.Param("id"))
	respond(c, r, e)
}
func CreateTimeRangeMultiplier(c *gin.Context) {
	var x struct {
		StartTimestamp, EndTimestamp int64
		Multiplier                   float64
		Label                        string
	}
	if e := c.ShouldBindJSON(&x); e != nil {
		respond(c, nil, e)
		return
	}
	r, e := marketplaceapp.CreateTimeRangeMultiplier(c.GetInt("id"), c.Param("id"), x.StartTimestamp, x.EndTimestamp, x.Multiplier, x.Label)
	respond(c, r, e)
}
func DeleteTimeRangeMultiplier(c *gin.Context) {
	respond(c, gin.H{"success": true}, marketplaceapp.DeleteTimeRangeMultiplier(c.GetInt("id"), c.Param("id"), c.Param("ruleId")))
}
func CreateBargainRequest(c *gin.Context) {
	var x struct {
		ProposedMultiplier float64 `json:"proposed_multiplier"`
		Reason             string  `json:"reason"`
	}
	if e := c.ShouldBindJSON(&x); e != nil {
		respond(c, nil, e)
		return
	}
	r, e := marketplaceapp.CreateBargainRequest(c.GetInt("id"), c.Param("id"), x.ProposedMultiplier, x.Reason)
	respond(c, r, e)
}
func ListMyBargainRequests(c *gin.Context) {
	r, e := marketplaceapp.ListOwnerBargainRequests(c.GetInt("id"), map[string]string{"status": c.Query("status"), "group_id": c.Query("group_id")}, queryInt(c, "page", 1), queryInt(c, "page_size", 20))
	respond(c, r, e)
}
func ResolveMyBargainRequest(c *gin.Context) {
	var x struct {
		Action         string `json:"action"`
		ResolutionNote string `json:"resolution_note"`
	}
	if e := c.ShouldBindJSON(&x); e != nil {
		respond(c, nil, e)
		return
	}
	r, e := marketplaceapp.ResolveOwnerBargainRequest(c.GetInt("id"), c.Param("id"), x.Action, x.ResolutionNote)
	respond(c, r, e)
}

func UpdateChannel(c *gin.Context) {
	var req marketplaceapp.UpdateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.UpdateOwnerChannel(c.GetInt("id"), c.Param("id"), req)
	respond(c, result, err)
}

func DeleteChannel(c *gin.Context) {
	respond(c, gin.H{"deleted": true}, marketplaceapp.DeleteOwnerChannel(c.GetInt("id"), c.Param("id")))
}

func VerifyChannel(c *gin.Context) {
	queueOwnedChannelAction(c, marketplaceapp.QueueRequiredVerification)
}

func DetectChannel(c *gin.Context) {
	queueOwnedChannelAction(c, marketplaceapp.QueueGPT56MappingVerification)
}

func TestChannelConnectivity(c *gin.Context) {
	queueOwnedChannelAction(c, marketplaceapp.QueueConnectivityTest)
}

func RetryFailedChannelConnectivity(c *gin.Context) {
	queueOwnedChannelAction(c, marketplaceapp.QueueFailedConnectivityTests)
}

func RemoveFailedChannelModel(c *gin.Context) {
	var req marketplaceapp.RemoveFailedModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.RemoveOwnerFailedChannelModel(c.GetInt("id"), c.Param("id"), req.Model)
	respond(c, result, err)
}

func PauseChannelVerification(c *gin.Context) {
	respond(
		c,
		gin.H{"paused": true},
		marketplaceapp.PauseOwnerChannelVerification(c.GetInt("id"), c.Param("id")),
	)
}

func queueOwnedChannelAction(c *gin.Context, queue func(string) error) {
	channels, err := marketplaceapp.ListOwnerChannels(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	for _, channel := range channels {
		if channel.ID == c.Param("id") {
			if err := queue(channel.ID); err != nil {
				httpapi.ApiError(c, err)
				return
			}
			httpapi.ApiSuccess(c, gin.H{"queued": true})
			return
		}
	}
	httpapi.ApiErrorMsg(c, "渠道不存在或无权限")
}

func PauseChannel(c *gin.Context) {
	respond(c, gin.H{"paused": true}, marketplaceapp.PauseOwnerChannel(c.GetInt("id"), c.Param("id"), true))
}

func ResumeChannel(c *gin.Context) {
	respond(c, gin.H{"resumed": true}, marketplaceapp.PauseOwnerChannel(c.GetInt("id"), c.Param("id"), false))
}

func SetChannelUserBlock(c *gin.Context) {
	var req struct {
		UserID  int  `json:"user_id"`
		Blocked bool `json:"blocked"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respond(c, nil, err)
		return
	}
	respond(c, gin.H{"blocked": req.Blocked}, marketplaceapp.SetChannelUserBlock(c.GetInt("id"), c.Param("id"), req.UserID, req.Blocked))
}

func BindToken(c *gin.Context) {
	var req marketplaceapp.TokenBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	tokenID, err := marketplaceapp.BindTokenToMarketplaceGroupResult(c.GetInt("id"), req.TokenID, c.Param("id"))
	respond(c, gin.H{"token_id": tokenID, "group_id": c.Param("id")}, err)
}

func CreateGroupInvite(c *gin.Context) {
	result, err := marketplaceapp.CreateMarketplaceGroupInvite(c.GetInt("id"), c.Param("id"))
	respond(c, result, err)
}

func AcceptGroupInvite(c *gin.Context) {
	var req marketplaceapp.GroupInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.AcceptMarketplaceGroupInvite(c.GetInt("id"), req.Token)
	respond(c, result, err)
}

func ListAdminChannels(c *gin.Context) {
	result, err := marketplaceapp.ListAdminChannels(marketplaceapp.AdminChannelQuery{
		Search:         c.Query("search"),
		Status:         c.Query("status"),
		Source:         c.Query("source"),
		Provider:       c.Query("provider"),
		Verification:   c.Query("verification"),
		MappingStatus:  c.Query("mapping_status"),
		OwnerSearch:    c.Query("owner_search"),
		StartTimestamp: queryInt64(c, "start_timestamp"),
		EndTimestamp:   queryInt64(c, "end_timestamp"),
	})
	respond(c, result, err)
}

func ListAdminOwnerIncome(c *gin.Context) {
	result, err := marketplaceapp.ListAdminOwnerIncome(marketplaceapp.AdminOwnerIncomeQuery{
		OwnerSearch:    c.Query("owner_search"),
		StartTimestamp: queryInt64(c, "start_timestamp"),
		EndTimestamp:   queryInt64(c, "end_timestamp"),
	})
	respond(c, result, err)
}

func ReleaseAdminOwnerIncome(c *gin.Context) {
	result, err := marketplaceapp.ReleaseAdminOwnerIncome(marketplaceapp.AdminOwnerIncomeQuery{
		OwnerSearch: c.Query("owner_search"), OwnerUserIDs: queryIntList(c, "owner_user_ids"), StartTimestamp: queryInt64(c, "start_timestamp"),
		EndTimestamp: queryInt64(c, "end_timestamp"),
	})
	respond(c, result, err)
}

func UpdateAdminChannel(c *gin.Context) {
	var req marketplaceapp.AdminUpdateChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.UpdateAdminChannel(c.Param("id"), req)
	respond(c, result, err)
}

func VerifyAdminChannel(c *gin.Context) {
	queueAdminChannelAction(c, marketplaceapp.QueueRequiredVerification)
}

func DetectAdminChannel(c *gin.Context) {
	queueAdminChannelAction(c, marketplaceapp.QueueGPT56MappingVerification)
}

func TestAdminChannelConnectivity(c *gin.Context) {
	queueAdminChannelAction(c, marketplaceapp.QueueConnectivityTest)
}

func RetryFailedAdminChannelConnectivity(c *gin.Context) {
	queueAdminChannelAction(c, marketplaceapp.QueueFailedConnectivityTests)
}

func RemoveFailedAdminChannelModel(c *gin.Context) {
	var req marketplaceapp.RemoveFailedModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.RemoveAdminFailedChannelModel(c.Param("id"), req.Model)
	respond(c, result, err)
}

func PauseAdminChannelVerification(c *gin.Context) {
	respond(c, gin.H{"paused": true}, marketplaceapp.PauseChannelVerification(c.Param("id")))
}

func PauseAdminChannel(c *gin.Context) {
	respond(c, gin.H{"paused": true}, marketplaceapp.PauseAdminChannel(c.Param("id"), true))
}
func ResumeAdminChannel(c *gin.Context) {
	respond(c, gin.H{"resumed": true}, marketplaceapp.PauseAdminChannel(c.Param("id"), false))
}

func queueAdminChannelAction(c *gin.Context, queue func(string) error) {
	if err := queue(c.Param("id")); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"queued": true})
}

func DeleteAdminChannel(c *gin.Context) {
	respond(c, gin.H{"deleted": true}, marketplaceapp.DeleteAdminChannel(c.Param("id")))
}

func ReviewChannel(c *gin.Context) {
	var req marketplaceapp.AdminReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	result, err := marketplaceapp.ReviewChannel(c.Param("id"), req)
	respond(c, result, err)
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

func queryInt(c *gin.Context, name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(name)))
	if err != nil {
		return fallback
	}
	return value
}

func queryFloat(c *gin.Context, name string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(c.Query(name)), 64)
	return value
}

func queryInt64(c *gin.Context, name string) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(c.Query(name)), 10, 64)
	return value
}

func queryIntList(c *gin.Context, name string) []int {
	values := strings.Split(c.Query(name), ",")
	result := make([]int, 0, len(values))
	for _, value := range values {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed > 0 {
			result = append(result, parsed)
		}
	}
	return result
}
