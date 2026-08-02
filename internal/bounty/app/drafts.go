package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	bounty "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

func SaveDraft(userID int64, req CreateTaskRequest) (*TaskDetailView, error) {
	if userID <= 0 {
		return nil, ErrForbidden
	}
	input, err := normalizeTaskInput(req, false)
	if err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = "bounty:draft:" + platformruntime.GetUUID()
	}
	if len(idempotencyKey) > 255 {
		return nil, fmt.Errorf("idempotency key is too long")
	}
	task := &bountyschema.BountyTask{
		PublisherUserID:  userID,
		Title:            input.title,
		Description:      input.description,
		RepoURL:          input.repoURL,
		TaskType:         input.taskType,
		TagsText:         input.tagsText,
		RewardWalletType: input.walletType,
		RewardAmount:     input.rewardAmount,
		IdempotencyKey:   idempotencyKey,
		Status:           bounty.TaskStatusDraft,
		DeadlineAt:       input.deadline,
		RevisionLimit:    2,
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var existing bountyschema.BountyTask
		if err := tx.Where("idempotency_key = ?", idempotencyKey).First(&existing).Error; err == nil {
			if existing.PublisherUserID != userID || !sameTaskInput(existing, input) {
				return ErrIdempotencyConflict
			}
			*task = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if _, err := loadUserTx(tx, userID); err != nil {
			return err
		}
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		_, err := recordEventTx(tx, task.TaskID, bounty.EventTaskDraftSaved, userID, constant.RoleCommonUser, map[string]any{"created": true})
		return err
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(task.TaskID, userID, constant.RoleCommonUser)
}

func UpdateDraft(userID int64, taskID string, req CreateTaskRequest) (*TaskDetailView, error) {
	if userID <= 0 {
		return nil, ErrForbidden
	}
	input, err := normalizeTaskInput(req, false)
	if err != nil {
		return nil, err
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.PublisherUserID != userID {
			return ErrForbidden
		}
		if task.Status != bounty.TaskStatusDraft {
			return ErrInvalidState
		}
		task.Title = input.title
		task.Description = input.description
		task.RepoURL = input.repoURL
		task.TaskType = input.taskType
		task.TagsText = input.tagsText
		task.RewardWalletType = input.walletType
		task.RewardAmount = input.rewardAmount
		task.DeadlineAt = input.deadline
		if err := tx.Model(task).Updates(map[string]any{
			"title":              task.Title,
			"description":        task.Description,
			"repo_url":           task.RepoURL,
			"task_type":          task.TaskType,
			"tags_text":          task.TagsText,
			"reward_wallet_type": task.RewardWalletType,
			"reward_amount":      task.RewardAmount,
			"deadline_at":        task.DeadlineAt,
		}).Error; err != nil {
			return err
		}
		_, err = recordEventTx(tx, taskID, bounty.EventTaskDraftSaved, userID, constant.RoleCommonUser, map[string]any{"updated": true})
		return err
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

func PublishDraft(userID int64, taskID string) (*TaskDetailView, error) {
	if userID <= 0 {
		return nil, ErrForbidden
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		task, err := lockTaskTx(tx, taskID)
		if err != nil {
			return err
		}
		if task.PublisherUserID != userID {
			return ErrForbidden
		}
		if task.Status != bounty.TaskStatusDraft {
			return ErrInvalidState
		}
		input := normalizedTaskInput{
			title:        task.Title,
			description:  task.Description,
			repoURL:      task.RepoURL,
			taskType:     task.TaskType,
			tagsText:     task.TagsText,
			walletType:   task.RewardWalletType,
			rewardAmount: task.RewardAmount,
			deadline:     task.DeadlineAt,
		}
		if _, err := normalizeTaskInput(CreateTaskRequest{
			Title:            input.title,
			Description:      input.description,
			RepoURL:          input.repoURL,
			TaskType:         input.taskType,
			Tags:             parseTags(input.tagsText),
			RewardWalletType: input.walletType,
			RewardAmount:     input.rewardAmount,
			DeadlineAt:       input.deadline.Format(time.RFC3339),
		}, true); err != nil {
			return err
		}
		account, err := ensureUserAccountTx(tx, userID, task.RewardWalletType)
		if err != nil {
			return err
		}
		expiresAt := task.DeadlineAt.Add(ReviewWindow)
		reservation, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
			AccountID:      account.AccountID,
			RequestID:      task.TaskID,
			WorkflowID:     "bounty:" + task.TaskID,
			ReservedAmount: task.RewardAmount,
			IdempotencyKey: "bounty:" + task.TaskID + ":reserve",
			ExpiresAt:      &expiresAt,
		})
		if err != nil {
			return err
		}
		now := time.Now()
		if err := transitionTaskTx(tx, task, bounty.TaskStatusPublished, map[string]any{
			"reservation_id": reservation.ReservationID,
			"published_at":   now,
		}); err != nil {
			return err
		}
		publishedEvent, err := recordEventTx(tx, task.TaskID, bounty.EventTaskPublished, userID, constant.RoleCommonUser, map[string]any{
			"title":              task.Title,
			"reward_amount":      task.RewardAmount,
			"reward_wallet_type": task.RewardWalletType,
			"deadline_at":        task.DeadlineAt,
		})
		if err != nil {
			return err
		}
		if _, err := recordEventTx(tx, task.TaskID, bounty.EventRewardHeld, userID, constant.RoleCommonUser, map[string]any{
			"reservation_id": reservation.ReservationID,
			"amount":         task.RewardAmount,
			"wallet_type":    task.RewardWalletType,
		}); err != nil {
			return err
		}
		return createNotificationTx(tx, userID, task.TaskID, publishedEvent.EventID, "task_published", "任务已发布", "悬赏额度已冻结，等待执行者申请。")
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(taskID, userID, constant.RoleCommonUser)
}

type normalizedTaskInput struct {
	title        string
	description  string
	repoURL      string
	taskType     string
	tagsText     string
	walletType   string
	rewardAmount int64
	deadline     time.Time
}

func normalizeTaskInput(req CreateTaskRequest, requirePublish bool) (normalizedTaskInput, error) {
	result := normalizedTaskInput{
		title:        strings.TrimSpace(req.Title),
		description:  strings.TrimSpace(req.Description),
		repoURL:      strings.TrimSpace(req.RepoURL),
		taskType:     normalizeTaskType(req.TaskType),
		tagsText:     tagsText(req.Tags),
		rewardAmount: req.RewardAmount,
	}
	if result.title != "" && (len([]rune(result.title)) < 4 || len([]rune(result.title)) > 80) {
		return result, fmt.Errorf("title must contain 4 to 80 characters")
	}
	if requirePublish && result.title == "" {
		return result, fmt.Errorf("title must contain 4 to 80 characters")
	}
	if len([]rune(result.description)) > 20000 || (requirePublish && result.description == "") {
		return result, fmt.Errorf("description is required and must not exceed 20000 characters")
	}
	if result.repoURL != "" {
		if err := bounty.ValidateGitHubURL(result.repoURL, false); err != nil {
			return result, err
		}
	} else if requirePublish {
		return result, fmt.Errorf("repo_url is required")
	}
	if strings.TrimSpace(req.RewardWalletType) == "" && !requirePublish {
		result.walletType = bounty.WalletTypeDefault
	} else {
		walletType, err := bounty.NormalizeWalletType(req.RewardWalletType)
		if err != nil {
			return result, err
		}
		result.walletType = walletType
	}
	if result.rewardAmount < 0 || (requirePublish && result.rewardAmount <= 0) {
		return result, fmt.Errorf("reward_amount must be positive")
	}
	if strings.TrimSpace(req.DeadlineAt) != "" {
		deadline, err := parseDeadline(req.DeadlineAt)
		if err != nil {
			return result, err
		}
		result.deadline = deadline
	} else if requirePublish {
		return result, fmt.Errorf("deadline_at must be later than now")
	}
	return result, nil
}

func sameTaskInput(task bountyschema.BountyTask, input normalizedTaskInput) bool {
	return task.Title == input.title && task.Description == input.description && task.RepoURL == input.repoURL && task.TaskType == input.taskType && task.TagsText == input.tagsText && task.RewardWalletType == input.walletType && task.RewardAmount == input.rewardAmount && task.DeadlineAt.Equal(input.deadline)
}
