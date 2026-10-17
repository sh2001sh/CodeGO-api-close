package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	workflowdomain "github.com/sh2001sh/new-api/internal/workflow/domain"
	workflowschema "github.com/sh2001sh/new-api/internal/workflow/schema"
	"io"
	"net/http"
	"time"
	// TaskPollingAdaptor defines the minimal adaptor contract required by workflow polling.
)

type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	AdjustBillingOnComplete(task *workflowschema.Task, taskResult *relaycommon.TaskInfo) int
}

// GetTaskAdaptorFunc is injected by bootstrap to avoid workflow -> relay package cycles.
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

func SweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := workflowdomain.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	now := time.Now().Unix()
	timedOutCount := 0
	for _, task := range tasks {
		oldStatus := task.Status
		decision := workflowdomain.ApplyTimeout(task, now, constant.TaskTimeoutMinutes, 1740182400)
		actualQuota := task.Quota
		if decision.ShouldRefund {
			actualQuota = 0
		}
		won, err := CommitTaskTerminalTransition(ctx, task, oldStatus, actualQuota, decision.Reason)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// DispatchPlatformUpdate dispatches polling work by task platform.
func DispatchPlatformUpdate(platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*workflowschema.Task) {
	switch platform {
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(context.Background(), taskChannelM, taskM)
	default:
		if err := UpdateVideoTasks(context.Background(), platform, taskChannelM, taskM); err != nil {
			platformobservability.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// PollSingleTask polls and applies one task update through the new workflow runtime.
func PollSingleTask(ctx context.Context, task *workflowschema.Task) error {
	if task == nil {
		return nil
	}
	upstreamTaskID := task.GetUpstreamTaskID()
	if upstreamTaskID == "" {
		oldStatus := task.Status
		task.Status = workflowdomain.TaskStatusFailure
		task.Progress = workflowdomain.TaskProgressComplete
		task.FailReason = "missing upstream task id"
		task.FinishTime = time.Now().Unix()
		_, err := CommitTaskTerminalTransition(ctx, task, oldStatus, 0, task.FailReason)
		return err
	}

	taskChannelM := map[int][]string{
		task.ChannelId: {upstreamTaskID},
	}
	taskM := map[string]*workflowschema.Task{
		upstreamTaskID: task,
	}
	if task.Platform == constant.TaskPlatformSuno {
		return UpdateSunoTasks(ctx, taskChannelM, taskM)
	}
	return UpdateVideoTasks(ctx, task.Platform, taskChannelM, taskM)
}

// UpdateSunoTasks polls batched Suno tasks channel-by-channel.
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*workflowschema.Task) error {
	var updateErr error
	for channelID, taskIDs := range taskChannelM {
		if err := updateSunoTasks(ctx, channelID, taskIDs, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelID, err.Error()))
			updateErr = errors.Join(updateErr, err)
		}
	}
	return updateErr
}

func updateSunoTasks(ctx context.Context, channelID int, taskIDs []string, taskM map[string]*workflowschema.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelID, len(taskIDs)))
	if len(taskIDs) == 0 {
		return nil
	}

	channel, err := gatewaystore.GetCachedChannel(channelID)
	if err != nil {
		platformobservability.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		for _, upstreamID := range taskIDs {
			if task, ok := taskM[upstreamID]; ok {
				oldStatus := task.Status
				task.Status = workflowdomain.TaskStatusFailure
				task.Progress = workflowdomain.TaskProgressComplete
				task.FailReason = fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelID)
				task.FinishTime = time.Now().Unix()
				if _, refundErr := CommitTaskTerminalTransition(ctx, task, oldStatus, 0, task.FailReason); refundErr != nil {
					return fmt.Errorf("fail Suno task %s after channel lookup: %w", task.TaskID, refundErr)
				}
			}
		}
		return nil
	}

	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		return errors.New("adaptor not found")
	}
	proxy := gatewaydomain.GetSettings(channel).Proxy
	resp, err := adaptor.FetchTask(*channel.BaseURL, channel.Key, map[string]any{"ids": taskIDs}, proxy)
	if err != nil {
		platformobservability.SysLog(fmt.Sprintf("Get Task Do req error: %v", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return fmt.Errorf("Get Task status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		platformobservability.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return err
	}
	var responseItems dto.TaskResponse[[]dto.SunoDataResponse]
	if err := platformencoding.Unmarshal(responseBody, &responseItems); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, body: %s", err, string(responseBody)))
		return err
	}
	if !responseItems.IsSuccess() {
		platformobservability.SysLog(fmt.Sprintf("渠道 #%d 未完成的任务有: %d, 成功获取到任务数: %s", channelID, len(taskIDs), string(responseBody)))
		return err
	}

	for _, responseItem := range responseItems.Data {
		task := taskM[responseItem.TaskID]
		if task == nil || workflowdomain.IsTaskTerminalStatus(task.Status) {
			continue
		}
		if !workflowdomain.ShouldApplySunoTaskUpdate(task, responseItem) {
			continue
		}
		oldStatus := task.Status
		decision := workflowdomain.ApplySunoTaskUpdate(task, responseItem)
		if workflowdomain.IsTaskTerminalStatus(task.Status) {
			actualQuota := task.Quota
			if decision.ShouldRefund {
				actualQuota = 0
			}
			if _, err := CommitTaskTerminalTransition(ctx, task, oldStatus, actualQuota, decision.Reason); err != nil {
				return fmt.Errorf("finalize Suno task %s: %w", task.TaskID, err)
			}
			continue
		}
		if err := workflowdomain.SaveTask(task); err != nil {
			return fmt.Errorf("update Suno task %s: %w", task.TaskID, err)
		}
	}
	return nil
}

// UpdateVideoTasks polls async video tasks by channel.
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*workflowschema.Task) error {
	var updateErr error
	for channelID, taskIDs := range taskChannelM {
		if err := updateVideoTasks(ctx, platform, channelID, taskIDs, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelID, err.Error()))
			updateErr = errors.Join(updateErr, err)
		}
	}
	return updateErr
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelID int, taskIDs []string, taskM map[string]*workflowschema.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelID, len(taskIDs)))
	if len(taskIDs) == 0 {
		return nil
	}

	channel, err := gatewaystore.GetCachedChannel(channelID)
	if err != nil {
		for _, upstreamID := range taskIDs {
			if task, ok := taskM[upstreamID]; ok {
				oldStatus := task.Status
				task.Status = workflowdomain.TaskStatusFailure
				task.Progress = workflowdomain.TaskProgressComplete
				task.FailReason = fmt.Sprintf("Failed to get channel info, channel ID: %d", channelID)
				task.FinishTime = time.Now().Unix()
				if _, refundErr := CommitTaskTerminalTransition(ctx, task, oldStatus, 0, task.FailReason); refundErr != nil {
					return fmt.Errorf("fail video task %s after channel lookup: %w", task.TaskID, refundErr)
				}
			}
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}

	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			MarketplaceEgress: channel.RequiresMarketplaceEgress(),
			ChannelType:       channel.Type,
			ChannelBaseUrl:    channel.GetBaseURL(),
			ApiKey:            channel.Key,
		},
	}
	adaptor.Init(info)

	var updateErr error
	for _, taskID := range taskIDs {
		if err := updateVideoSingleTask(ctx, adaptor, channel, taskID, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskID, err.Error()))
			updateErr = errors.Join(updateErr, err)
		}
		time.Sleep(1 * time.Second)
	}
	return updateErr
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, channel *gatewayschema.Channel, taskID string, taskM map[string]*workflowschema.Task) error {
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}
	proxy := gatewaydomain.GetSettings(channel).Proxy

	task := taskM[taskID]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskID))
		return fmt.Errorf("task %s not found", taskID)
	}
	if workflowdomain.IsTaskTerminalStatus(task.Status) {
		return nil
	}

	key := channel.Key
	if privateKey := task.PrivateData.Key; privateKey != "" {
		key = privateKey
	}
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %w", taskID, err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", taskID, err)
	}
	logger.LogDebug(ctx, fmt.Sprintf("updateVideoSingleTask response: %s", string(responseBody)))

	snap := workflowdomain.TakeTaskSnapshot(task)
	taskResult := &relaycommon.TaskInfo{}
	var responseItems dto.TaskResponse[workflowschema.Task]
	if err := platformencoding.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, fmt.Sprintf("updateVideoSingleTask parsed as new api response format: %+v", responseItems))
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = t.Data
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskID, err)
	}

	task.Data = redactVideoResponseBody(responseBody)
	logger.LogDebug(ctx, fmt.Sprintf("updateVideoSingleTask taskResult: %+v", taskResult))

	now := time.Now().Unix()
	if taskResult.Status == "" {
		errorResult := &dto.GeneralErrorResponse{}
		if err := platformencoding.Unmarshal(responseBody, &errorResult); err == nil {
			openaiError := errorResult.TryToOpenAIError()
			if openaiError != nil {
				if openaiError.Code == "429" {
					return nil
				}
				taskResult = relaycommon.FailTaskInfo("upstream returned error")
			} else {
				logger.LogError(ctx, fmt.Sprintf("Task %s returned empty status with unrecognized error format, response: %s", taskID, string(responseBody)))
				taskResult = relaycommon.FailTaskInfo("upstream returned unrecognized message")
			}
		}
	}

	decision, err := workflowdomain.ApplyVideoTaskResult(task, taskResult, now)
	if err != nil {
		return err
	}
	shouldRefund := decision.ShouldRefund
	shouldSettle := decision.ShouldSettle
	if shouldRefund {
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskID), task)
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
	}

	isDone := workflowdomain.IsTaskTerminalStatus(task.Status)
	if isDone && snap.Status != task.Status {
		actualQuota, reason := task.Quota, ""
		if shouldRefund {
			actualQuota, reason = 0, task.FailReason
		} else if shouldSettle {
			if completionQuota, completionReason := taskCompletionQuota(adaptor, task, taskResult); completionQuota > 0 {
				actualQuota, reason = completionQuota, completionReason
			}
		}
		won, err := CommitTaskTerminalTransition(ctx, task, snap.Status, actualQuota, reason)
		if err != nil {
			return fmt.Errorf("finalize task %s: %w", task.TaskID, err)
		}
		if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned by another process, skip billing", task.TaskID))
		}
		return nil
	} else if !snap.Equal(workflowdomain.TakeTaskSnapshot(task)) {
		if _, err := workflowdomain.UpdateTaskWithStatus(task, snap.Status); err != nil {
			return fmt.Errorf("update task %s: %w", task.TaskID, err)
		}
	} else {
		logger.LogDebug(ctx, fmt.Sprintf("No update needed for task %s", task.TaskID))
	}

	return nil
}

func redactVideoResponseBody(body []byte) []byte {
	var payload map[string]any
	if err := platformencoding.Unmarshal(body, &payload); err != nil {
		return body
	}
	response, _ := payload["response"].(map[string]any)
	if response != nil {
		delete(response, "bytesBase64Encoded")
		if video, ok := response["video"].(string); ok {
			response["video"] = truncateBase64(video)
		}
		if videos, ok := response["videos"].([]any); ok {
			for i := range videos {
				if videoMap, ok := videos[i].(map[string]any); ok {
					delete(videoMap, "bytesBase64Encoded")
				}
			}
		}
	}
	encoded, err := platformencoding.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}
