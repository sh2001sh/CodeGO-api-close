package activities

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	workflowapp "github.com/sh2001sh/new-api/internal/workflow/app"
	workflowschema "github.com/sh2001sh/new-api/internal/workflow/schema"
	"github.com/sh2001sh/new-api/internal/workflow/temporal/contracts"
	"github.com/sh2001sh/new-api/internal/workflow/temporal/workflows"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type billingRetryTaskAdaptor struct {
	body        string
	actualQuota int
	polls       int
}

func (*billingRetryTaskAdaptor) Init(*relaycommon.RelayInfo) {}
func (a *billingRetryTaskAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	a.polls++
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(a.body))}, nil
}
func (*billingRetryTaskAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	var result relaycommon.TaskInfo
	err := json.Unmarshal(body, &result)
	return &result, err
}
func (a *billingRetryTaskAdaptor) AdjustBillingOnComplete(*workflowschema.Task, *relaycommon.TaskInfo) int {
	return a.actualQuota
}

func TestAsyncTaskWorkflowRetriesAtomicSubscriptionBilling(t *testing.T) {
	for _, test := range []struct {
		name       string
		platform   constant.TaskPlatform
		body       string
		actual     int
		timeout    bool
		legacy     bool
		settlement string
	}{
		{name: "video_refund", body: `{"status":"FAILURE","reason":"provider failed"}`, actual: 0, settlement: "refunded"},
		{name: "video_completion_difference", body: `{"status":"SUCCESS","url":"https://example.com/result.mp4"}`, actual: 3000, settlement: "settled"},
		{name: "suno_refund", platform: constant.TaskPlatformSuno, body: `{"code":"success","data":[{"task_id":"task-retry","status":"FAILURE","fail_reason":"provider failed","data":{}}]}`, actual: 0, settlement: "refunded"},
		{name: "timeout_refund", actual: 0, timeout: true, settlement: "refunded"},
		{name: "legacy_timeout_preserves_no_refund", actual: 5000, timeout: true, legacy: true, settlement: "refunded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB, previousLogDB := platformdb.DB, platformdb.LogDB
			previousSQLite, previousPostgreSQL := platformdb.UsingSQLite, platformdb.UsingPostgreSQL
			previousAdaptor := workflowapp.GetTaskAdaptorFunc
			previousMemory, previousBatch, previousRedis, previousLog := platformconfig.MemoryCacheEnabled, platformconfig.BatchUpdateEnabled, platformcache.RedisEnabled, platformconfig.LogConsumeEnabled
			platformdb.DB, platformdb.LogDB, platformdb.UsingSQLite, platformdb.UsingPostgreSQL = db, db, true, false
			platformconfig.MemoryCacheEnabled, platformconfig.BatchUpdateEnabled, platformcache.RedisEnabled, platformconfig.LogConsumeEnabled = false, false, false, true
			t.Cleanup(func() {
				platformdb.DB, platformdb.LogDB, platformdb.UsingSQLite, platformdb.UsingPostgreSQL = previousDB, previousLogDB, previousSQLite, previousPostgreSQL
				workflowapp.GetTaskAdaptorFunc = previousAdaptor
				platformconfig.MemoryCacheEnabled, platformconfig.BatchUpdateEnabled, platformcache.RedisEnabled, platformconfig.LogConsumeEnabled = previousMemory, previousBatch, previousRedis, previousLog
				_ = sqlDB.Close()
			})
			require.NoError(t, db.AutoMigrate(&workflowschema.Task{}, &workflowschema.WorkflowTaskWorkflow{}, &workflowschema.WorkflowTaskSnapshot{}, &workflowschema.WorkflowTaskTerminalResult{}, &commerceschema.SubscriptionPlan{}, &commerceschema.UserSubscription{}, &identityschema.User{}, &gatewayschema.Channel{}, &auditschema.Log{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}))
			require.NoError(t, db.Create(&identityschema.User{Id: 31, Username: "task-retry-user"}).Error)
			baseURL := "https://example.com"
			require.NoError(t, db.Create(&gatewayschema.Channel{Id: 31, BaseURL: &baseURL, Key: "test-key"}).Error)
			require.NoError(t, db.Create(&commerceschema.SubscriptionPlan{Id: 31, Title: "Task test", TotalAmount: 100000}).Error)
			require.NoError(t, db.Create(&commerceschema.UserSubscription{Id: 31, UserId: 31, PlanId: 31, AmountTotal: 100000, AmountUsed: 50000}).Error)
			task := &workflowschema.Task{TaskID: "task-retry", Platform: test.platform, UserId: 31, ChannelId: 31, Status: workflowschema.TaskStatusInProgress, Progress: "30%", Quota: 5000, SubmitTime: time.Now().Unix(), Data: json.RawMessage(`{}`), Properties: workflowschema.Properties{OriginModelName: "task-model"}, PrivateData: workflowschema.TaskPrivateData{BillingSource: "subscription", SubscriptionId: 31}}
			if test.legacy {
				task.SubmitTime = legacyTaskCutoffUnix - 1
			}
			require.NoError(t, db.Create(task).Error)
			adaptor := &billingRetryTaskAdaptor{body: test.body, actualQuota: test.actual}
			workflowapp.GetTaskAdaptorFunc = func(constant.TaskPlatform) workflowapp.TaskPollingAdaptor { return adaptor }
			writes := 0
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:one-subscription-failure", func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "UserSubscription" {
					writes++
					if writes == 1 {
						tx.AddError(errors.New("transient subscription projection failure"))
					}
				}
			}))
			input := contracts.AsyncTaskWorkflowInput{PublicTaskID: task.TaskID, TimeoutAt: time.Now().Add(time.Hour)}
			if test.timeout {
				input.TimeoutAt = time.Now().Add(-time.Minute)
			}
			for replay := 0; replay < 2; replay++ {
				var suite testsuite.WorkflowTestSuite
				env := suite.NewTestWorkflowEnvironment()
				env.SetTestTimeout(15 * time.Second)
				taskActivities := &TaskActivities{}
				env.RegisterActivityWithOptions(taskActivities.RecordTaskWorkflow, activity.RegisterOptions{Name: contracts.ActivityRecordTaskWorkflow})
				env.RegisterActivityWithOptions(taskActivities.SubmitAsyncTask, activity.RegisterOptions{Name: contracts.ActivitySubmitAsyncTask})
				env.RegisterActivityWithOptions(taskActivities.PollAsyncTaskStatus, activity.RegisterOptions{Name: contracts.ActivityPollAsyncTaskStatus})
				env.RegisterActivityWithOptions(taskActivities.RecordTaskSnapshot, activity.RegisterOptions{Name: contracts.ActivityRecordTaskSnapshot})
				env.RegisterActivityWithOptions(taskActivities.FinalizeTaskTerminalState, activity.RegisterOptions{Name: contracts.ActivityFinalizeTaskTerminalState})
				env.RegisterActivityWithOptions(taskActivities.ProjectTaskResult, activity.RegisterOptions{Name: contracts.ActivityProjectTaskResult})
				env.RegisterActivityWithOptions((&BillingActivities{}).RefundReference, activity.RegisterOptions{Name: contracts.ActivityRefundReference})
				env.ExecuteWorkflow(workflows.AsyncTaskWorkflow, input)
				require.True(t, env.IsWorkflowCompleted())
				require.NoError(t, env.GetWorkflowError())
				var output contracts.AsyncTaskWorkflowOutput
				require.NoError(t, env.GetWorkflowResult(&output))
				require.Equal(t, test.settlement, output.SettlementStatus)
			}
			expectedWrites, expectedAdjustments := 2, int64(1)
			if test.legacy {
				expectedWrites, expectedAdjustments = 0, 0
			}
			require.Equal(t, expectedWrites, writes, "real activity retry must attempt failed subscription transactions again; legacy timeouts keep their no-refund policy")
			if test.timeout {
				require.Zero(t, adaptor.polls)
			} else {
				require.Equal(t, 2, adaptor.polls, "the existing terminal fast path must not swallow the failed billing attempt")
			}
			var reloaded workflowschema.Task
			require.NoError(t, db.First(&reloaded, task.ID).Error)
			require.Equal(t, test.actual, reloaded.Quota)
			var subscription commerceschema.UserSubscription
			require.NoError(t, db.First(&subscription, 31).Error)
			require.EqualValues(t, 50000-5000+test.actual, subscription.AmountUsed)
			var count int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("reason_code = ?", "subscription_usage_adjustment").Count(&count).Error)
			require.EqualValues(t, expectedAdjustments, count)
			require.NoError(t, db.Model(&auditschema.Log{}).Count(&count).Error)
			require.EqualValues(t, expectedAdjustments, count, "completed workflow replay must not duplicate adjustment logs")
			t.Logf("%s: subscription transaction attempts=%d, authoritative adjustments=%d after workflow replay", test.name, writes, expectedAdjustments)
		})
	}
}
