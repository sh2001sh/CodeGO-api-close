package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	bounty "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func ReviewTask(taskID string, publisherID int64, input ReviewInput) (*TaskDetailView, error) {
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action != "approve" && action != "request_changes" && action != "reject" && action != "dispute" {
		return nil, fmt.Errorf("unsupported review action")
	}
	comment := strings.TrimSpace(input.Comment)
	if (action == "request_changes" || action == "reject" || action == "dispute") && comment == "" {
		return nil, fmt.Errorf("comment is required for this review action")
	}
	var queuedDisputeID string
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.PublisherUserID != publisherID {
			return ErrForbidden
		}
		if task.Status != bounty.TaskStatusReviewing && task.Status != bounty.TaskStatusSubmitted {
			return ErrInvalidState
		}
		switch action {
		case "approve":
			return settleTaskTx(tx, task, publisherID, constant.RoleCommonUser, task.RewardAmount, bounty.EventReviewApproved, comment, false)
		case "request_changes":
			if task.RevisionCount >= task.RevisionLimit {
				return fmt.Errorf("revision limit reached; open a dispute instead")
			}
			task.RevisionCount++
			if err := transitionTaskTx(tx, task, bounty.TaskStatusChangesRequested, map[string]any{"revision_count": task.RevisionCount, "review_deadline_at": nil, "review_deadline_notified_at": nil}); err != nil {
				return err
			}
			event, err := recordEventTx(tx, taskID, bounty.EventChangesRequested, publisherID, constant.RoleCommonUser, map[string]any{"comment": comment, "revision_count": task.RevisionCount})
			if err != nil {
				return err
			}
			if task.AssigneeUserID != nil {
				return createNotificationTx(tx, *task.AssigneeUserID, taskID, event.EventID, "changes_requested", "发布者要求修改", comment)
			}
			return nil
		default:
			dispute, err := openDisputeTx(tx, task, publisherID, comment, "", "")
			if dispute != nil {
				queuedDisputeID = dispute.DisputeID
			}
			return err
		}
	}); err != nil {
		return nil, err
	}
	if queuedDisputeID != "" {
		queueDisputeAnalysis(queuedDisputeID)
	}
	return GetTaskDetail(taskID, publisherID, constant.RoleCommonUser)
}

