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
	"gorm.io/gorm/clause"
)

func ListAdminTasks(req ListTasksRequest) (*AdminTaskListResponse, error) {
	result, err := ListTasks(0, req, constant.RoleAdminUser)
	if err != nil {
		return nil, err
	}
	return &AdminTaskListResponse{Items: result.Items, Total: result.Total, Page: result.Page, PageSize: result.PageSize}, nil
}

func ListAdminDisputes() ([]DisputeView, error) {
	var disputes []bountyschema.BountyDispute
	if err := platformdb.DB.Where("status = ?", bounty.DisputeStatusOpen).Order("created_at DESC").Find(&disputes).Error; err != nil {
		return nil, err
	}
	return buildDisputeViews(disputes)
}

func ResolveDispute(taskID string, adminID int64, input AdminResolutionInput) (*TaskDetailView, error) {
	resolution := strings.ToLower(strings.TrimSpace(input.ResolutionType))
	if resolution != bounty.DisputeResolutionPayFull && resolution != bounty.DisputeResolutionPayPart && resolution != bounty.DisputeResolutionRelease && resolution != bounty.DisputeResolutionChanges {
		return nil, fmt.Errorf("unsupported dispute resolution")
	}
	if strings.TrimSpace(input.DisputeID) == "" {
		return nil, fmt.Errorf("dispute_id is required")
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		var dispute bountyschema.BountyDispute
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("dispute_id = ? AND task_id = ?", input.DisputeID, taskID).First(&dispute).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("dispute not found")
			}
			return err
		}
		if dispute.Status == bounty.DisputeStatusResolved {
			return nil
		}
		if task.Status != bounty.TaskStatusDisputed {
			return ErrInvalidState
		}
		amount := input.Amount
		switch resolution {
		case bounty.DisputeResolutionPayFull:
			amount = task.RewardAmount
		case bounty.DisputeResolutionRelease, bounty.DisputeResolutionChanges:
			amount = 0
		case bounty.DisputeResolutionPayPart:
			if amount <= 0 || amount >= task.RewardAmount {
				return fmt.Errorf("partial settlement amount must be between 0 and the reward")
			}
		}
		if resolution == bounty.DisputeResolutionChanges {
			if task.RevisionCount >= task.RevisionLimit {
				return fmt.Errorf("revision limit reached")
			}
			task.RevisionCount++
			if err := transitionTaskTx(tx, task, bounty.TaskStatusChangesRequested, map[string]any{"revision_count": task.RevisionCount}); err != nil {
				return err
			}
			changeEvent, err := recordEventTx(tx, taskID, bounty.EventChangesRequested, adminID, constant.RoleAdminUser, map[string]any{"revision_count": task.RevisionCount, "comment": input.Note, "source": "dispute_resolution"})
			if err != nil {
				return err
			}
			if task.AssigneeUserID != nil {
				if err := createNotificationTx(tx, *task.AssigneeUserID, taskID, changeEvent.EventID, "changes_requested", "平台要求继续修改", strings.TrimSpace(input.Note)); err != nil {
					return err
				}
			}
		} else if resolution == bounty.DisputeResolutionRelease {
			if err := releaseTaskRewardTx(tx, task, "admin_dispute_release"); err != nil {
				return err
			}
			if err := transitionTaskTx(tx, task, bounty.TaskStatusResolved, nil); err != nil {
				return err
			}
		} else {
			if err := settleTaskTx(tx, task, adminID, constant.RoleAdminUser, amount, bounty.EventReviewApproved, input.Note, false); err != nil {
				return err
			}
			if err := transitionTaskTx(tx, task, bounty.TaskStatusResolved, nil); err != nil {
				return err
			}
		}
		now := time.Now()
		if err := tx.Model(&dispute).Updates(map[string]any{
			"status":              bounty.DisputeStatusResolved,
			"resolution_type":     resolution,
			"resolution_amount":   amount,
			"resolution_note":     strings.TrimSpace(input.Note),
			"resolved_by_user_id": adminID,
			"resolved_at":         &now,
		}).Error; err != nil {
			return err
		}
		event, err := recordEventTx(tx, taskID, bounty.EventDisputeResolved, adminID, constant.RoleAdminUser, map[string]any{
			"dispute_id":      dispute.DisputeID,
			"resolution_type": resolution,
			"amount":          amount,
			"note":            input.Note,
		})
		if err != nil {
			return err
		}
		recipients := []int64{task.PublisherUserID}
		if task.AssigneeUserID != nil {
			recipients = append(recipients, *task.AssigneeUserID)
		}
		return createNotificationsTx(tx, recipients, taskID, event, "dispute_resolved", "申诉已有裁决", fmt.Sprintf("平台裁决：%s。", resolution))
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, adminID, constant.RoleAdminUser)
}

