package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"github.com/sh2001sh/new-api/internal/workflow/temporal/contracts"
	"go.temporal.io/api/serviceerror"
	temporalclient "go.temporal.io/sdk/client"
)

var (
	orderWorkflowClient           temporalclient.Client
	orderWorkflowClientErr        error
	orderWorkflowClientMu         sync.Mutex
	orderWorkflowClientRetryAt    time.Time
	orderWorkflowDial             = temporalclient.DialContext
	orderFulfillmentSchedulerOnce sync.Once
)

const (
	orderFulfillmentScheduleInterval = time.Minute
	orderFulfillmentScheduleBatch    = 300
)

// StartOrderFulfillmentWorkflow records a durable, idempotent workflow for an
// order already committed by the payment callback transaction.
func StartOrderFulfillmentWorkflow(ctx context.Context, order *commerceschema.SubscriptionOrder) error {
	if order == nil || order.TradeNo == "" {
		return fmt.Errorf("paid subscription order is required")
	}
	client, err := orderFulfillmentTemporalClient(ctx)
	if err != nil {
		return err
	}
	return startOrderFulfillmentWorkflow(ctx, client, order)
}

func startOrderFulfillmentWorkflow(ctx context.Context, client temporalclient.Client, order *commerceschema.SubscriptionOrder) error {
	if client == nil || order == nil {
		return fmt.Errorf("temporal client and subscription order are required")
	}
	_, err := client.ExecuteWorkflow(ctx, temporalclient.StartWorkflowOptions{
		ID:                       orderFulfillmentWorkflowID(order.TradeNo),
		TaskQueue:                platformconfig.GetEnvOrDefaultString("TEMPORAL_TASK_QUEUE_ORDERS", "workflow-orders"),
		WorkflowExecutionTimeout: time.Hour,
		WorkflowRunTimeout:       time.Hour,
		WorkflowTaskTimeout:      15 * time.Second,
	}, contracts.WorkflowOrderFulfillment, contracts.OrderFulfillmentWorkflowInput{
		WorkflowVersion: "v1",
		OrderID:         order.TradeNo,
		ProductID:       fmt.Sprintf("%d", order.PlanId),
		PaymentProvider: order.PaymentProvider,
		Amount:          order.Money,
	})
	if _, alreadyStarted := err.(*serviceerror.WorkflowExecutionAlreadyStarted); alreadyStarted {
		return nil
	}
	return err
}

func orderFulfillmentWorkflowID(tradeNo string) string {
	return "order-fulfillment-" + tradeNo
}

// StartPendingOrderFulfillmentScheduler retries durable workflow starts for
// confirmed payments. It makes callback-to-Temporal handoff recoverable.
func StartPendingOrderFulfillmentScheduler(ctx context.Context) {
	orderFulfillmentSchedulerOnce.Do(func() {
		go func() {
			schedulePendingOrderFulfillments(ctx)
			ticker := time.NewTicker(orderFulfillmentScheduleInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					schedulePendingOrderFulfillments(ctx)
				}
			}
		}()
	})
}

func schedulePendingOrderFulfillments(ctx context.Context) {
	var afterTime int64
	var afterID int
	for {
		if ctx.Err() != nil {
			return
		}
		var orders []commerceschema.SubscriptionOrder
		query := platformdb.DB.WithContext(ctx).
			Where("status = ? AND fulfillment_status = ? AND COALESCE(refund_status, '') = ''", "success", commerceschema.SubscriptionOrderFulfillmentPending)
		if afterID > 0 {
			query = query.Where("complete_time > ? OR (complete_time = ? AND id > ?)", afterTime, afterTime, afterID)
		}
		if err := query.
			Order("complete_time asc, id asc").
			Limit(orderFulfillmentScheduleBatch).
			Find(&orders).Error; err != nil {
			platformobservability.SysLog("load pending order fulfillments: " + err.Error())
			return
		}
		for index := range orders {
			if ctx.Err() != nil {
				return
			}
			if err := StartOrderFulfillmentWorkflow(ctx, &orders[index]); err != nil {
				platformobservability.SysLog(fmt.Sprintf("start pending order fulfillment %s: %v", orders[index].TradeNo, err))
			}
		}
		if len(orders) < orderFulfillmentScheduleBatch {
			return
		}
		last := orders[len(orders)-1]
		afterTime, afterID = last.CompleteTime, last.Id
	}
}

func orderFulfillmentTemporalClient(ctx context.Context) (temporalclient.Client, error) {
	orderWorkflowClientMu.Lock()
	defer orderWorkflowClientMu.Unlock()
	if orderWorkflowClient != nil {
		return orderWorkflowClient, nil
	}
	if time.Now().Before(orderWorkflowClientRetryAt) {
		return nil, orderWorkflowClientErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	orderWorkflowClientRetryAt = time.Now().Add(5 * time.Second)
	hostPort := platformconfig.GetEnvOrDefaultString("TEMPORAL_HOSTPORT", "")
	if hostPort == "" {
		orderWorkflowClientErr = fmt.Errorf("TEMPORAL_HOSTPORT is required for order fulfillment workflow orchestration")
		return nil, orderWorkflowClientErr
	}
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	orderWorkflowClient, orderWorkflowClientErr = orderWorkflowDial(dialCtx, temporalclient.Options{
		HostPort:  hostPort,
		Namespace: platformconfig.GetEnvOrDefaultString("TEMPORAL_NAMESPACE", "default"),
	})
	if orderWorkflowClientErr != nil {
		orderWorkflowClientRetryAt = time.Now().Add(5 * time.Second)
	}
	return orderWorkflowClient, orderWorkflowClientErr
}
