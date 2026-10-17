package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/sh2001sh/new-api/internal/workflow/temporal/contracts"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	temporalclient "go.temporal.io/sdk/client"
)

type commercialWorkflowClient struct {
	temporalclient.Client
	execute func(context.Context, temporalclient.StartWorkflowOptions, interface{}, ...interface{}) (temporalclient.WorkflowRun, error)
}

func (c *commercialWorkflowClient) ExecuteWorkflow(ctx context.Context, options temporalclient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalclient.WorkflowRun, error) {
	return c.execute(ctx, options, workflow, args...)
}

func resetCommercialWorkflowClient(t *testing.T) {
	t.Helper()
	oldClient, oldErr, oldRetry, oldDial := orderWorkflowClient, orderWorkflowClientErr, orderWorkflowClientRetryAt, orderWorkflowDial
	orderWorkflowClient, orderWorkflowClientErr, orderWorkflowClientRetryAt = nil, nil, time.Time{}
	t.Cleanup(func() {
		orderWorkflowClient, orderWorkflowClientErr, orderWorkflowClientRetryAt, orderWorkflowDial = oldClient, oldErr, oldRetry, oldDial
	})
}

func TestCommercialWorkflowDialRecoversAfterTransientFailure(t *testing.T) {
	resetCommercialWorkflowClient(t)
	t.Setenv("TEMPORAL_HOSTPORT", "synthetic.example.test:7233")
	client := &commercialWorkflowClient{}
	attempts := 0
	orderWorkflowDial = func(ctx context.Context, options temporalclient.Options) (temporalclient.Client, error) {
		attempts++
		require.Equal(t, "synthetic.example.test:7233", options.HostPort)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
		if attempts == 1 {
			return nil, errors.New("synthetic connection outage")
		}
		return client, nil
	}
	_, err := orderFulfillmentTemporalClient(context.Background())
	require.ErrorContains(t, err, "synthetic connection outage")
	_, err = orderFulfillmentTemporalClient(context.Background())
	require.Error(t, err)
	require.Equal(t, 1, attempts, "backoff must bound dial attempts")
	orderWorkflowClientRetryAt = time.Now().Add(-time.Second)
	got, err := orderFulfillmentTemporalClient(context.Background())
	require.NoError(t, err)
	require.Same(t, client, got)
	require.Equal(t, 2, attempts)
	got, err = orderFulfillmentTemporalClient(context.Background())
	require.NoError(t, err)
	require.Same(t, client, got)
	require.Equal(t, 2, attempts)
}