func settleTaskTx(tx *gorm.DB, task *bountyschema.BountyTask, actorID int64, role int, amount int64, eventType string, comment string, automatic bool) error {
	if task.AssigneeUserID == nil || *task.AssigneeUserID <= 0 {
		return fmt.Errorf("task has no executor")
	}
	if amount < 0 || amount > task.RewardAmount {
		return fmt.Errorf("settlement amount is outside the reward range")
	}
	var latest bountyschema.BountySubmission
	if err := tx.Where("task_id = ?", task.TaskID).Order("version DESC").First(&latest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("task has no submission")
		}
		return err
	}
	if _, err := billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{
		ReservationID:   task.ReservationID,
		UsageEvidenceID: latest.SubmissionID,
		ActualAmount:    amount,
		IdempotencyKey:  "bounty:" + task.TaskID + ":settle",
	}); err != nil {
		return err
	}
	if amount > 0 {
		var err error
		if task.RewardWalletType == bounty.WalletTypeClaude {
			err = billingapp.CreditClaudeWalletQuotaTx(tx, int(*task.AssigneeUserID), int(amount), "bounty:"+task.TaskID+":credit", "bounty_reward")
		} else {
			err = billingapp.CreditWalletQuotaTx(tx, int(*task.AssigneeUserID), int(amount), "bounty:"+task.TaskID+":credit", "bounty_reward")
		}
		if err != nil {
			return err
		}
	}
	if amount < task.RewardAmount {
		if _, err := recordEventTx(tx, task.TaskID, bounty.EventRewardReleased, actorID, role, map[string]any{"amount": task.RewardAmount - amount, "reason": "settlement_remainder"}); err != nil {
			return err
		}
	}
	wasDisputed := task.Status == bounty.TaskStatusDisputed
	if !wasDisputed {
		if err := transitionTaskTx(tx, task, bounty.TaskStatusCompleted, map[string]any{"review_deadline_at": nil, "review_deadline_notified_at": nil}); err != nil {
			return err
		}
	} else if err := tx.Model(task).Updates(map[string]any{"review_deadline_at": nil, "review_deadline_notified_at": nil}).Error; err != nil {
		return err
	}
	reviewEvent, err := recordEventTx(tx, task.TaskID, eventType, actorID, role, map[string]any{"comment": comment, "amount": amount, "automatic": automatic})
	if err != nil {
		return err
	}
	completedEventID := reviewEvent.EventID
	if !wasDisputed {
		completedEvent, err := recordEventTx(tx, task.TaskID, bounty.EventTaskCompleted, actorID, role, map[string]any{"amount": amount, "automatic": automatic})
		if err != nil {
			return err
		}
		completedEventID = completedEvent.EventID
	}
	paidEvent, err := recordEventTx(tx, task.TaskID, bounty.EventRewardPaid, actorID, role, map[string]any{"amount": amount, "wallet_type": task.RewardWalletType})
	if err != nil {
		return err
	}
	notificationType := "task_completed"
	title := "任务已验收"
	content := fmt.Sprintf("任务已验收，获得 %d 额度。", amount)
	if automatic {
		notificationType = "task_auto_settled"
		title = "任务已自动结算"
		content = fmt.Sprintf("发布者在 72 小时内未操作，任务已自动结算，你获得 %d 额度。", amount)
	}
	if err := createNotificationTx(tx, *task.AssigneeUserID, task.TaskID, completedEventID, notificationType, title, content); err != nil {
		return err
	}
	if automatic {
		return createNotificationTx(tx, task.PublisherUserID, task.TaskID, paidEvent.EventID, notificationType, title, "验收窗口已结束，任务悬赏已自动写入账本。")
	}
	return createNotificationTx(tx, task.PublisherUserID, task.TaskID, paidEvent.EventID, "task_reward_paid", "悬赏已结算", "任务悬赏已按验收结果写入账本。")
}

func releaseTaskRewardTx(tx *gorm.DB, task *bountyschema.BountyTask, reason string) error {
	if task.ReservationID == "" {
		return nil
	}
	_, err := billingdomain.ReleaseReservationTx(tx, billingdomain.ReleaseReservationParams{
		ReservationID:  task.ReservationID,
		IdempotencyKey: "bounty:" + task.TaskID + ":release",
		ReasonCode:     reason,
	})
	if err != nil {
		return err
	}
	_, err = recordEventTx(tx, task.TaskID, bounty.EventRewardReleased, 0, constant.RoleRootUser, map[string]any{"amount": task.RewardAmount, "reason": reason})
	return err
}

