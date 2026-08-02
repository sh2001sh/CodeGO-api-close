package app

import (
	"sync"
	"time"

	"github.com/sh2001sh/new-api/constant"
	bounty "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

var bountyMaintenanceOnce sync.Once

func StartBountyMaintenanceTask() {
	bountyMaintenanceOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				if err := RunBountyMaintenanceOnce(); err != nil {
					continue
				}
			}
		}()
	})
}

func RunBountyMaintenanceOnce() error {
	if err := notifyReviewDeadlines(); err != nil {
		return err
	}
	if err := notifyMaterialTimeouts(); err != nil {
		return err
	}
	if err := autoSettleReviews(); err != nil {
		return err
	}
	return expireTasks()
}

func notifyReviewDeadlines() error {
	cutoff := time.Now().Add(24 * time.Hour)
	var tasks []bountyschema.BountyTask
	if err := platformdb.DB.Where("status = ? AND review_deadline_at IS NOT NULL AND review_deadline_at <= ? AND review_deadline_at > ? AND review_deadline_notified_at IS NULL", bounty.TaskStatusReviewing, cutoff, time.Now()).Find(&tasks).Error; err != nil {
		return err
	}
	for index := range tasks {
		taskID := tasks[index].TaskID
		if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			task, err := lockTaskTx(tx, taskID)
			if err != nil {
				return err
			}
			if task.Status != bounty.TaskStatusReviewing || task.ReviewDeadlineAt == nil || task.ReviewDeadlineAt.After(cutoff) || task.ReviewDeadlineNotifiedAt != nil {
				return nil
			}
			event, err := recordEventTx(tx, taskID, bounty.EventReviewDeadlineSoon, 0, constant.RoleRootUser, map[string]any{"review_deadline_at": task.ReviewDeadlineAt})
			if err != nil {
				return err
			}
			now := time.Now()
			if err := tx.Model(task).Update("review_deadline_notified_at", &now).Error; err != nil {
				return err
			}
			return createNotificationTx(tx, task.PublisherUserID, taskID, event.EventID, "review_deadline_soon", "验收即将超时", "任务验收窗口将在 24 小时内结束，请及时验收、要求修改或发起申诉。")
		}); err != nil {
			return err
		}
	}
	return nil
}

func notifyMaterialTimeouts() error {
	now := time.Now()
	var requests []bountyschema.BountyMaterialRequest
	if err := platformdb.DB.Where("is_blocking = ? AND status = ? AND timeout_at IS NOT NULL AND timeout_at <= ? AND timeout_notified_at IS NULL", true, bounty.MaterialStatusOpen, now).Find(&requests).Error; err != nil {
		return err
	}
	for index := range requests {
		requestID := requests[index].RequestID
		if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			var request bountyschema.BountyMaterialRequest
			if err := tx.Where("request_id = ?", requestID).First(&request).Error; err != nil {
				return err
			}
			if request.TimeoutNotifiedAt != nil || request.Status != bounty.MaterialStatusOpen || request.TimeoutAt == nil || request.TimeoutAt.After(time.Now()) {
				return nil
			}
			task, err := lockTaskTx(tx, request.TaskID)
			if err != nil {
				return err
			}
			event, err := recordEventTx(tx, request.TaskID, bounty.EventMaterialTimeout, 0, constant.RoleRootUser, map[string]any{"request_id": request.RequestID, "timeout_at": request.TimeoutAt})
			if err != nil {
				return err
			}
			if err := tx.Model(&request).Update("timeout_notified_at", &now).Error; err != nil {
				return err
			}
			if err := createNotificationTx(tx, task.PublisherUserID, task.TaskID, event.EventID, "material_timeout", "材料请求已超时", "材料请求已超过 48 小时未回复，执行者可以申请延长或无责取消。"); err != nil {
				return err
			}
			return createNotificationTx(tx, request.RequesterUserID, task.TaskID, event.EventID, "material_timeout", "材料请求已超时", "材料请求已超过 48 小时未回复，请选择延长或无责取消。")
		}); err != nil {
			return err
		}
	}
	return nil
}

func autoSettleReviews() error {
	var tasks []bountyschema.BountyTask
	if err := platformdb.DB.Where("status = ? AND review_deadline_at IS NOT NULL AND review_deadline_at <= ?", bounty.TaskStatusReviewing, time.Now()).Find(&tasks).Error; err != nil {
		return err
	}
	for index := range tasks {
		taskID := tasks[index].TaskID
		if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			task, err := lockTaskTx(tx, taskID)
			if err != nil {
				return err
			}
			if task.Status != bounty.TaskStatusReviewing || task.ReviewDeadlineAt == nil || task.ReviewDeadlineAt.After(time.Now()) {
				return nil
			}
			return settleTaskTx(tx, task, 0, constant.RoleRootUser, task.RewardAmount, bounty.EventReviewApproved, "72 小时内未收到验收操作，系统自动结算。", true)
		}); err != nil {
			return err
		}
	}
	return nil
}

func expireTasks() error {
	var tasks []bountyschema.BountyTask
	if err := platformdb.DB.Where("status IN ? AND deadline_at <= ? AND paused_at IS NULL", []string{bounty.TaskStatusPublished, bounty.TaskStatusSelecting, bounty.TaskStatusAssigned, bounty.TaskStatusInProgress, bounty.TaskStatusPublisherReplied, bounty.TaskStatusChangesRequested}, time.Now()).Find(&tasks).Error; err != nil {
		return err
	}
	for index := range tasks {
		if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			task, err := lockTaskTx(tx, tasks[index].TaskID)
			if err != nil {
				return err
			}
			if task.PausedAt != nil || task.DeadlineAt.After(time.Now()) || bounty.IsTerminalStatus(task.Status) {
				return nil
			}
			if err := releaseTaskRewardTx(tx, task, "task_expired"); err != nil {
				return err
			}
			if err := transitionTaskTx(tx, task, bounty.TaskStatusExpired, nil); err != nil {
				return err
			}
			event, err := recordEventTx(tx, task.TaskID, bounty.EventTaskExpired, 0, constant.RoleRootUser, map[string]any{"deadline_at": task.DeadlineAt})
			if err != nil {
				return err
			}
			recipients := []int64{task.PublisherUserID}
			if task.AssigneeUserID != nil {
				recipients = append(recipients, *task.AssigneeUserID)
			}
			return createNotificationsTx(tx, recipients, task.TaskID, event, "task_expired", "任务已过期", "任务截止时间已到，冻结额度已释放。")
		}); err != nil {
			return err
		}
	}
	return nil
}
