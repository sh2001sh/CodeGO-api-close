package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	bounty "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func CreateSubmission(taskID string, userID int64, input SubmissionInput) (*TaskDetailView, error) {
	if err := validateSubmissionInput(input); err != nil {
		return nil, err
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.AssigneeUserID == nil || *task.AssigneeUserID != userID {
			return ErrForbidden
		}
		if task.Status != bounty.TaskStatusInProgress && task.Status != bounty.TaskStatusChangesRequested && task.Status != bounty.TaskStatusPublisherReplied {
			return ErrInvalidState
		}
		var openBlockingRequests int64
		if err := tx.Model(&bountyschema.BountyMaterialRequest{}).
			Where("task_id = ? AND is_blocking = ? AND status IN ?", taskID, true, []string{bounty.MaterialStatusOpen, bounty.MaterialStatusReplied, bounty.MaterialStatusAwaitingConfirmation}).
			Count(&openBlockingRequests).Error; err != nil {
			return err
		}
		if openBlockingRequests > 0 {
			return fmt.Errorf("resolve all blocking material requests before submitting")
		}
		if bounty.IsTaskTypeUI(task.TaskType, parseTags(task.TagsText)) && len(input.EffectImages) == 0 {
			return fmt.Errorf("UI tasks require at least one GitHub effect image")
		}
		var latest bountyschema.BountySubmission
		version := 1
		if err := tx.Where("task_id = ?", taskID).Order("version DESC").First(&latest).Error; err == nil {
			version = latest.Version + 1
			if err := tx.Model(&latest).Update("status", bounty.SubmissionStatusSuperseded).Error; err != nil {
				return err
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		submission := &bountyschema.BountySubmission{
			TaskID:            taskID,
			ExecutorUserID:    userID,
			Version:           version,
			RepoURL:           strings.TrimSpace(input.RepoURL),
			IssueURL:          strings.TrimSpace(input.IssueURL),
			PullRequestURL:    strings.TrimSpace(input.PullRequestURL),
			CommitSHA:         strings.TrimSpace(input.CommitSHA),
			CompletionSummary: strings.TrimSpace(input.CompletionSummary),
			EffectImagesText:  effectImagesText(input.EffectImages),
			TestReport:        strings.TrimSpace(input.TestReport),
			KnownLimitations:  strings.TrimSpace(input.KnownLimitations),
			Status:            bounty.SubmissionStatusSubmitted,
		}
		if err := tx.Create(submission).Error; err != nil {
			return err
		}
		reviewDeadline := time.Now().Add(ReviewWindow)
		if err := transitionTaskTx(tx, task, bounty.TaskStatusSubmitted, map[string]any{"review_deadline_at": reviewDeadline, "review_deadline_notified_at": nil}); err != nil {
			return err
		}
		event, err := recordEventTx(tx, taskID, bounty.EventSubmissionCreated, userID, constant.RoleCommonUser, map[string]any{
			"submission_id": submission.SubmissionID,
			"version":       version,
			"commit_sha":    submission.CommitSHA,
		})
		if err != nil {
			return err
		}
		if err := transitionTaskTx(tx, task, bounty.TaskStatusReviewing, nil); err != nil {
			return err
		}
		if _, err := recordEventTx(tx, taskID, bounty.EventReviewStarted, userID, constant.RoleCommonUser, map[string]any{"review_deadline_at": reviewDeadline, "phase": "reviewing"}); err != nil {
			return err
		}
		return createNotificationTx(tx, task.PublisherUserID, taskID, event.EventID, "submission_created", "收到新的交付", "执行者已提交 GitHub 交付，请在 72 小时内验收。")
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func validateSubmissionInput(input SubmissionInput) error {
	if err := bounty.ValidateGitHubURL(input.RepoURL, false); err != nil {
		return fmt.Errorf("repo_url: %w", err)
	}
	if err := bounty.ValidateCommitSHA(input.CommitSHA); err != nil {
		return err
	}
	completionSummary := strings.TrimSpace(input.CompletionSummary)
	if completionSummary == "" || len([]rune(completionSummary)) > 20000 {
		return fmt.Errorf("completion_summary is required and must not exceed 20000 characters")
	}
	testReport := strings.TrimSpace(input.TestReport)
	if testReport == "" || len([]rune(testReport)) > 20000 {
		return fmt.Errorf("test_report is required and must not exceed 20000 characters")
	}
	if len([]rune(strings.TrimSpace(input.KnownLimitations))) > 20000 {
		return fmt.Errorf("known_limitations must not exceed 20000 characters")
	}
	for _, value := range []string{input.IssueURL, input.PullRequestURL} {
		if strings.TrimSpace(value) != "" {
			if err := bounty.ValidateGitHubURL(value, false); err != nil {
				return err
			}
		}
	}
	for _, image := range input.EffectImages {
		if err := bounty.ValidateGitHubURL(image, true); err != nil {
			return fmt.Errorf("effect_images: %w", err)
		}
	}
	return nil
}
