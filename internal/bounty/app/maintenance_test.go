package app

import (
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	bountydomain "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestBountyMaintenanceExpiresPublishedTaskAndReleasesReward(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 500)

	created, err := CreateTask(1, CreateTaskRequest{
		Title:            "过期任务释放冻结额度",
		Description:      "验证未开始的公开任务到期后会释放冻结额度并留下审计记录。",
		RepoURL:          "https://github.com/example/project",
		RewardWalletType: "wallet",
		RewardAmount:     100,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "maintenance-expiry-1",
	})
	require.NoError(t, err)
	past := time.Now().Add(-time.Hour)
	require.NoError(t, platformdb.DB.Model(&bountyschema.BountyTask{}).Where("task_id = ?", created.Task.TaskID).Update("deadline_at", &past).Error)

	require.NoError(t, RunBountyMaintenanceOnce())

	var task bountyschema.BountyTask
	require.NoError(t, platformdb.DB.Where("task_id = ?", created.Task.TaskID).First(&task).Error)
	require.Equal(t, bountydomain.TaskStatusExpired, task.Status)
	var reservation billingschema.BillingReservation
	require.NoError(t, platformdb.DB.Where("reservation_id = ?", task.ReservationID).First(&reservation).Error)
	require.Equal(t, billingschema.BillingReservationStatusReleased, reservation.Status)
	var events int64
	require.NoError(t, platformdb.DB.Model(&bountyschema.BountyEvent{}).Where("task_id = ? AND event_type = ?", task.TaskID, bountydomain.EventTaskExpired).Count(&events).Error)
	require.Equal(t, int64(1), events)
}

func TestBountyMaintenanceAutoSettlesReviewAndNotifiesParticipants(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 500)
	seedBountyUser(t, 2, "executor", 0)

	created, err := CreateTask(1, CreateTaskRequest{
		Title:            "自动结算审核窗口",
		Description:      "验证发布者超过验收窗口没有操作时会自动结算并通知双方。",
		RepoURL:          "https://github.com/example/project",
		RewardWalletType: "wallet",
		RewardAmount:     100,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "maintenance-auto-settle-1",
	})
	require.NoError(t, err)
	application, err := SubmitApplication(created.Task.TaskID, 2, CreateApplicationRequest{Message: "可以完成交付。"})
	require.NoError(t, err)
	_, err = AssignApplication(created.Task.TaskID, 1, application.MyApplication.ApplicationID)
	require.NoError(t, err)
	_, err = StartTask(created.Task.TaskID, 2)
	require.NoError(t, err)
	_, err = CreateSubmission(created.Task.TaskID, 2, SubmissionInput{
		RepoURL:           "https://github.com/example/project/commit/abcdef1",
		CommitSHA:         "abcdef1234567",
		CompletionSummary: "完成任务交付。",
		TestReport:        "go test ./... 通过",
	})
	require.NoError(t, err)
	past := time.Now().Add(-time.Hour)
	require.NoError(t, platformdb.DB.Model(&bountyschema.BountyTask{}).Where("task_id = ?", created.Task.TaskID).Update("review_deadline_at", &past).Error)

	require.NoError(t, RunBountyMaintenanceOnce())

	var task bountyschema.BountyTask
	require.NoError(t, platformdb.DB.Where("task_id = ?", created.Task.TaskID).First(&task).Error)
	require.Equal(t, bountydomain.TaskStatusCompleted, task.Status)
	var autoNotifications int64
	require.NoError(t, platformdb.DB.Model(&bountyschema.BountyNotification{}).Where("task_id = ? AND type = ?", task.TaskID, "task_auto_settled").Count(&autoNotifications).Error)
	require.Equal(t, int64(2), autoNotifications)
}
