package channelmarket

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ProcessBatchTests never retries a potentially charged upstream call. A stale
// running item is failed explicitly and its request ID remains reconcilable.
func (s *Service) ProcessBatchTests(ctx context.Context, limit int) (int, error) {
	if s.cfg.BatchRelay == nil || s.pool == nil {
		return 0, ErrUnavailable
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	if _, err := s.pool.Exec(ctx, `UPDATE v3_channelmarket.batch_test_items SET status='failed',error_message='测试执行中断，请根据请求记录核对扣费',ended_at=$1 WHERE status='running' AND started_at<$2`, s.cfg.Now(), s.cfg.Now().Add(-5*time.Minute)); err != nil {
		return 0, err
	}
	if err := s.reconcileBatchReceipts(ctx); err != nil {
		return 0, err
	}
	if err := s.finishBatches(ctx); err != nil {
		return 0, err
	}
	for done := 0; done < limit; done++ {
		id, group, request, err := s.claimNextBatchItemTx(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return done, nil
		}
		if err != nil {
			return done, err
		}
		if err := s.runAndRecordBatchItem(ctx, id, group, request); err != nil {
			return done, err
		}
		if err := s.finishBatches(ctx); err != nil {
			return done, err
		}
	}
	return limit, nil
}

// claimNextBatchItemTx locks the oldest queued batch item, marks its batch
// and item rows running, and returns its relay request.
func (s *Service) claimNextBatchItemTx(ctx context.Context) (string, string, BatchRelayRequest, error) {
	var id, group string
	var request BatchRelayRequest
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT i.batch_id,i.group_id,b.owner_user_id,i.internal_group_name,b.model,i.request_id FROM v3_channelmarket.batch_test_items i JOIN v3_channelmarket.batch_tests b ON b.id=i.batch_id WHERE i.status='queued' ORDER BY b.created_at,i.group_id LIMIT 1 FOR UPDATE OF i SKIP LOCKED`).Scan(&id, &group, &request.UserID, &request.Group, &request.Model, &request.RequestID); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE v3_channelmarket.batch_tests SET status='running',updated_at=$2 WHERE id=$1`, id, s.cfg.Now()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `UPDATE v3_channelmarket.batch_test_items SET status='running',started_at=$3 WHERE batch_id=$1 AND group_id=$2`, id, group, s.cfg.Now())
		return e
	})
	return id, group, request, err
}

// runAndRecordBatchItem relays one claimed batch item, reconciles its receipt
// against durable usage logs, and persists the final item status.
func (s *Service) runAndRecordBatchItem(ctx context.Context, id, group string, request BatchRelayRequest) error {
	started := time.Now()
	receipt, status, message, relayOK := s.relayBatchItem(ctx, group, request)
	receipt, status, message, err := s.reconcileBatchItemReceipt(ctx, request, receipt, status, message, relayOK)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE v3_channelmarket.batch_test_items SET status=$3,latency_ms=$4,amount_micro=$5,billing_source=$6,log_created=$7,error_message=$8,ended_at=$9 WHERE batch_id=$1 AND group_id=$2 AND status='running'`, id, group, status, time.Since(started).Milliseconds(), receipt.AmountMicro, receipt.BillingSource, receipt.LogCreated, message, s.cfg.Now())
	return err
}

// relayBatchItem re-validates the item's routing target still matches the
// claimed group, then issues the upstream batch relay call. The returned bool
// reports whether the relay itself succeeded (relayErr == nil), independent
// of any later status downgrade.
func (s *Service) relayBatchItem(ctx context.Context, group string, request BatchRelayRequest) (BatchReceipt, string, string, bool) {
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var receipt BatchReceipt
	relayErr := s.transaction(callCtx, func(tx pgx.Tx) error {
		internal, _, e := batchTarget(callCtx, tx, request.UserID, group, request.Model)
		if e == nil && internal != request.Group {
			return ErrConflict
		}
		return e
	})
	if relayErr == nil {
		receipt, relayErr = s.cfg.BatchRelay(callCtx, request)
	}
	status, message := "passed", ""
	if relayErr != nil {
		status, message = "failed", "测试请求失败，请根据请求记录核对扣费"
	}
	return receipt, status, message, relayErr == nil
}

// reconcileBatchItemReceipt cross-checks the relay receipt's request id and
// looks up the durable usage log that proves the charge actually settled.
// Only durable, owner-scoped usage logs are evidence of a successful charge.
func (s *Service) reconcileBatchItemReceipt(ctx context.Context, request BatchRelayRequest, receipt BatchReceipt, status, message string, relayOK bool) (BatchReceipt, string, string, error) {
	if receipt.RequestID != "" && receipt.RequestID != request.RequestID {
		status, message = "failed", "测试回执的请求编号不匹配"
	}
	receipt.RequestID = request.RequestID
	var amount *int64
	var source string
	err := s.pool.QueryRow(ctx, `SELECT sum(l.amount)::bigint,coalesce(string_agg(DISTINCT coalesce(s.billing_source,a.kind),'+' ORDER BY coalesce(s.billing_source,a.kind)),'') FROM v3_billing.usage_logs l JOIN v3_billing.accounts a ON a.id=l.account_id LEFT JOIN v3_channelmarket.settlements s ON s.request_id=l.request_id WHERE l.request_id=$1 AND l.user_id=$2 AND l.model=$3 AND a.kind IN ('wallet','subscription')`, request.RequestID, request.UserID, request.Model).Scan(&amount, &source)
	if err == nil && amount == nil {
		receipt.LogCreated = false
		receipt.AmountMicro = 0
		receipt.BillingSource = ""
		if relayOK {
			status, message = "failed", "测试请求缺少已结算使用记录"
		}
		return receipt, status, message, nil
	}
	if err != nil {
		return receipt, status, message, err
	}
	receipt.LogCreated = true
	receipt.AmountMicro = *amount
	receipt.BillingSource = source
	return receipt, status, message, nil
}

// Late settlement may land after a timeout or worker crash. Update the receipt
// from durable usage, without ever executing that request again.
func (s *Service) reconcileBatchReceipts(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE v3_channelmarket.batch_test_items i SET amount_micro=u.amount,billing_source=u.source,log_created=true FROM(
	SELECT i.batch_id,i.group_id,sum(l.amount)::bigint AS amount,string_agg(DISTINCT coalesce(s.billing_source,a.kind),'+' ORDER BY coalesce(s.billing_source,a.kind)) AS source
	FROM v3_channelmarket.batch_test_items i JOIN v3_channelmarket.batch_tests b ON b.id=i.batch_id
	JOIN v3_billing.usage_logs l ON l.request_id=i.request_id AND l.user_id=b.owner_user_id AND l.model=b.model
	JOIN v3_billing.accounts a ON a.id=l.account_id AND a.kind IN ('wallet','subscription')
	LEFT JOIN v3_channelmarket.settlements s ON s.request_id=l.request_id
	WHERE i.status='failed' AND NOT i.log_created GROUP BY i.batch_id,i.group_id) u
	WHERE i.batch_id=u.batch_id AND i.group_id=u.group_id AND i.status='failed' AND NOT i.log_created`)
	return err
}
func (s *Service) finishBatches(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE v3_channelmarket.batch_tests b SET status='completed',updated_at=$1 WHERE status IN ('running','queued') AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.batch_test_items i WHERE i.batch_id=b.id AND i.status IN ('queued','running'))`, s.cfg.Now())
	return err
}
