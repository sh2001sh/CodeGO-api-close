package app

import (
	"context"
	"errors"
	"fmt"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	"math"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformmath "github.com/sh2001sh/new-api/internal/platform/mathx"
	workflowdomain "github.com/sh2001sh/new-api/internal/workflow/domain"
	workflowschema "github.com/sh2001sh/new-api/internal/workflow/schema"
	"gorm.io/gorm"
)

func taskBillingModelName(task *workflowschema.Task) string {
	if billingContext := task.PrivateData.BillingContext; billingContext != nil && billingContext.OriginModelName != "" {
		return billingContext.OriginModelName
	}
	return task.Properties.OriginModelName
}

func taskBillingIsSubscription(task *workflowschema.Task) bool {
	return task.PrivateData.BillingSource == "subscription" && task.PrivateData.SubscriptionId > 0
}

func taskBillingResolveTokenKey(ctx context.Context, tokenID int, taskID string) string {
	token, err := billingapp.GetTokenByID(tokenID)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenID, taskID, err.Error()))
		return ""
	}
	return token.Key
}

func taskBillingOperationKey(task *workflowschema.Task) string {
	if task.ID > 0 {
		return fmt.Sprintf("async-task:%d:terminal", task.ID)
	}
	return "async-task:" + task.TaskID + ":terminal"
}

func taskBillingAdjustFunding(task *workflowschema.Task, delta int) (bool, error) {
	if task == nil || delta == 0 {
		return false, nil
	}
	if taskBillingIsSubscription(task) {
		return commerceapp.AdjustUserSubscriptionUsageDelta(task.PrivateData.SubscriptionId, taskBillingModelName(task), int64(delta), taskBillingOperationKey(task))
	}
	err := billingapp.AdjustClaudeWalletQuota(task.UserId, delta)
	return err == nil, err
}

// CommitTaskTerminalTransition keeps a subscription's terminal CAS, authoritative
// balance adjustment and final task quota in one transaction. A funding failure
// leaves the task unfinished so the existing polling activity can retry it.
func CommitTaskTerminalTransition(ctx context.Context, task *workflowschema.Task, fromStatus workflowschema.TaskStatus, actualQuota int, reason string) (bool, error) {
	if task == nil || task.ID <= 0 || task.Quota < 0 || actualQuota < 0 {
		return false, errors.New("valid persisted task and nonnegative quota are required")
	}
	if !workflowdomain.IsTaskTerminalStatus(task.Status) {
		return false, errors.New("task transition must target a terminal status")
	}
	if workflowdomain.IsTaskTerminalStatus(fromStatus) {
		return false, nil
	}
	if !taskBillingIsSubscription(task) {
		won, err := workflowdomain.UpdateTaskWithStatus(task, fromStatus)
		if err != nil || !won {
			return won, err
		}
		if actualQuota == 0 {
			return true, recordTaskRefund(ctx, task, reason)
		}
		return true, settleTaskQuotaDelta(ctx, task, actualQuota, reason)
	}

	preConsumedQuota := task.Quota
	updated := *task
	updated.Quota = actualQuota
	var won, applied bool
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&updated).Where("status = ?", fromStatus).Select("*").Updates(&updated)
		if result.Error != nil {
			return result.Error
		}
		won = result.RowsAffected > 0
		if !won || actualQuota == preConsumedQuota {
			return nil
		}
		var err error
		applied, err = commerceapp.AdjustUserSubscriptionUsageDeltaTx(tx, task.PrivateData.SubscriptionId, taskBillingModelName(task), int64(actualQuota-preConsumedQuota), taskBillingOperationKey(task))
		return err
	})
	if err != nil {
		task.Status = fromStatus
		return false, err
	}
	if won {
		task.Quota = actualQuota
		if applied {
			projectTaskBillingAdjustment(ctx, task, preConsumedQuota, actualQuota, reason)
		}
	}
	return won, nil
}

func taskFundingQuota(task *workflowschema.Task, quota int) int {
	if task == nil || quota <= 0 {
		return quota
	}
	billingContext := task.PrivateData.BillingContext
	if billingContext == nil || billingContext.FundingQuotaScale <= 0 || billingContext.FundingQuotaScale == 1 {
		return quota
	}
	return int(math.Round(float64(quota) * billingContext.FundingQuotaScale))
}

func taskBillingAdjustTokenQuota(ctx context.Context, task *workflowschema.Task, delta int) {
	if task == nil || task.PrivateData.TokenId <= 0 || delta == 0 {
		return
	}
	tokenKey := taskBillingResolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		return
	}

	var err error
	err = billingapp.AdjustTokenQuota(task.PrivateData.TokenId, tokenKey, delta)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("调整令牌额度失败 (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
}

func taskBillingOther(task *workflowschema.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if billingContext := task.PrivateData.BillingContext; billingContext != nil {
		other["model_price"] = billingContext.ModelPrice
		if billingContext.ModelRatio > 0 {
			other["model_ratio"] = billingContext.ModelRatio
		}
		other["group_ratio"] = billingContext.GroupRatio
		for key, value := range billingContext.OtherRatios {
			other[key] = value
		}
	}
	if upstreamModelName := task.Properties.UpstreamModelName; upstreamModelName != "" && upstreamModelName != task.Properties.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = upstreamModelName
	}
	return other
}

