package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformarchive "github.com/sh2001sh/new-api/internal/platform/archivex"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type gatewayExecutionArchiveBundle struct {
	Execution     gatewayschema.RequestExecution   `json:"execution"`
	RoutePlan     *gatewayschema.GatewayRoutePlan  `json:"route_plan,omitempty"`
	Attempts      []gatewayschema.ExecutionAttempt `json:"attempts"`
	UsageEvidence []gatewayschema.UsageEvidence    `json:"usage_evidence"`
}

// ArchiveSettledExecutionsBatch stores complete settled execution trees before
// removing their hot-table rows in one checked transaction.
func ArchiveSettledExecutionsBatch(ctx context.Context, sink platformarchive.Sink, now time.Time, retentionDays, limit int) (int64, error) {
	if sink == nil || platformdb.DB == nil || retentionDays <= 0 || limit <= 0 {
		return 0, nil
	}
	cutoff := now.UTC().AddDate(0, 0, -retentionDays)
	executions := make([]gatewayschema.RequestExecution, 0, limit)
	if err := platformdb.DB.WithContext(ctx).
		Where("status = ? AND updated_at < ?", gatewayschema.RequestExecutionStatusSettled, cutoff).
		Order("updated_at asc, execution_id asc").
		Limit(limit).
		Find(&executions).Error; err != nil {
		return 0, err
	}
	if len(executions) == 0 {
		return 0, nil
	}
	bundles, childCounts, err := loadExecutionArchiveBundles(ctx, executions)
	if err != nil {
		return 0, err
	}
	records := make([]platformarchive.Record, 0, len(bundles))
	for index := range bundles {
		records = append(records, platformarchive.Record{Type: "gateway_execution_bundle", Data: bundles[index]})
	}
	batch := platformarchive.Batch{
		Dataset:   "gateway-executions",
		Partition: executions[0].UpdatedAt.UTC(),
		ID:        gatewayArchiveBatchID(executions),
		Records:   records,
	}
	if err := sink.Store(ctx, batch); err != nil {
		return 0, fmt.Errorf("store gateway execution archive batch: %w", err)
	}
	return deleteArchivedExecutionBundles(ctx, cutoff, executions, childCounts)
}

type executionArchiveChildCounts struct {
	attempts int64
	evidence int64
}

func loadExecutionArchiveBundles(ctx context.Context, executions []gatewayschema.RequestExecution) ([]gatewayExecutionArchiveBundle, executionArchiveChildCounts, error) {
	executionIDs, routePlanIDs := executionArchiveIDs(executions)
	var attempts []gatewayschema.ExecutionAttempt
	if err := platformdb.DB.WithContext(ctx).Where("execution_id IN ?", executionIDs).
		Order("execution_id asc, attempt_no asc, attempt_id asc").Find(&attempts).Error; err != nil {
		return nil, executionArchiveChildCounts{}, err
	}
	var evidence []gatewayschema.UsageEvidence
	if err := platformdb.DB.WithContext(ctx).Where("execution_id IN ?", executionIDs).
		Order("execution_id asc, usage_evidence_id asc").Find(&evidence).Error; err != nil {
		return nil, executionArchiveChildCounts{}, err
	}
	var plans []gatewayschema.GatewayRoutePlan
	if len(routePlanIDs) > 0 {
		if err := platformdb.DB.WithContext(ctx).Where("route_plan_id IN ?", routePlanIDs).Find(&plans).Error; err != nil {
			return nil, executionArchiveChildCounts{}, err
		}
	}
	planByID := make(map[string]gatewayschema.GatewayRoutePlan, len(plans))
	for _, plan := range plans {
		planByID[plan.RoutePlanID] = plan
	}
	attemptsByExecution := make(map[string][]gatewayschema.ExecutionAttempt)
	for _, attempt := range attempts {
		attemptsByExecution[attempt.ExecutionID] = append(attemptsByExecution[attempt.ExecutionID], attempt)
	}
	evidenceByExecution := make(map[string][]gatewayschema.UsageEvidence)
	for _, item := range evidence {
		evidenceByExecution[item.ExecutionID] = append(evidenceByExecution[item.ExecutionID], item)
	}
	bundles := make([]gatewayExecutionArchiveBundle, 0, len(executions))
	for _, execution := range executions {
		bundle := gatewayExecutionArchiveBundle{
			Execution:     execution,
			Attempts:      attemptsByExecution[execution.ExecutionID],
			UsageEvidence: evidenceByExecution[execution.ExecutionID],
		}
		if plan, found := planByID[execution.RoutePlanID]; found {
			bundle.RoutePlan = &plan
		}
		bundles = append(bundles, bundle)
	}
	return bundles, executionArchiveChildCounts{attempts: int64(len(attempts)), evidence: int64(len(evidence))}, nil
}