func OpenDispute(taskID string, userID int64, input DisputeInput) (*TaskDetailView, error) {
	reason := strings.TrimSpace(input.Reason)
	if reason == "" || len([]rune(reason)) > 10000 {
		return nil, fmt.Errorf("reason is required and must not exceed 10000 characters")
	}
	desiredOutcome := strings.TrimSpace(input.DesiredOutcome)
	if len([]rune(desiredOutcome)) > 2000 {
		return nil, fmt.Errorf("desired_outcome must not exceed 2000 characters")
	}
	evidenceText := strings.TrimSpace(input.EvidenceText)
	if len([]rune(evidenceText)) > 20000 {
		return nil, fmt.Errorf("evidence_text must not exceed 20000 characters")
	}
	if len(input.GitHubURLs) > 20 {
		return nil, fmt.Errorf("at most 20 GitHub URLs may be attached")
	}
	githubURLs := make([]string, 0, len(input.GitHubURLs))
	for _, rawURL := range input.GitHubURLs {
		githubURL := strings.TrimSpace(rawURL)
		if githubURL == "" {
			continue
		}
		if err := bounty.ValidateGitHubURL(githubURL, false); err != nil {
			return nil, fmt.Errorf("github_urls: %w", err)
		}
		githubURLs = append(githubURLs, githubURL)
	}
	combinedEvidence := strings.TrimSpace(strings.Join([]string{strings.Join(githubURLs, "\n"), evidenceText}, "\n"))
	var queuedDisputeID string
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.PublisherUserID != userID && (task.AssigneeUserID == nil || *task.AssigneeUserID != userID) {
			return ErrForbidden
		}
		if !canOpenDispute(*task, userID) || task.Status == bounty.TaskStatusCompleted || task.Status == bounty.TaskStatusCancelled || task.Status == bounty.TaskStatusResolved {
			return ErrInvalidState
		}
		dispute, err := openDisputeTx(tx, task, userID, reason, desiredOutcome, combinedEvidence)
		if dispute != nil {
			queuedDisputeID = dispute.DisputeID
		}
		return err
	}); err != nil {
		return nil, err
	}
	if queuedDisputeID != "" {
		queueDisputeAnalysis(queuedDisputeID)
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func openDisputeTx(tx *gorm.DB, task *bountyschema.BountyTask, openedBy int64, reason string, desiredOutcome string, evidence string) (*bountyschema.BountyDispute, error) {
	var openCount int64
	if err := tx.Model(&bountyschema.BountyDispute{}).Where("task_id = ? AND status = ?", task.TaskID, bounty.DisputeStatusOpen).Count(&openCount).Error; err != nil {
		return nil, err
	}
	if openCount > 0 {
		return nil, fmt.Errorf("task already has an open dispute")
	}
	if task.Status != bounty.TaskStatusDisputed {
		if err := bounty.RequireTransition(task.Status, bounty.TaskStatusDisputed); err != nil {
			return nil, err
		}
	}
	snapshot, err := buildDisputeSnapshotTx(tx, task, reason, desiredOutcome, evidence)
	if err != nil {
		return nil, err
	}
	analysis, err := buildEvidenceAnalysis(snapshot)
	if err != nil {
		return nil, err
	}
	provider, providerConfigured, providerErr := configuredDisputeAIProvider()
	if providerErr != nil {
		return nil, providerErr
	}
	aiModel := "evidence-rule-engine-v1"
	aiStatus := "completed"
	if providerConfigured {
		aiModel = provider.model
		aiStatus = "pending"
	}
	dispute := &bountyschema.BountyDispute{
		TaskID:         task.TaskID,
		OpenedByUserID: openedBy,
		Reason:         strings.TrimSpace(reason),
		DesiredOutcome: strings.TrimSpace(desiredOutcome),
		EvidenceText:   strings.TrimSpace(evidence),
		SnapshotText:   snapshot,
		AIAnalysisText: analysis,
		AIModel:        aiModel,
		AIStatus:       aiStatus,
		Status:         bounty.DisputeStatusOpen,
	}
	if err := tx.Create(dispute).Error; err != nil {
		return nil, err
	}
	if err := transitionTaskTx(tx, task, bounty.TaskStatusDisputed, map[string]any{"review_deadline_at": nil, "review_deadline_notified_at": nil}); err != nil {
		return nil, err
	}
	event, err := recordEventTx(tx, task.TaskID, bounty.EventDisputeOpened, openedBy, constant.RoleCommonUser, map[string]any{"dispute_id": dispute.DisputeID, "reason": reason})
	if err != nil {
		return nil, err
	}
	recipients := []int64{task.PublisherUserID}
	if task.AssigneeUserID != nil {
		recipients = append(recipients, *task.AssigneeUserID)
	}
	if err := createNotificationsTx(tx, recipients, task.TaskID, event, "dispute_opened", "任务进入申诉", "任务已进入平台裁决，悬赏额度保持冻结。"); err != nil {
		return nil, err
	}
	return dispute, nil
}
