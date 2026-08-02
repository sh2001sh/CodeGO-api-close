package app

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic(err)
	}
	sqlDB.SetMaxOpenConns(1)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingMySQL = false
	platformdb.UsingPostgreSQL = false
	if err := db.AutoMigrate(
		&identityschema.User{},
		&billingschema.BillingAccount{},
		&billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{},
		&billingschema.BillingReservation{},
		&billingschema.BillingSettlement{},
		&billingschema.BillingOutboxEvent{},
		&bountyschema.BountyTask{},
	); err != nil {
		panic(err)
	}
	if err := bountyschema.AutoMigrateModels(db); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestBountyWorkflowFreezesAndSettlesLedger(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 1000)
	seedBountyUser(t, 2, "executor", 50)

	created, err := CreateTask(1, CreateTaskRequest{
		Title:            "优化控制台首页加载体验",
		Description:      "减少首页首屏阻塞请求，并补充加载、空状态和错误状态。",
		RepoURL:          "https://github.com/example/project",
		TaskType:         "ui",
		Tags:             []string{"React", "性能优化"},
		RewardWalletType: "wallet",
		RewardAmount:     200,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "create-workflow-1",
	})
	require.NoError(t, err)
	require.Equal(t, "published", created.Task.Status)

	retried, err := CreateTask(1, CreateTaskRequest{
		Title:            "优化控制台首页加载体验",
		Description:      "减少首页首屏阻塞请求，并补充加载、空状态和错误状态。",
		RepoURL:          "https://github.com/example/project",
		TaskType:         "ui",
		RewardWalletType: "wallet",
		RewardAmount:     200,
		DeadlineAt:       created.Task.DeadlineAt.Format(time.RFC3339),
		IdempotencyKey:   "create-workflow-1",
	})
	require.NoError(t, err)
	require.Equal(t, created.Task.TaskID, retried.Task.TaskID)

	application, err := SubmitApplication(created.Task.TaskID, 2, CreateApplicationRequest{Message: "我会先检查首屏网络瀑布，再补齐响应式状态。"})
	require.NoError(t, err)
	require.Equal(t, "selecting", application.Task.Status)
	assigned, err := AssignApplication(created.Task.TaskID, 1, application.MyApplication.ApplicationID)
	require.NoError(t, err)
	require.Equal(t, "assigned", assigned.Task.Status)
	started, err := StartTask(created.Task.TaskID, 2)
	require.NoError(t, err)
	require.Equal(t, "in_progress", started.Task.Status)

	request, err := CreateMaterialRequest(created.Task.TaskID, 2, MaterialRequestInput{Content: "请确认移动端目标宽度，并补充当前页面截图。", IsBlocking: true})
	require.NoError(t, err)
	require.Equal(t, "waiting_for_publisher", request.Task.Status)
	requestID := request.MaterialRequests[0].RequestID
	replied, err := ReplyMaterialRequest(created.Task.TaskID, requestID, 1, MaterialReplyInput{Content: "需要适配 375px，截图已放在仓库 docs/mobile.png。", SourceType: "github", SourceURL: "https://github.com/example/project/issues/42"})
	require.NoError(t, err)
	require.Equal(t, "publisher_replied", replied.Task.Status)
	resolved, err := ResolveMaterialRequest(created.Task.TaskID, requestID, 2)
	require.NoError(t, err)
	require.Equal(t, "in_progress", resolved.Task.Status)
	require.True(t, resolved.Task.DeadlineAt.After(created.Task.DeadlineAt))

	submitted, err := CreateSubmission(created.Task.TaskID, 2, SubmissionInput{
		RepoURL:           "https://github.com/example/project/pull/42",
		PullRequestURL:    "https://github.com/example/project/pull/42",
		CommitSHA:         "abcdef1234567",
		CompletionSummary: "完成首页加载优化并补齐移动端状态。",
		EffectImages:      []string{"https://github.com/example/project/assets/1/preview.png"},
		TestReport:        "bun run test 通过",
	})
	require.NoError(t, err)
	require.Equal(t, "reviewing", submitted.Task.Status)

	completed, err := ReviewTask(created.Task.TaskID, 1, ReviewInput{Action: "approve", Comment: "交付证据和补充要求一致。"})
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Task.Status)

	var publisherSnapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, platformdb.DB.Raw("SELECT * FROM billing_balance_snapshots WHERE account_id = (SELECT account_id FROM billing_accounts WHERE owner_id = 1 AND account_type = 'wallet')").Scan(&publisherSnapshot).Error)
	require.Equal(t, int64(800), publisherSnapshot.AvailableBalance)
	require.Equal(t, int64(0), publisherSnapshot.ReservedBalance)
	var executor identityschema.User
	require.NoError(t, platformdb.DB.Where("id = ?", 2).First(&executor).Error)
	require.Equal(t, 250, executor.Quota)
	var paidEvents int64
	require.NoError(t, platformdb.DB.Model(&bountyschema.BountyEvent{}).Where("task_id = ? AND event_type = ?", created.Task.TaskID, "task_reward_paid").Count(&paidEvents).Error)
	require.Equal(t, int64(1), paidEvents)
	var completedEvents int64
	require.NoError(t, platformdb.DB.Model(&bountyschema.BountyEvent{}).Where("task_id = ? AND event_type = ?", created.Task.TaskID, "task_completed").Count(&completedEvents).Error)
	require.Equal(t, int64(1), completedEvents)
}