func deleteArchivedExecutionBundles(ctx context.Context, cutoff time.Time, executions []gatewayschema.RequestExecution, expected executionArchiveChildCounts) (int64, error) {
	executionIDs, routePlanIDs := executionArchiveIDs(executions)
	var deleted int64
	err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked []gatewayschema.RequestExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("execution_id IN ? AND status = ? AND updated_at < ?", executionIDs, gatewayschema.RequestExecutionStatusSettled, cutoff).
			Find(&locked).Error; err != nil {
			return err
		}
		if len(locked) != len(executions) {
			return fmt.Errorf("gateway archive eligibility changed: got %d of %d executions", len(locked), len(executions))
		}
		if err := verifyExecutionArchiveChildren(tx, executionIDs, expected); err != nil {
			return err
		}
		if err := tx.Where("execution_id IN ?", executionIDs).Delete(&gatewayschema.ExecutionAttempt{}).Error; err != nil {
			return err
		}
		if err := tx.Where("execution_id IN ?", executionIDs).Delete(&gatewayschema.UsageEvidence{}).Error; err != nil {
			return err
		}
		result := tx.Where("execution_id IN ?", executionIDs).Delete(&gatewayschema.RequestExecution{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(executions)) {
			return fmt.Errorf("gateway archive delete mismatch: deleted %d of %d executions", result.RowsAffected, len(executions))
		}
		if err := deleteUnreferencedRoutePlans(tx, routePlanIDs); err != nil {
			return err
		}
		deleted = result.RowsAffected
		return nil
	})
	return deleted, err
}

func verifyExecutionArchiveChildren(tx *gorm.DB, executionIDs []string, expected executionArchiveChildCounts) error {
	var attempts, evidence int64
	if err := tx.Model(&gatewayschema.ExecutionAttempt{}).Where("execution_id IN ?", executionIDs).Count(&attempts).Error; err != nil {
		return err
	}
	if err := tx.Model(&gatewayschema.UsageEvidence{}).Where("execution_id IN ?", executionIDs).Count(&evidence).Error; err != nil {
		return err
	}
	if attempts != expected.attempts || evidence != expected.evidence {
		return fmt.Errorf("gateway archive children changed: attempts=%d/%d evidence=%d/%d", attempts, expected.attempts, evidence, expected.evidence)
	}
	return nil
}

func deleteUnreferencedRoutePlans(tx *gorm.DB, routePlanIDs []string) error {
	if len(routePlanIDs) == 0 {
		return nil
	}
	var referenced []string
	if err := tx.Model(&gatewayschema.RequestExecution{}).Where("route_plan_id IN ?", routePlanIDs).
		Distinct().Pluck("route_plan_id", &referenced).Error; err != nil {
		return err
	}
	referencedSet := make(map[string]struct{}, len(referenced))
	for _, id := range referenced {
		referencedSet[id] = struct{}{}
	}
	unreferenced := make([]string, 0, len(routePlanIDs))
	for _, id := range routePlanIDs {
		if id == "" {
			continue
		}
		if _, found := referencedSet[id]; !found {
			unreferenced = append(unreferenced, id)
		}
	}
	if len(unreferenced) == 0 {
		return nil
	}
	return tx.Where("route_plan_id IN ?", unreferenced).Delete(&gatewayschema.GatewayRoutePlan{}).Error
}

func executionArchiveIDs(executions []gatewayschema.RequestExecution) ([]string, []string) {
	executionIDs := make([]string, 0, len(executions))
	routePlanIDs := make([]string, 0, len(executions))
	seenPlans := make(map[string]struct{})
	for _, execution := range executions {
		executionIDs = append(executionIDs, execution.ExecutionID)
		if execution.RoutePlanID == "" {
			continue
		}
		if _, found := seenPlans[execution.RoutePlanID]; !found {
			seenPlans[execution.RoutePlanID] = struct{}{}
			routePlanIDs = append(routePlanIDs, execution.RoutePlanID)
		}
	}
	return executionIDs, routePlanIDs
}

func gatewayArchiveBatchID(executions []gatewayschema.RequestExecution) string {
	hash := sha256.New()
	for _, execution := range executions {
		hash.Write([]byte(execution.ExecutionID))
		hash.Write([]byte{0})
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	return fmt.Sprintf("%d-%s", executions[0].UpdatedAt.UTC().Unix(), digest[:16])
}
