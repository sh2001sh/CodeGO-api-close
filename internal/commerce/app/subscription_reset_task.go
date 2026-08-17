package app

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	"github.com/sh2001sh/new-api/internal/platform/logger"
)

const (
	subscriptionMaintenanceTickInterval     = 1 * time.Minute
	subscriptionMaintenanceBatchSize        = 300
	subscriptionCleanupInterval             = 30 * time.Minute
	subscriptionProjectionReconcileInterval = 15 * time.Minute
	subscriptionLuckyBackfillInterval       = 10 * time.Minute
)

var (
	subscriptionMaintenanceOnce         sync.Once
	subscriptionMaintenanceRunning      atomic.Bool
	subscriptionCleanupLast             atomic.Int64
	subscriptionProjectionReconcileLast atomic.Int64
	subscriptionLuckyBackfillLast       atomic.Int64
)

// StartSubscriptionMaintenanceTask owns non-workflow subscription maintenance.
// Durable periodic quota resets are scheduled by workflow-worker through Temporal.
func StartSubscriptionMaintenanceTask() {
	subscriptionMaintenanceOnce.Do(func() {
		if !platformconfig.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("subscription maintenance task started: tick=%s", subscriptionMaintenanceTickInterval))
			ticker := time.NewTicker(subscriptionMaintenanceTickInterval)
			defer ticker.Stop()

			runSubscriptionMaintenanceOnce()
			for range ticker.C {
				runSubscriptionMaintenanceOnce()
			}
		})
	})
}

func runSubscriptionMaintenanceOnce() {
	if !subscriptionMaintenanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer subscriptionMaintenanceRunning.Store(false)

	ctx := context.Background()
	totalExpiredTopUps := 0
	for {
		n, err := ExpireDueTopUps(subscriptionMaintenanceBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("pending topup expiry task failed: %v", err))
			break
		}
		totalExpiredTopUps += n
		if n < subscriptionMaintenanceBatchSize {
			break
		}
	}
	totalExpiredBlindBoxOrders := 0
	for {
		n, err := ExpireDueBlindBoxOrders(subscriptionMaintenanceBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("pending blind-box order expiry task failed: %v", err))
			break
		}
		totalExpiredBlindBoxOrders += n
		if n < subscriptionMaintenanceBatchSize {
			break
		}
	}
	totalExpired := 0
	for {
		n, err := ExpireDueSubscriptions(subscriptionMaintenanceBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("subscription expire task failed: %v", err))
			return
		}
		if n == 0 {
			break
		}
		totalExpired += n
		if n < subscriptionMaintenanceBatchSize {
			break
		}
	}
	lastCleanup := time.Unix(subscriptionCleanupLast.Load(), 0)
	if time.Since(lastCleanup) >= subscriptionCleanupInterval {
		if _, err := CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600); err == nil {
			subscriptionCleanupLast.Store(time.Now().Unix())
		}
	}
	lastProjectionReconcile := time.Unix(subscriptionProjectionReconcileLast.Load(), 0)
	if time.Since(lastProjectionReconcile) >= subscriptionProjectionReconcileInterval {
		if reconciled, err := ReconcileActiveSubscriptionLedgerProjections(subscriptionMaintenanceBatchSize); err == nil {
			subscriptionProjectionReconcileLast.Store(time.Now().Unix())
			if platformconfig.DebugEnabled && reconciled > 0 {
				logger.LogDebug(ctx, "subscription projection reconciliation: updated_count=%d", reconciled)
			}
		} else {
			logger.LogWarn(ctx, fmt.Sprintf("subscription projection reconciliation failed: %v", err))
		}
	}
	lastLuckyBackfill := time.Unix(subscriptionLuckyBackfillLast.Load(), 0)
	if time.Since(lastLuckyBackfill) >= subscriptionLuckyBackfillInterval {
		if result, err := BackfillDailyLuckyNumbers(); err == nil {
			subscriptionLuckyBackfillLast.Store(time.Now().Unix())
			if platformconfig.DebugEnabled && result.Created > 0 {
				logger.LogDebug(ctx, fmt.Sprintf("daily lucky number backfill: created=%d scanned=%d", result.Created, result.Scanned))
			}
		} else {
			logger.LogWarn(ctx, fmt.Sprintf("daily lucky number backfill failed: %v", err))
		}
		if merged, err := ReconcileMonthlyPassProps(); err == nil {
			if platformconfig.DebugEnabled && merged > 0 {
				logger.LogDebug(ctx, fmt.Sprintf("monthly pass prop reconciliation: merged=%d", merged))
			}
		} else {
			logger.LogWarn(ctx, fmt.Sprintf("monthly pass prop reconciliation failed: %v", err))
		}
	}
	if platformconfig.DebugEnabled && (totalExpired > 0 || totalExpiredTopUps > 0 || totalExpiredBlindBoxOrders > 0) {
		logger.LogDebug(ctx, "commerce maintenance: expired_subscriptions=%d expired_topups=%d expired_blind_box_orders=%d", totalExpired, totalExpiredTopUps, totalExpiredBlindBoxOrders)
	}
}