func recordTaskRefund(ctx context.Context, task *workflowschema.Task, reason string) error {
	if task == nil {
		return nil
	}
	quota := task.Quota
	if quota == 0 {
		return nil
	}
	if quota < 0 {
		return errors.New("task quota must be nonnegative")
	}
	applied, err := taskBillingAdjustFunding(task, -quota)
	if err != nil {
		return err
	}
	if applied {
		projectTaskBillingAdjustment(ctx, task, quota, 0, reason)
	}
	return nil
}

func settleTaskQuotaDelta(ctx context.Context, task *workflowschema.Task, actualQuota int, reason string) error {
	if task == nil || actualQuota <= 0 {
		return nil
	}
	if task.Quota < 0 {
		return errors.New("task quota must be nonnegative")
	}

	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota
	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）", task.TaskID, logger.LogQuota(actualQuota), reason))
		return nil
	}

	logger.LogInfo(ctx, fmt.Sprintf(
		"任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	var applied bool
	if taskBillingIsSubscription(task) {
		err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			var err error
			applied, err = commerceapp.AdjustUserSubscriptionUsageDeltaTx(tx, task.PrivateData.SubscriptionId, taskBillingModelName(task), int64(quotaDelta), taskBillingOperationKey(task))
			if err != nil || task.ID <= 0 {
				return err
			}
			return tx.Model(task).Update("quota", actualQuota).Error
		})
		if err != nil {
			return err
		}
	} else {
		var err error
		applied, err = taskBillingAdjustFunding(task, quotaDelta)
		if err != nil {
			return err
		}
		task.Quota = actualQuota
		if err := workflowdomain.UpdateTaskQuota(task); err != nil {
			logger.LogError(ctx, fmt.Sprintf("差额结算回写 quota 失败 task %s: %s", task.TaskID, err.Error()))
		}
	}
	task.Quota = actualQuota
	if applied {
		projectTaskBillingAdjustment(ctx, task, preConsumedQuota, actualQuota, reason)
	}
	return nil
}

func projectTaskBillingAdjustment(ctx context.Context, task *workflowschema.Task, preConsumedQuota, actualQuota int, reason string) {
	quotaDelta := actualQuota - preConsumedQuota
	taskBillingAdjustTokenQuota(ctx, task, quotaDelta)

	logType := auditschema.LogTypeRefund
	logQuota := -quotaDelta
	if quotaDelta > 0 {
		logType = auditschema.LogTypeConsume
		logQuota = quotaDelta
		billingapp.RecordUsageStats(task.UserId, task.ChannelId, quotaDelta)
	} else {
		billingapp.AdjustUsageQuotaStats(task.UserId, task.ChannelId, quotaDelta)
	}

	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	content := reason
	if actualQuota == 0 {
		other["reason"] = reason
		content = ""
	}
	auditapp.RecordTaskBillingLog(auditschema.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   content,
		ChannelId: task.ChannelId,
		ModelName: taskBillingModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})
}

func taskQuotaByTokens(task *workflowschema.Task, totalTokens int) (int, string) {
	if task == nil || totalTokens <= 0 {
		return 0, ""
	}

	modelName := taskBillingModelName(task)
	modelRatio, hasRatioSetting, _ := gatewaystore.GetModelRatio(modelName)
	if !hasRatioSetting || modelRatio <= 0 {
		return 0, ""
	}

	group := task.Group
	if group == "" {
		user, err := getWorkflowUserByID(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return 0, ""
	}

	groupRatio := gatewaystore.GetGroupRatio(group)
	userGroupRatio, hasUserGroupRatio := gatewaystore.GetGroupGroupRatio(group, group)
	finalGroupRatio := groupRatio
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	}

	otherMultiplier := 1.0
	if billingContext := task.PrivateData.BillingContext; billingContext != nil {
		for _, ratio := range billingContext.OtherRatios {
			if ratio > 0 && ratio != 1.0 {
				otherMultiplier *= ratio
			}
		}
	}

	actualQuota := platformmath.SaturatingMulToInt(float64(totalTokens), modelRatio, finalGroupRatio, otherMultiplier)
	actualQuota = taskFundingQuota(task, actualQuota)
	reason := fmt.Sprintf(
		"token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f",
		totalTokens,
		modelRatio,
		finalGroupRatio,
		otherMultiplier,
	)
	return actualQuota, reason
}

func taskCompletionQuota(adaptor TaskPollingAdaptor, task *workflowschema.Task, taskResult *relaycommon.TaskInfo) (int, string) {
	if task == nil || taskResult == nil {
		return 0, ""
	}
	if billingContext := task.PrivateData.BillingContext; billingContext != nil && billingContext.PerCallBilling {
		return task.Quota, "按次计费"
	}
	if adaptor != nil {
		if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
			return taskFundingQuota(task, actualQuota), "adaptor计费调整"
		}
	}
	if taskResult.TotalTokens > 0 {
		return taskQuotaByTokens(task, taskResult.TotalTokens)
	}
	return task.Quota, ""
}