func SuspendTask(taskID string, adminID int64) (*TaskDetailView, error) {
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if bounty.IsTerminalStatus(task.Status) || task.Status == bounty.TaskStatusSuspended {
			return ErrInvalidState
		}
		previousStatus := task.Status
		now := time.Now()
		updates := map[string]any{
			"suspended_from_status":        previousStatus,
			"suspended_at":                 &now,
			"suspended_previous_paused_at": task.PausedAt,
		}
		if task.PausedAt == nil {
			updates["paused_at"] = &now
		}
		if err := transitionTaskTx(tx, task, bounty.TaskStatusSuspended, updates); err != nil {
			return err
		}
		event, err := recordEventTx(tx, taskID, bounty.EventTaskSuspended, adminID, constant.RoleAdminUser, map[string]any{"previous_status": previousStatus})
		if err != nil {
			return err
		}
		recipients := []int64{task.PublisherUserID}
		if task.AssigneeUserID != nil {
			recipients = append(recipients, *task.AssigneeUserID)
		}
		return createNotificationsTx(tx, recipients, taskID, event, "task_suspended", "任务已暂停", "管理员已暂停任务，倒计时和交付操作暂时停止。")
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, adminID, constant.RoleAdminUser)
}

func ResumeTask(taskID string, adminID int64) (*TaskDetailView, error) {
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.Status != bounty.TaskStatusSuspended {
			return ErrInvalidState
		}
		previous := task.SuspendedFromStatus
		if previous == "" {
			previous = bounty.TaskStatusPublished
		}
		if err := bounty.RequireTransition(task.Status, previous); err != nil {
			return err
		}
		updates := map[string]any{"suspended_from_status": "", "suspended_at": nil, "suspended_previous_paused_at": nil}
		if task.SuspendedAt != nil {
			pausedDuration := time.Since(*task.SuspendedAt)
			if pausedDuration > 0 {
				task.DeadlineAt = task.DeadlineAt.Add(pausedDuration)
				updates["deadline_at"] = task.DeadlineAt
				if task.ReviewDeadlineAt != nil {
					reviewDeadline := task.ReviewDeadlineAt.Add(pausedDuration)
					task.ReviewDeadlineAt = &reviewDeadline
					updates["review_deadline_at"] = task.ReviewDeadlineAt
				}
			}
			if task.SuspendedPreviousPausedAt != nil {
				adjustedPausedAt := task.SuspendedPreviousPausedAt.Add(pausedDuration)
				updates["paused_at"] = &adjustedPausedAt
			} else {
				updates["paused_at"] = nil
			}
		}
		if err := tx.Model(task).Updates(updates).Error; err != nil {
			return err
		}
		task.Status = previous
		event, err := recordEventTx(tx, taskID, bounty.EventTaskResumed, adminID, constant.RoleAdminUser, map[string]any{"status": previous})
		if err != nil {
			return err
		}
		recipients := []int64{task.PublisherUserID}
		if task.AssigneeUserID != nil {
			recipients = append(recipients, *task.AssigneeUserID)
		}
		return createNotificationsTx(tx, recipients, taskID, event, "task_resumed", "任务已恢复", "管理员已恢复任务，可以继续推进。")
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, adminID, constant.RoleAdminUser)
}

func ListNotifications(userID int64) (*NotificationListResponse, error) {
	var items []bountyschema.BountyNotification
	if err := platformdb.DB.Where("user_id = ?", userID).Order("created_at DESC").Limit(100).Find(&items).Error; err != nil {
		return nil, err
	}
	var unread int64
	if err := platformdb.DB.Model(&bountyschema.BountyNotification{}).Where("user_id = ? AND read_at IS NULL", userID).Count(&unread).Error; err != nil {
		return nil, err
	}
	views := make([]NotificationView, 0, len(items))
	for index := range items {
		item := items[index]
		views = append(views, NotificationView{NotificationID: item.NotificationID, TaskID: item.TaskID, Type: item.Type, Title: item.Title, Content: item.Content, ReadAt: item.ReadAt, CreatedAt: item.CreatedAt})
	}
	return &NotificationListResponse{Items: views, UnreadCount: unread}, nil
}

func MarkNotificationRead(userID int64, notificationID string) error {
	now := time.Now()
	return platformdb.DB.Model(&bountyschema.BountyNotification{}).Where("notification_id = ? AND user_id = ?", notificationID, userID).Update("read_at", &now).Error
}

func MarkAllNotificationsRead(userID int64) error {
	now := time.Now()
	return platformdb.DB.Model(&bountyschema.BountyNotification{}).Where("user_id = ? AND read_at IS NULL", userID).Update("read_at", &now).Error
}
