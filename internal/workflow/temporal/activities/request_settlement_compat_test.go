package activities

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/workflow/temporal/contracts"
	"github.com/sh2001sh/new-api/internal/workflow/temporal/workflows"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRequestSettlementWorkflowRetainsNativeIdentityContract(t *testing.T) {
	for _, test := range []struct {
		name                           string
		pack, previousFailedProjection bool
	}{
		{name: "wallet"},
		{name: "legacy_subscription"},
		{name: "credit_pack", pack: true},
		{name: "credit_pack_after_original_projection_failed", pack: true, previousFailedProjection: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB, previousSQLite, previousPostgreSQL := platformdb.DB, platformdb.UsingSQLite, platformdb.UsingPostgreSQL
			platformdb.DB, platformdb.UsingSQLite, platformdb.UsingPostgreSQL = db, true, false
			t.Cleanup(func() {
				platformdb.DB, platformdb.UsingSQLite, platformdb.UsingPostgreSQL = previousDB, previousSQLite, previousPostgreSQL
				_ = sqlDB.Close()
			})
			require.NoError(t, db.AutoMigrate(&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}, &gatewayschema.RequestExecution{}, &gatewayschema.GatewayRoutePlan{}, &gatewayschema.ExecutionAttempt{}, &gatewayschema.UsageEvidence{}, &gatewayschema.RequestAudit{}, &auditschema.Log{}, &marketplaceschema.Settlement{}))
			publicRequest := "http-" + test.name
			fundingRequest := publicRequest
			if test.pack {
				fundingRequest = "cp:internal-" + test.name
			}
			account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: 22})
			require.NoError(t, err)
			_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1000, IdempotencyKey: "purchase"})
			require.NoError(t, err)
			reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, RequestID: fundingRequest, ReservedAmount: 100, IdempotencyKey: "native:" + fundingRequest + ":reserve"})
			require.NoError(t, err)
			settlement, err := billingdomain.SettleReservation(billingdomain.SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: 80, UsageEvidenceID: fundingRequest, IdempotencyKey: "native:" + fundingRequest + ":settle"})
			require.NoError(t, err)
			require.NoError(t, db.Create(&auditschema.Log{RequestId: publicRequest, Type: auditschema.LogTypeConsume, Quota: 80}).Error)
			require.NoError(t, db.Create(&gatewayschema.RequestAudit{RequestID: publicRequest, Status: gatewayschema.RequestAuditStatusSucceeded}).Error)
			require.NoError(t, db.Create(&marketplaceschema.Settlement{RequestID: publicRequest, GroupID: "market", OwnerUserID: 11, ConsumerUserID: 22, SettlementGrossAmount: 80, OwnerNetAmount: 76}).Error)
			input := contracts.RequestSettlementWorkflowInput{WorkflowVersion: "v1", RequestID: fundingRequest, UsageEvidenceID: publicRequest, UserID: 22, AccountID: account.AccountID, ReservationID: reservation.ReservationID, SettlementID: settlement.SettlementID, ReservedAmount: 100, ActualAmount: 80}
			gateway, billing := &GatewayActivities{}, &BillingActivities{}
			var failedExecution string
			if test.previousFailedProjection {
				badInput := input
				badInput.RequestID = publicRequest
				created, err := gateway.CreateRequestExecution(context.Background(), badInput)
				require.NoError(t, err)
				failedExecution = created.ExecutionID
				_, err = billing.CreateReservation(context.Background(), badInput)
				require.ErrorIs(t, err, billingdomain.ErrLedgerConflict, "original producer must reproduce the retained worker's real rejection")
			}
			var output contracts.RequestSettlementWorkflowOutput
			for replay := 0; replay < 2; replay++ {
				var suite testsuite.WorkflowTestSuite
				env := suite.NewTestWorkflowEnvironment()
				env.SetTestTimeout(5 * time.Second)
				// These implementations are byte-identical to retained ce9219a.
				env.RegisterActivityWithOptions(gateway.CreateRequestExecution, activity.RegisterOptions{Name: contracts.ActivityCreateRequestExecution})
				env.RegisterActivityWithOptions(billing.CreateReservation, activity.RegisterOptions{Name: contracts.ActivityCreateReservation})
				env.RegisterActivityWithOptions(gateway.ExecuteProviderRequest, activity.RegisterOptions{Name: contracts.ActivityExecuteProviderRequest})
				env.RegisterActivityWithOptions(gateway.CollectUsageEvidence, activity.RegisterOptions{Name: contracts.ActivityCollectUsageEvidence})
				env.RegisterActivityWithOptions(billing.CreateSettlement, activity.RegisterOptions{Name: contracts.ActivityCreateSettlement})
				env.RegisterActivityWithOptions(gateway.PublishRequestSettledEvent, activity.RegisterOptions{Name: contracts.ActivityPublishRequestSettled})
				env.ExecuteWorkflow(workflows.RequestSettlementWorkflow, input)
				require.True(t, env.IsWorkflowCompleted())
				require.NoError(t, env.GetWorkflowError())
				require.NoError(t, env.GetWorkflowResult(&output))
				require.Equal(t, reservation.ReservationID, output.ReservationID)
				require.Equal(t, settlement.SettlementID, output.SettlementID)
				require.Equal(t, publicRequest, output.UsageEvidenceID)
			}
			var evidence gatewayschema.UsageEvidence
			require.NoError(t, db.First(&evidence, "usage_evidence_id = ?", publicRequest).Error)
			var execution gatewayschema.RequestExecution
			require.NoError(t, db.First(&execution, "execution_id = ?", evidence.ExecutionID).Error)
			require.Equal(t, fundingRequest, execution.RequestID)
			require.Equal(t, reservation.ReservationID, execution.ReservationID)
			require.Equal(t, settlement.SettlementID, execution.SettlementID)
			require.Equal(t, gatewayschema.RequestExecutionStatusSettled, execution.Status)
			for _, model := range []any{&auditschema.Log{}, &gatewayschema.RequestAudit{}, &marketplaceschema.Settlement{}} {
				var count int64
				require.NoError(t, db.Model(model).Where("request_id = ?", evidence.UsageEvidenceID).Count(&count).Error)
				require.EqualValues(t, 1, count, "public HTTP correlation remains available")
			}
			for _, model := range []any{&billingschema.BillingSettlement{}, &gatewayschema.UsageEvidence{}} {
				var count int64
				require.NoError(t, db.Model(model).Count(&count).Error)
				require.EqualValues(t, 1, count, "replaying the completed projection must not settle or duplicate evidence")
			}
			var attempts int64
			require.NoError(t, db.Model(&gatewayschema.ExecutionAttempt{}).Where("execution_id = ?", execution.ExecutionID).Count(&attempts).Error)
			require.EqualValues(t, 1, attempts, "projection replay cannot create another provider attempt")
			var balance billingschema.BillingBalanceSnapshot
			require.NoError(t, db.First(&balance, "account_id = ?", account.AccountID).Error)
			require.EqualValues(t, 920, balance.AvailableBalance)
			require.EqualValues(t, 80, balance.ConsumedTotal)
			badAccount := input
			badAccount.AccountID = "foreign-account"
			_, err = billing.CreateReservation(context.Background(), badAccount)
			require.ErrorIs(t, err, billingdomain.ErrLedgerConflict, "the original identity guard stays active")
			if failedExecution != "" {
				require.NotEqual(t, failedExecution, execution.ExecutionID)
				var previous gatewayschema.RequestExecution
				require.NoError(t, db.First(&previous, "execution_id = ?", failedExecution).Error)
				require.Equal(t, gatewayschema.RequestExecutionStatusRecorded, previous.Status)
			}
			t.Logf("workflow and replay completed; funding=%s HTTP=%s settlements=1 consumed=80", fundingRequest, publicRequest)
		})
	}
}
