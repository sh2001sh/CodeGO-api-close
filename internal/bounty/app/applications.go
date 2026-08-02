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

func SubmitApplication(taskID string, userID int64, req CreateApplicationRequest) (*TaskDetailView, error) {
	message := strings.TrimSpace(req.Message)
	if message == "" || len([]rune(message)) > 8000 {
		return nil, fmt.Errorf("application message is required and must not exceed 8000 characters")
	}
	estimated, err := parseOptionalTime(req.EstimatedDeliveryAt)
	if err != nil {
		return nil, err
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.PublisherUserID == userID {
			return fmt.Errorf("publisher cannot apply to own task")
		}
		if task.Status != bounty.TaskStatusPublished && task.Status != bounty.TaskStatusSelecting {
			return ErrInvalidState
		}
		var existing bountyschema.BountyApplication
		if err := tx.Where("task_id = ? AND applicant_user_id = ?", taskID, userID).First(&existing).Error; err == nil {
			return ErrApplicationFound
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		application := &bountyschema.BountyApplication{TaskID: taskID, ApplicantUserID: userID, Message: message, EstimatedDeliveryAt: estimated, Status: bounty.ApplicationStatusPending}
		if err := tx.Create(application).Error; err != nil {
			return err
		}
		if task.Status == bounty.TaskStatusPublished {
			if err := transitionTaskTx(tx, task, bounty.TaskStatusSelecting, nil); err != nil {
				return err
			}
		}
		event, err := recordEventTx(tx, taskID, bounty.EventApplicationSubmitted, userID, constant.RoleCommonUser, map[string]any{
			"application_id": application.ApplicationID,
			"message":        message,
		})
		if err != nil {
			return err
		}
		return createNotificationTx(tx, task.PublisherUserID, taskID, event.EventID, "application_submitted", "收到新的接取申请", "有人申请接取你的任务，请查看申请说明。")
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func AssignApplication(taskID string, publisherID int64, applicationID string) (*TaskDetailView, error) {
	if strings.TrimSpace(applicationID) == "" {
		return nil, fmt.Errorf("application_id is required")
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.PublisherUserID != publisherID {
			return ErrForbidden
		}
		if task.Status != bounty.TaskStatusPublished && task.Status != bounty.TaskStatusSelecting {
			return ErrInvalidState
		}
		var application bountyschema.BountyApplication
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("application_id = ? AND task_id = ?", applicationID, taskID).First(&application).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("application not found")
			}
			return err
		}
		if application.Status != bounty.ApplicationStatusPending {
			return fmt.Errorf("application is no longer pending")
		}
		var rejected []bountyschema.BountyApplication
		if err := tx.Where("task_id = ? AND status = ? AND application_id <> ?", taskID, bounty.ApplicationStatusPending, applicationID).Find(&rejected).Error; err != nil {
			return err
		}
		if err := tx.Model(&bountyschema.BountyApplication{}).Where("task_id = ? AND status = ? AND application_id <> ?", taskID, bounty.ApplicationStatusPending, applicationID).Update("status", bounty.ApplicationStatusRejected).Error; err != nil {
			return err
		}
		if err := tx.Model(&application).Update("status", bounty.ApplicationStatusAccepted).Error; err != nil {
			return err
		}
		if err := transitionTaskTx(tx, task, bounty.TaskStatusAssigned, map[string]any{"assignee_user_id": application.ApplicantUserID}); err != nil {
			return err
		}
		accepted, err := recordEventTx(tx, taskID, bounty.EventApplicationAccepted, publisherID, constant.RoleCommonUser, map[string]any{
			"application_id":   application.ApplicationID,
			"executor_user_id": application.ApplicantUserID,
		})
		if err != nil {
			return err
		}
		if err := createNotificationTx(tx, application.ApplicantUserID, taskID, accepted.EventID, "application_accepted", "申请已确认", "发布者已确认由你执行这个任务，可以开始开发。"); err != nil {
			return err
		}
		for index := range rejected {
			rejectedEvent, err := recordEventTx(tx, taskID, bounty.EventApplicationRejected, publisherID, constant.RoleCommonUser, map[string]any{
				"application_id":   rejected[index].ApplicationID,
				"executor_user_id": application.ApplicantUserID,
			})
			if err != nil {
				return err
			}
			if err := createNotificationTx(tx, rejected[index].ApplicantUserID, taskID, rejectedEvent.EventID, "application_rejected", "申请未被选中", "发布者已选择其他执行者，感谢你的申请。"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, publisherID, constant.RoleCommonUser)
}

func StartTask(taskID string, userID int64) (*TaskDetailView, error) {
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.AssigneeUserID == nil || *task.AssigneeUserID != userID {
			return ErrForbidden
		}
		if task.Status != bounty.TaskStatusAssigned {
			return ErrInvalidState
		}
		if err := transitionTaskTx(tx, task, bounty.TaskStatusInProgress, nil); err != nil {
			return err
		}
		event, err := recordEventTx(tx, taskID, bounty.EventTaskStarted, userID, constant.RoleCommonUser, map[string]any{"started_at": time.Now()})
		if err != nil {
			return err
		}
		return createNotificationTx(tx, task.PublisherUserID, taskID, event.EventID, "task_started", "执行者已开始开发", "任务已进入开发中。")
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func CancelTask(taskID string, publisherID int64) (*TaskDetailView, error) {
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.PublisherUserID != publisherID {
			return ErrForbidden
		}
		if task.Status != bounty.TaskStatusPublished && task.Status != bounty.TaskStatusSelecting && task.Status != bounty.TaskStatusAssigned {
			return ErrInvalidState
		}
		if err := releaseTaskRewardTx(tx, task, "task_cancelled"); err != nil {
			return err
		}
		if err := transitionTaskTx(tx, task, bounty.TaskStatusCancelled, nil); err != nil {
			return err
		}
		event, err := recordEventTx(tx, taskID, bounty.EventTaskCancelled, publisherID, constant.RoleCommonUser, map[string]any{"reason": "publisher_cancelled"})
		if err != nil {
			return err
		}
		recipients := []int64{}
		if task.AssigneeUserID != nil {
			recipients = append(recipients, *task.AssigneeUserID)
		}
		return createNotificationsTx(tx, recipients, taskID, event, "task_cancelled", "任务已取消", "发布者取消了任务，悬赏额度已释放。")
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, publisherID, constant.RoleCommonUser)
}
