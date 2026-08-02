package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	bounty "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func ReportTask(taskID string, userID int64, input ReportInput) (*TaskDetailView, error) {
	if userID <= 0 {
		return nil, ErrForbidden
	}
	reason := strings.TrimSpace(input.Reason)
	details := strings.TrimSpace(input.Details)
	if reason == "" || len([]rune(reason)) > 64 {
		return nil, fmt.Errorf("report reason is required and must not exceed 64 characters")
	}
	if len([]rune(details)) > 10000 {
		return nil, fmt.Errorf("report details must not exceed 10000 characters")
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var task bountyschema.BountyTask
		if err := tx.Where("task_id = ?", taskID).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTaskNotFound
			}
			return err
		}
		if task.PublisherUserID == userID || (task.AssigneeUserID != nil && *task.AssigneeUserID == userID) {
			return fmt.Errorf("participants should use the dispute workflow")
		}
		var existing int64
		if err := tx.Model(&bountyschema.BountyReport{}).Where("task_id = ? AND reporter_user_id = ? AND status = ?", taskID, userID, "open").Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return fmt.Errorf("an open report already exists for this task")
		}
		report := &bountyschema.BountyReport{TaskID: taskID, ReporterUserID: userID, Reason: reason, Details: details}
		if err := tx.Create(report).Error; err != nil {
			return err
		}
		event, err := recordEventTx(tx, taskID, bounty.EventTaskReported, userID, constant.RoleCommonUser, map[string]any{"report_id": report.ReportID, "reason": reason})
		if err != nil {
			return err
		}
		var admins []identityschema.User
		if err := tx.Where("role >= ?", constant.RoleAdminUser).Find(&admins).Error; err != nil {
			return err
		}
		for index := range admins {
			if err := createNotificationTx(tx, int64(admins[index].Id), taskID, event.EventID, "task_reported", "任务收到举报", "有用户举报了这个任务，请在管理页核查。"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func ListAdminReports() ([]ReportView, error) {
	var reports []bountyschema.BountyReport
	if err := platformdb.DB.Where("status = ?", "open").Order("created_at DESC").Find(&reports).Error; err != nil {
		return nil, err
	}
	return buildReportViews(reports)
}

func ResolveReport(taskID string, adminID int64, input AdminReportResolutionInput) error {
	if strings.TrimSpace(input.ReportID) == "" {
		return fmt.Errorf("report_id is required")
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var report bountyschema.BountyReport
		if err := tx.Where("report_id = ? AND task_id = ?", input.ReportID, taskID).First(&report).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("report not found")
			}
			return err
		}
		if report.Status == "resolved" {
			return nil
		}
		now := time.Now()
		if err := tx.Model(&report).Updates(map[string]any{
			"status":              "resolved",
			"resolved_by_user_id": adminID,
			"resolved_at":         &now,
			"resolution_note":     strings.TrimSpace(input.Note),
		}).Error; err != nil {
			return err
		}
		event, err := recordEventTx(tx, taskID, bounty.EventReportResolved, adminID, constant.RoleAdminUser, map[string]any{"report_id": report.ReportID, "note": input.Note})
		if err != nil {
			return err
		}
		return createNotificationTx(tx, report.ReporterUserID, taskID, event.EventID, "report_resolved", "举报已有处理结果", "管理员已处理你提交的任务举报。")
	}); err != nil {
		return err
	}
	return nil
}

func buildReportViews(items []bountyschema.BountyReport) ([]ReportView, error) {
	ids := make([]int64, 0, len(items)*2)
	for index := range items {
		ids = append(ids, items[index].ReporterUserID)
		if items[index].ResolvedByUserID != nil {
			ids = append(ids, *items[index].ResolvedByUserID)
		}
	}
	users, err := loadUserViewsTx(platformdb.DB, ids)
	if err != nil {
		return nil, err
	}
	views := make([]ReportView, 0, len(items))
	for index := range items {
		item := items[index]
		var resolvedBy *UserView
		if item.ResolvedByUserID != nil {
			resolved := users[*item.ResolvedByUserID]
			resolvedBy = &resolved
		}
		views = append(views, ReportView{
			ReportID: item.ReportID, TaskID: item.TaskID, Reporter: users[item.ReporterUserID], Reason: item.Reason,
			Details: item.Details, Status: item.Status, ResolutionNote: item.ResolutionNote,
			ResolvedBy: resolvedBy, ResolvedAt: item.ResolvedAt, CreatedAt: item.CreatedAt,
		})
	}
	return views, nil
}