func TestCommercialWorkflowMissingConfigurationCanRecoverAndCancellationDoesNotDial(t *testing.T) {
	resetCommercialWorkflowClient(t)
	t.Setenv("TEMPORAL_HOSTPORT", "")
	_, err := orderFulfillmentTemporalClient(context.Background())
	require.ErrorContains(t, err, "TEMPORAL_HOSTPORT")
	orderWorkflowClientRetryAt = time.Time{}
	t.Setenv("TEMPORAL_HOSTPORT", "synthetic.example.test:7233")
	attempts := 0
	orderWorkflowDial = func(context.Context, temporalclient.Options) (temporalclient.Client, error) {
		attempts++
		return &commercialWorkflowClient{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = orderFulfillmentTemporalClient(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, attempts)
	_, err = orderFulfillmentTemporalClient(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, attempts)
}

func TestCommercialWorkflowSchedulerReachesPastFailingFirstBatch(t *testing.T) {
	db := setupRedemptionTestDB(t)
	resetCommercialWorkflowClient(t)
	orders := make([]commerceschema.SubscriptionOrder, orderFulfillmentScheduleBatch+5)
	for i := range orders {
		orders[i] = commerceschema.SubscriptionOrder{TradeNo: fmt.Sprintf("commercial-schedule-%04d", i), Status: constant.TopUpStatusSuccess, FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentPending, CompleteTime: 100, PlanId: 7, Money: 64}
	}
	require.NoError(t, db.Create(&orders).Error)
	for _, order := range []commerceschema.SubscriptionOrder{
		{TradeNo: "commercial-refunding", Status: constant.TopUpStatusSuccess, FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentPending, RefundStatus: "processing"},
		{TradeNo: "commercial-completed", Status: constant.TopUpStatusSuccess, FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentCompleted},
		{TradeNo: "commercial-unpaid", Status: constant.TopUpStatusPending, FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentPending},
	} {
		require.NoError(t, db.Create(&order).Error)
	}
	seen := map[string]int{}
	client := &commercialWorkflowClient{execute: func(ctx context.Context, options temporalclient.StartWorkflowOptions, workflow interface{}, args ...interface{}) (temporalclient.WorkflowRun, error) {
		seen[options.ID]++
		require.Equal(t, contracts.WorkflowOrderFulfillment, workflow)
		input := args[0].(contracts.OrderFulfillmentWorkflowInput)
		require.Equal(t, orderFulfillmentWorkflowID(input.OrderID), options.ID)
		if len(seen) <= orderFulfillmentScheduleBatch {
			return nil, errors.New("synthetic unavailable workflow start")
		}
		return nil, &serviceerror.WorkflowExecutionAlreadyStarted{}
	}}
	orderWorkflowClient = client
	schedulePendingOrderFulfillments(context.Background())
	require.Len(t, seen, len(orders))
	for _, order := range orders {
		require.Equal(t, 1, seen[orderFulfillmentWorkflowID(order.TradeNo)])
	}
	var count int64
	require.NoError(t, db.Model(&commerceschema.SubscriptionOrder{}).Where("status = ? AND fulfillment_status = ?", constant.TopUpStatusSuccess, commerceschema.SubscriptionOrderFulfillmentPending).Count(&count).Error)
	require.EqualValues(t, len(orders)+1, count, "failed handoffs and refund hold remain durable")
	t.Logf("scheduler attempted all %d paid orders despite the first %d workflow starts failing; refund, unpaid and completed excluded", len(orders), orderFulfillmentScheduleBatch)
}

func TestCommercialWorkflowPaidCommitSurvivesOutageAndUsesStableRetryID(t *testing.T) {
	db := setupRedemptionTestDB(t)
	configureEpayLateTest(t)
	resetCommercialWorkflowClient(t)
	t.Setenv("TEMPORAL_HOSTPORT", "")
	order, _ := seedCommercialSubscription(t, db, false)
	require.NoError(t, deliverCommercialSubscription(commercialEpayParams(order.TradeNo, order.Money)))
	require.NoError(t, db.First(order, order.Id).Error)
	require.Equal(t, constant.TopUpStatusSuccess, order.Status)
	require.Equal(t, commerceschema.SubscriptionOrderFulfillmentPending, order.FulfillmentStatus)
	var ids []string
	orderWorkflowClient = &commercialWorkflowClient{execute: func(_ context.Context, options temporalclient.StartWorkflowOptions, _ interface{}, _ ...interface{}) (temporalclient.WorkflowRun, error) {
		ids = append(ids, options.ID)
		return nil, &serviceerror.WorkflowExecutionAlreadyStarted{}
	}}
	schedulePendingOrderFulfillments(context.Background())
	schedulePendingOrderFulfillments(context.Background())
	require.Equal(t, []string{orderFulfillmentWorkflowID(order.TradeNo), orderFulfillmentWorkflowID(order.TradeNo)}, ids)
}

func TestCommercialWorkflowSchedulerHonorsCancellation(t *testing.T) {
	db := setupRedemptionTestDB(t)
	resetCommercialWorkflowClient(t)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{TradeNo: fmt.Sprintf("cancel-%d", i), Status: constant.TopUpStatusSuccess, FulfillmentStatus: commerceschema.SubscriptionOrderFulfillmentPending}).Error)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	orderWorkflowClient = &commercialWorkflowClient{execute: func(context.Context, temporalclient.StartWorkflowOptions, interface{}, ...interface{}) (temporalclient.WorkflowRun, error) {
		attempts++
		cancel()
		return nil, context.Canceled
	}}
	schedulePendingOrderFulfillments(ctx)
	require.Equal(t, 1, attempts)
}
