package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	bounty "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func CreateMaterialRequest(taskID string, userID int64, input MaterialRequestInput) (*TaskDetailView, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" || len([]rune(content)) > 10000 {
		return nil, fmt.Errorf("material request content is required and must not exceed 10000 characters")
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.AssigneeUserID == nil || *task.AssigneeUserID != userID {
			return ErrForbidden
		}
		if task.Status != bounty.TaskStatusAssigned && task.Status != bounty.TaskStatusInProgress && task.Status != bounty.TaskStatusChangesRequested && task.Status != bounty.TaskStatusPublisherReplied {
			return ErrInvalidState
		}
		now := time.Now()
		request := &bountyschema.BountyMaterialRequest{TaskID: taskID, RequesterUserID: userID, Content: content, IsBlocking: input.IsBlocking, Status: bounty.MaterialStatusOpen}
		if input.IsBlocking {
			timeoutAt := now.Add(MaterialReplyWindow)
			request.TimeoutAt = &timeoutAt
		}
		if err := tx.Create(request).Error; err != nil {
			return err
		}
		if input.IsBlocking {
			if task.PausedAt == nil {
				task.PausedAt = &now
			}
			if err := transitionTaskTx(tx, task, bounty.TaskStatusWaitingForPublisher, map[string]any{"paused_at": task.PausedAt}); err != nil {
				return err
			}
		}
		event, err := recordEventTx(tx, taskID, bounty.EventMaterialRequested, userID, constant.RoleCommonUser, map[string]any{
			"request_id":  request.RequestID,
			"is_blocking": input.IsBlocking,
			"content":     content,
		})
		if err != nil {
			return err
		}
		return createNotificationTx(tx, task.PublisherUserID, taskID, event.EventID, "material_requested", "执行者请求补充材料", content)
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func ReplyMaterialRequest(taskID string, requestID string, userID int64, input MaterialReplyInput) (*TaskDetailView, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" || len([]rune(content)) > 10000 {
		return nil, fmt.Errorf("reply content is required and must not exceed 10000 characters")
	}
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = "platform"
	}
	sourceURL := strings.TrimSpace(input.SourceURL)
	if sourceURL != "" {
		if err := bounty.ValidateGitHubURL(sourceURL, false); err != nil {
			return nil, err
		}
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if bounty.IsTerminalStatus(task.Status) || task.Status == bounty.TaskStatusSuspended {
			return ErrInvalidState
		}
		var request bountyschema.BountyMaterialRequest
		if err := tx.Where("request_id = ? AND task_id = ?", requestID, taskID).First(&request).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("material request not found")
			}
			return err
		}
		if request.Status == bounty.MaterialStatusClosed {
			return fmt.Errorf("material request is already closed")
		}
		if userID != task.PublisherUserID && (task.AssigneeUserID == nil || *task.AssigneeUserID != userID) {
			return ErrForbidden
		}
		reply := &bountyschema.BountyMaterialReply{RequestID: requestID, AuthorUserID: userID, Content: content, SourceType: sourceType, SourceURL: sourceURL}
		if err := tx.Create(reply).Error; err != nil {
			return err
		}
		if userID == task.PublisherUserID {
			if err := tx.Model(&request).Updates(map[string]any{"status": bounty.MaterialStatusAwaitingConfirmation, "timeout_at": nil, "timeout_notified_at": nil}).Error; err != nil {
				return err
			}
			if request.IsBlocking && task.Status == bounty.TaskStatusWaitingForPublisher {
				if err := transitionTaskTx(tx, task, bounty.TaskStatusPublisherReplied, nil); err != nil {
					return err
				}
			}
		} else if request.Status == bounty.MaterialStatusOpen {
			if err := tx.Model(&request).Update("status", bounty.MaterialStatusReplied).Error; err != nil {
				return err
			}
		}
		event, err := recordEventTx(tx, taskID, bounty.EventMaterialReplied, userID, constant.RoleCommonUser, map[string]any{
			"request_id":  requestID,
			"source_type": sourceType,
			"source_url":  sourceURL,
		})
		if err != nil {
			return err
		}
		recipient := request.RequesterUserID
		if recipient == userID {
			recipient = task.PublisherUserID
		}
		return createNotificationTx(tx, recipient, taskID, event.EventID, "material_replied", "材料请求已有回复", content)
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func ResolveMaterialRequest(taskID string, requestID string, userID int64) (*TaskDetailView, error) {
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if bounty.IsTerminalStatus(task.Status) || task.Status == bounty.TaskStatusSuspended {
			return ErrInvalidState
		}
		if task.AssigneeUserID == nil || *task.AssigneeUserID != userID {
			return ErrForbidden
		}
		var request bountyschema.BountyMaterialRequest
		if err := tx.Where("request_id = ? AND task_id = ?", requestID, taskID).First(&request).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("material request not found")
			}
			return err
		}
		if request.Status == bounty.MaterialStatusClosed {
			return nil
		}
		now := time.Now()
		if err := tx.Model(&request).Updates(map[string]any{"status": bounty.MaterialStatusClosed, "resolved_at": &now}).Error; err != nil {
			return err
		}
		if request.IsBlocking {
			var openCount int64
			if err := tx.Model(&bountyschema.BountyMaterialRequest{}).Where("task_id = ? AND is_blocking = ? AND status IN ?", taskID, true, []string{bounty.MaterialStatusOpen, bounty.MaterialStatusReplied, bounty.MaterialStatusAwaitingConfirmation}).Count(&openCount).Error; err != nil {
				return err
			}
			if openCount == 0 {
				if err := resumeTaskTimerTx(tx, task); err != nil {
					return err
				}
				if err := transitionTaskTx(tx, task, bounty.TaskStatusInProgress, nil); err != nil {
					return err
				}
			}
		}
		event, err := recordEventTx(tx, taskID, bounty.EventMaterialResolved, userID, constant.RoleCommonUser, map[string]any{"request_id": requestID})
		if err != nil {
			return err
		}
		return createNotificationTx(tx, task.PublisherUserID, taskID, event.EventID, "material_resolved", "材料请求已解决", "执行者已确认补充内容，任务可以继续推进。")
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func HandleMaterialTimeout(taskID string, requestID string, userID int64, input MaterialTimeoutInput) (*TaskDetailView, error) {
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action != "extend" && action != "cancel" {
		return nil, fmt.Errorf("unsupported material timeout action")
	}
	extensionHours := input.ExtensionHours
	if extensionHours == 0 {
		extensionHours = 48
	}
	if extensionHours < 1 || extensionHours > 168 {
		return nil, fmt.Errorf("extension_hours must be between 1 and 168")
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.AssigneeUserID == nil || *task.AssigneeUserID != userID {
			return ErrForbidden
		}
		var request bountyschema.BountyMaterialRequest
		if err := tx.Where("request_id = ? AND task_id = ?", requestID, taskID).First(&request).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("material request not found")
			}
			return err
		}
		if !request.IsBlocking || request.Status != bounty.MaterialStatusOpen {
			return fmt.Errorf("material request is not eligible for timeout handling")
		}
		now := time.Now()
		if request.TimeoutAt == nil || request.TimeoutAt.After(now) {
			return fmt.Errorf("material reply window has not elapsed")
		}
		if action == "extend" {
			timeoutAt := now.Add(time.Duration(extensionHours) * time.Hour)
			if err := tx.Model(&request).Updates(map[string]any{"timeout_at": &timeoutAt, "timeout_notified_at": nil, "timeout_action": "extended"}).Error; err != nil {
				return err
			}
			event, err := recordEventTx(tx, taskID, bounty.EventMaterialTimeoutAction, userID, constant.RoleCommonUser, map[string]any{"request_id": requestID, "action": action, "extension_hours": extensionHours})
			if err != nil {
				return err
			}
			return createNotificationTx(tx, task.PublisherUserID, taskID, event.EventID, "material_timeout_extension", "材料回复期限已延长", fmt.Sprintf("执行者将材料回复期限延长了 %d 小时。", extensionHours))
		}
		if err := tx.Model(&request).Updates(map[string]any{"status": bounty.MaterialStatusClosed, "resolved_at": &now, "timeout_action": "cancelled"}).Error; err != nil {
			return err
		}
		if err := releaseTaskRewardTx(tx, task, "material_timeout_cancelled"); err != nil {
			return err
		}
		if err := transitionTaskTx(tx, task, bounty.TaskStatusCancelled, nil); err != nil {
			return err
		}
		if _, err := recordEventTx(tx, taskID, bounty.EventMaterialTimeoutAction, userID, constant.RoleCommonUser, map[string]any{"request_id": requestID, "action": action}); err != nil {
			return err
		}
		cancelledEvent, err := recordEventTx(tx, taskID, bounty.EventTaskCancelled, userID, constant.RoleCommonUser, map[string]any{"reason": "material_timeout", "request_id": requestID})
		if err != nil {
			return err
		}
		if err := createNotificationTx(tx, task.PublisherUserID, taskID, cancelledEvent.EventID, "task_cancelled", "任务已无责取消", "材料请求超过 48 小时未回复，执行者已无责取消任务，冻结额度已释放。"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func resumeTaskTimerTx(tx *gorm.DB, task *bountyschema.BountyTask) error {
	if task.PausedAt == nil {
		return nil
	}
	pausedDuration := time.Since(*task.PausedAt)
	if pausedDuration > 0 {
		task.DeadlineAt = task.DeadlineAt.Add(pausedDuration)
	}
	task.PausedAt = nil
	return tx.Model(task).Updates(map[string]any{"deadline_at": task.DeadlineAt, "paused_at": nil}).Error
}