func TestBountyGuestDetailOnlyExposesPublicTimeline(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 500)

	created, err := CreateTask(1, CreateTaskRequest{
		Title:            "公开任务详情时间线",
		Description:      "验证未登录用户可以查看公开任务和安全的时间线。",
		RepoURL:          "https://github.com/example/project",
		RewardWalletType: "wallet",
		RewardAmount:     100,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "guest-detail-public-timeline",
	})
	require.NoError(t, err)

	detail, err := GetTaskDetail(created.Task.TaskID, 0, 0)
	require.NoError(t, err)
	require.Empty(t, detail.Task.ReservationID)
	require.False(t, detail.Task.CanApply)
	require.False(t, detail.Task.CanDispute)
	require.Empty(t, detail.Applications)
	require.Empty(t, detail.MaterialRequests)
	require.Empty(t, detail.Submissions)
	require.Empty(t, detail.Disputes)
	require.NotEmpty(t, detail.Timeline)
	for _, event := range detail.Timeline {
		require.Nil(t, event.Payload)
	}
	publicTimeline, err := GetTimeline(created.Task.TaskID, 0, 0)
	require.NoError(t, err)
	require.Len(t, publicTimeline, len(detail.Timeline))
	for _, event := range publicTimeline {
		require.Nil(t, event.Payload)
	}
}

func TestBountyDisputeAdminPartialSettlementIsIdempotent(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 1000)
	seedBountyUser(t, 2, "executor", 0)
	seedBountyUser(t, 3, "admin", 0)
	if err := platformdb.DB.Model(&identityschema.User{}).Where("id = ?", 3).Update("role", constant.RoleAdminUser).Error; err != nil {
		t.Fatal(err)
	}

	task, err := CreateTask(1, CreateTaskRequest{
		Title:            "修复 Claude 路由重试逻辑",
		Description:      "修复网络抖动时的重试次数和错误回退逻辑。",
		RepoURL:          "https://github.com/example/project",
		TaskType:         "backend",
		RewardWalletType: "claude",
		RewardAmount:     300,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "create-dispute-1",
	})
	require.NoError(t, err)
	application, err := SubmitApplication(task.Task.TaskID, 2, CreateApplicationRequest{Message: "我会补充重试边界测试。"})
	require.NoError(t, err)
	_, err = AssignApplication(task.Task.TaskID, 1, application.MyApplication.ApplicationID)
	require.NoError(t, err)
	_, err = StartTask(task.Task.TaskID, 2)
	require.NoError(t, err)
	_, err = CreateSubmission(task.Task.TaskID, 2, SubmissionInput{RepoURL: "https://github.com/example/project/commit/abcdef1", CommitSHA: "abcdef1234567", CompletionSummary: "完成重试边界和回退逻辑。", TestReport: "go test ./... 通过"})
	require.NoError(t, err)
	_, err = OpenDispute(task.Task.TaskID, 2, DisputeInput{
		Reason:     "链接校验测试",
		GitHubURLs: []string{"https://example.com/not-github"},
	})
	require.Error(t, err)
	disputed, err := OpenDispute(task.Task.TaskID, 2, DisputeInput{Reason: "验收理由与最终确认内容不一致", DesiredOutcome: "部分结算", EvidenceText: "issue comment"})
	require.NoError(t, err)
	require.Equal(t, "disputed", disputed.Task.Status)
	require.Len(t, disputed.Disputes, 1)
	require.Equal(t, "completed", disputed.Disputes[0].AIStatus)

	resolved, err := ResolveDispute(task.Task.TaskID, 3, AdminResolutionInput{DisputeID: disputed.Disputes[0].DisputeID, ResolutionType: "pay_partial", Amount: 100, Note: "按已完成范围部分结算。"})
	require.NoError(t, err)
	require.Equal(t, "resolved", resolved.Task.Status)
	retried, err := ResolveDispute(task.Task.TaskID, 3, AdminResolutionInput{DisputeID: disputed.Disputes[0].DisputeID, ResolutionType: "pay_partial", Amount: 100})
	require.NoError(t, err)
	require.Equal(t, "resolved", retried.Task.Status)
	var executor identityschema.User
	require.NoError(t, platformdb.DB.Where("id = ?", 2).First(&executor).Error)
	require.Equal(t, 100, executor.ClaudeQuota)
	var settlementCount int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingSettlement{}).Count(&settlementCount).Error)
	require.Equal(t, int64(1), settlementCount)
}

