package app

import (
	"testing"
	"time"

	bountydomain "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestAssignApplicationRecordsRejectionAuditAndNotification(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 500)
	seedBountyUser(t, 2, "first-executor", 0)
	seedBountyUser(t, 3, "second-executor", 0)

	created, err := CreateTask(1, CreateTaskRequest{
		Title:            "申请拒绝审计记录",
		Description:      "验证未被选中的申请会有独立的状态事件和站内通知。",
		RepoURL:          "https://github.com/example/project",
		RewardWalletType: "wallet",
		RewardAmount:     100,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "application-rejection-audit-1",
	})
	require.NoError(t, err)
	first, err := SubmitApplication(created.Task.TaskID, 2, CreateApplicationRequest{Message: "我可以完成。"})
	require.NoError(t, err)
	second, err := SubmitApplication(created.Task.TaskID, 3, CreateApplicationRequest{Message: "我也可以完成。"})
	require.NoError(t, err)
	_, err = AssignApplication(created.Task.TaskID, 1, first.MyApplication.ApplicationID)
	require.NoError(t, err)
	require.NotEmpty(t, second.MyApplication.ApplicationID)

	var rejectedEvents int64
	require.NoError(t, platformdb.DB.Model(&bountyschema.BountyEvent{}).Where("task_id = ? AND event_type = ?", created.Task.TaskID, bountydomain.EventApplicationRejected).Count(&rejectedEvents).Error)
	require.Equal(t, int64(1), rejectedEvents)
	var rejectedNotifications int64
	require.NoError(t, platformdb.DB.Model(&bountyschema.BountyNotification{}).Where("task_id = ? AND user_id = ? AND type = ?", created.Task.TaskID, 3, "application_rejected").Count(&rejectedNotifications).Error)
	require.Equal(t, int64(1), rejectedNotifications)
}

func TestSubmissionVersionsAreScopedToEachTask(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 1000)
	seedBountyUser(t, 2, "executor", 0)

	first := createAssignedBountyForSubmissionTest(t, "版本按任务隔离一", "submission-version-scope-1")
	second := createAssignedBountyForSubmissionTest(t, "版本按任务隔离二", "submission-version-scope-2")
	for _, taskID := range []string{first, second} {
		_, err := CreateSubmission(taskID, 2, SubmissionInput{
			RepoURL:           "https://github.com/example/project/commit/abcdef1",
			CommitSHA:         "abcdef1234567",
			CompletionSummary: "完成任务交付。",
			TestReport:        "go test ./... 通过",
		})
		require.NoError(t, err)
	}
}

func createAssignedBountyForSubmissionTest(t *testing.T, title string, idempotencyKey string) string {
	t.Helper()
	created, err := CreateTask(1, CreateTaskRequest{
		Title:            title,
		Description:      "验证不同任务的交付版本从一开始计数且互不冲突。",
		RepoURL:          "https://github.com/example/project",
		RewardWalletType: "wallet",
		RewardAmount:     100,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   idempotencyKey,
	})
	require.NoError(t, err)
	application, err := SubmitApplication(created.Task.TaskID, 2, CreateApplicationRequest{Message: "可以完成。"})
	require.NoError(t, err)
	_, err = AssignApplication(created.Task.TaskID, 1, application.MyApplication.ApplicationID)
	require.NoError(t, err)
	_, err = StartTask(created.Task.TaskID, 2)
	require.NoError(t, err)
	return created.Task.TaskID
}
