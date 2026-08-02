package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	bountyapp "github.com/sh2001sh/new-api/internal/bounty/app"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func listBounties(c *gin.Context) {
	result, err := bountyapp.ListTasks(int64(c.GetInt("id")), parseListRequest(c), c.GetInt("role"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func listMineBounties(c *gin.Context) {
	request := parseListRequest(c)
	if request.Scope == "" {
		request.Scope = "mine_published"
	}
	result, err := bountyapp.ListTasks(int64(c.GetInt("id")), request, c.GetInt("role"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func listBountyBalances(c *gin.Context) {
	result, err := bountyapp.ListBalances(int64(c.GetInt("id")))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func getBountyDetail(c *gin.Context) {
	result, err := bountyapp.GetTaskDetail(c.Param("id"), int64(c.GetInt("id")), c.GetInt("role"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func getBountyTimeline(c *gin.Context) {
	result, err := bountyapp.GetTimeline(c.Param("id"), int64(c.GetInt("id")), c.GetInt("role"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func createBounty(c *gin.Context) {
	var request bountyapp.CreateTaskRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	request.IdempotencyKey = firstNonEmpty(request.IdempotencyKey, c.GetHeader("Idempotency-Key"), c.GetHeader("X-Idempotency-Key"))
	result, err := bountyapp.CreateTask(int64(c.GetInt("id")), request)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func saveBountyDraft(c *gin.Context) {
	var request bountyapp.CreateTaskRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	request.IdempotencyKey = firstNonEmpty(request.IdempotencyKey, c.GetHeader("Idempotency-Key"), c.GetHeader("X-Idempotency-Key"))
	result, err := bountyapp.SaveDraft(int64(c.GetInt("id")), request)
	respondTaskAction(c, result, err)
}

func updateBountyDraft(c *gin.Context) {
	var request bountyapp.CreateTaskRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.UpdateDraft(int64(c.GetInt("id")), c.Param("id"), request)
	respondTaskAction(c, result, err)
}

func publishBountyDraft(c *gin.Context) {
	result, err := bountyapp.PublishDraft(int64(c.GetInt("id")), c.Param("id"))
	respondTaskAction(c, result, err)
}

func applyBounty(c *gin.Context) {
	var request bountyapp.CreateApplicationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.SubmitApplication(c.Param("id"), int64(c.GetInt("id")), request)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func assignBounty(c *gin.Context) {
	var request bountyapp.AssignApplicationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.AssignApplication(c.Param("id"), int64(c.GetInt("id")), request.ApplicationID)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func startBounty(c *gin.Context) {
	result, err := bountyapp.StartTask(c.Param("id"), int64(c.GetInt("id")))
	respondTaskAction(c, result, err)
}

func cancelBounty(c *gin.Context) {
	result, err := bountyapp.CancelTask(c.Param("id"), int64(c.GetInt("id")))
	respondTaskAction(c, result, err)
}

func createMaterialRequest(c *gin.Context) {
	var request bountyapp.MaterialRequestInput
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.CreateMaterialRequest(c.Param("id"), int64(c.GetInt("id")), request)
	respondTaskAction(c, result, err)
}

func replyMaterialRequest(c *gin.Context) {
	var request bountyapp.MaterialReplyInput
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.ReplyMaterialRequest(c.Param("id"), c.Param("request_id"), int64(c.GetInt("id")), request)
	respondTaskAction(c, result, err)
}

func resolveMaterialRequest(c *gin.Context) {
	result, err := bountyapp.ResolveMaterialRequest(c.Param("id"), c.Param("request_id"), int64(c.GetInt("id")))
	respondTaskAction(c, result, err)
}

func handleMaterialTimeout(c *gin.Context) {
	var request bountyapp.MaterialTimeoutInput
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.HandleMaterialTimeout(c.Param("id"), c.Param("request_id"), int64(c.GetInt("id")), request)
	respondTaskAction(c, result, err)
}

func submitBountyDelivery(c *gin.Context) {
	var request bountyapp.SubmissionInput
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.CreateSubmission(c.Param("id"), int64(c.GetInt("id")), request)
	respondTaskAction(c, result, err)
}

func reviewBounty(c *gin.Context) {
	var request bountyapp.ReviewInput
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.ReviewTask(c.Param("id"), int64(c.GetInt("id")), request)
	respondTaskAction(c, result, err)
}

func openBountyDispute(c *gin.Context) {
	var request bountyapp.DisputeInput
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.OpenDispute(c.Param("id"), int64(c.GetInt("id")), request)
	respondTaskAction(c, result, err)
}

func reportBounty(c *gin.Context) {
	var request bountyapp.ReportInput
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.ReportTask(c.Param("id"), int64(c.GetInt("id")), request)
	respondTaskAction(c, result, err)
}

func listAdminBounties(c *gin.Context) {
	result, err := bountyapp.ListAdminTasks(parseListRequest(c))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func listAdminBountyDisputes(c *gin.Context) {
	result, err := bountyapp.ListAdminDisputes()
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func listAdminBountyReports(c *gin.Context) {
	result, err := bountyapp.ListAdminReports()
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func resolveAdminBountyDispute(c *gin.Context) {
	var request bountyapp.AdminResolutionInput
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	result, err := bountyapp.ResolveDispute(c.Param("id"), int64(c.GetInt("id")), request)
	respondTaskAction(c, result, err)
}

func suspendBounty(c *gin.Context) {
	result, err := bountyapp.SuspendTask(c.Param("id"), int64(c.GetInt("id")))
	respondTaskAction(c, result, err)
}

func resumeBounty(c *gin.Context) {
	result, err := bountyapp.ResumeTask(c.Param("id"), int64(c.GetInt("id")))
	respondTaskAction(c, result, err)
}

func resolveBountyReport(c *gin.Context) {
	var request bountyapp.AdminReportResolutionInput
	if err := c.ShouldBindJSON(&request); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	if request.ReportID == "" {
		request.ReportID = c.Param("report_id")
	}
	if err := bountyapp.ResolveReport(c.Param("id"), int64(c.GetInt("id")), request); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, nil)
}

func listBountyNotifications(c *gin.Context) {
	result, err := bountyapp.ListNotifications(int64(c.GetInt("id")))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func markBountyNotificationRead(c *gin.Context) {
	if err := bountyapp.MarkNotificationRead(int64(c.GetInt("id")), c.Param("notification_id")); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, nil)
}

func markAllBountyNotificationsRead(c *gin.Context) {
	if err := bountyapp.MarkAllNotificationsRead(int64(c.GetInt("id"))); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, nil)
}

func parseListRequest(c *gin.Context) bountyapp.ListTasksRequest {
	page, _ := strconv.Atoi(c.DefaultQuery("page", c.DefaultQuery("p", "1")))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", c.DefaultQuery("pageSize", "20")))
	return bountyapp.ListTasksRequest{
		Scope:      c.Query("scope"),
		Keyword:    c.Query("keyword"),
		WalletType: c.Query("wallet_type"),
		Status:     c.Query("status"),
		Tag:        c.Query("tag"),
		MinReward:  parseInt64(c.Query("min_reward")),
		MaxReward:  parseInt64(c.Query("max_reward")),
		Sort:       c.Query("sort"),
		Page:       page,
		PageSize:   pageSize,
	}
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func respondTaskAction(c *gin.Context, result *bountyapp.TaskDetailView, err error) {
	if err != nil {
		if errors.Is(err, bountyapp.ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