func TestBountyUITaskRequiresEffectImage(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 500)
	seedBountyUser(t, 2, "executor", 0)
	task, err := CreateTask(1, CreateTaskRequest{
		Title:            "优化移动端任务详情页",
		Description:      "需要优化移动端详情页布局和触控操作。",
		RepoURL:          "https://github.com/example/project",
		TaskType:         "ui",
		RewardWalletType: "wallet",
		RewardAmount:     100,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "create-ui-1",
	})
	require.NoError(t, err)
	application, err := SubmitApplication(task.Task.TaskID, 2, CreateApplicationRequest{Message: "可以处理。"})
	require.NoError(t, err)
	_, err = AssignApplication(task.Task.TaskID, 1, application.MyApplication.ApplicationID)
	require.NoError(t, err)
	_, err = StartTask(task.Task.TaskID, 2)
	require.NoError(t, err)
	_, err = CreateSubmission(task.Task.TaskID, 2, SubmissionInput{RepoURL: "https://github.com/example/project/pull/7", CommitSHA: "abcdef1234567", CompletionSummary: "完成移动端详情页优化。", TestReport: "bun run test 通过"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "effect image")
}

func TestBountyDraftCanBeEditedAndPublishedWithoutEarlyReservation(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 500)
	draft, err := SaveDraft(1, CreateTaskRequest{
		Title:            "优化中的任务草稿",
		Description:      "先保存任务内容，稍后补充验收范围并发布。",
		RepoURL:          "https://github.com/example/project",
		TaskType:         "frontend",
		RewardWalletType: "wallet",
		RewardAmount:     150,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "draft-test-1",
	})
	require.NoError(t, err)
	require.Equal(t, "draft", draft.Task.Status)
	var reservations int64
	require.NoError(t, platformdb.DB.Model(&billingschema.BillingReservation{}).Count(&reservations).Error)
	require.Zero(t, reservations)
	updated, err := UpdateDraft(1, draft.Task.TaskID, CreateTaskRequest{
		Title:            "优化完成的任务草稿",
		Description:      "发布前更新任务目标和交付说明，确保执行者理解范围。",
		RepoURL:          "https://github.com/example/project",
		TaskType:         "frontend",
		Tags:             []string{"React"},
		RewardWalletType: "wallet",
		RewardAmount:     200,
		DeadlineAt:       time.Now().Add(48 * time.Hour).Format(time.RFC3339),
	})
	require.NoError(t, err)
	published, err := PublishDraft(1, updated.Task.TaskID)
	require.NoError(t, err)
	require.Equal(t, "published", published.Task.Status)
	require.NotEmpty(t, published.Task.ReservationID)
}

func TestBountyReportAndTimeoutPermissions(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 500)
	seedBountyUser(t, 2, "executor", 0)
	task, err := CreateTask(1, CreateTaskRequest{
		Title:            "验证材料超时和举报边界",
		Description:      "检查参与者可以处理自己的材料请求，旁观者可以提交风险举报。",
		RepoURL:          "https://github.com/example/project",
		RewardWalletType: "wallet",
		RewardAmount:     100,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "timeout-report-test",
	})
	require.NoError(t, err)
	application, err := SubmitApplication(task.Task.TaskID, 2, CreateApplicationRequest{Message: "可以处理。"})
	require.NoError(t, err)
	_, err = AssignApplication(task.Task.TaskID, 1, application.MyApplication.ApplicationID)
	require.NoError(t, err)
	_, err = StartTask(task.Task.TaskID, 2)
	require.NoError(t, err)
	requested, err := CreateMaterialRequest(task.Task.TaskID, 2, MaterialRequestInput{Content: "请补充测试账号", IsBlocking: true})
	require.NoError(t, err)
	request := requested.MaterialRequests[0]
	past := time.Now().Add(-49 * time.Hour)
	require.NoError(t, platformdb.DB.Model(&bountyschema.BountyMaterialRequest{}).Where("request_id = ?", request.RequestID).Updates(map[string]any{"timeout_at": &past}).Error)
	_, err = HandleMaterialTimeout(task.Task.TaskID, request.RequestID, 1, MaterialTimeoutInput{Action: "cancel"})
	require.ErrorIs(t, err, ErrForbidden)
	_, err = HandleMaterialTimeout(task.Task.TaskID, request.RequestID, 2, MaterialTimeoutInput{Action: "extend"})
	require.NoError(t, err)
	_, err = ReportTask(task.Task.TaskID, 3, ReportInput{Reason: "疑似索取密钥", Details: "任务描述要求提交 API key"})
	require.NoError(t, err)
}

func seedBountyUser(t *testing.T, id int, username string, quota int) {
	t.Helper()
	require.NoError(t, platformdb.DB.Create(&identityschema.User{Id: id, Username: username, DisplayName: username, Password: "password123", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", AffCode: fmt.Sprintf("bounty-test-%d", id), Quota: quota, ClaudeQuota: quota}).Error)
}

func resetBountyTestData(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"bounty_notifications",
		"bounty_reports",
		"bounty_events",
		"bounty_disputes",
		"bounty_submissions",
		"bounty_material_replies",
		"bounty_material_requests",
		"bounty_applications",
		"bounty_tasks",
		"billing_outbox_events",
		"billing_settlements",
		"billing_reservations",
		"billing_ledger_entries",
		"billing_balance_snapshots",
		"billing_accounts",
		"users",
	} {
		if err := platformdb.DB.Exec(fmt.Sprintf("DELETE FROM %s", table)).Error; err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
}
