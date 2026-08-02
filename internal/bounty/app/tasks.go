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

func CreateTask(userID int64, req CreateTaskRequest) (*TaskDetailView, error) {
	if userID <= 0 {
		return nil, ErrForbidden
	}
	title := strings.TrimSpace(req.Title)
	description := strings.TrimSpace(req.Description)
	if length := len([]rune(title)); length < 4 || length > 80 {
		return nil, fmt.Errorf("title must contain 4 to 80 characters")
	}
	if description == "" || len([]rune(description)) > 20000 {
		return nil, fmt.Errorf("description is required and must not exceed 20000 characters")
	}
	if err := bounty.ValidateGitHubURL(req.RepoURL, false); err != nil {
		return nil, err
	}
	walletType, err := bounty.NormalizeWalletType(req.RewardWalletType)
	if err != nil {
		return nil, err
	}
	if req.RewardAmount <= 0 {
		return nil, fmt.Errorf("reward_amount must be positive")
	}
	deadline, err := parseDeadline(req.DeadlineAt)
	if err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = "bounty:create:" + platformruntime.GetUUID()
	}
	if len(idempotencyKey) > 255 {
		return nil, fmt.Errorf("idempotency key is too long")
	}
	taskID := platformruntime.GetUUID()
	now := time.Now()
	task := &bountyschema.BountyTask{
		TaskID:           taskID,
		PublisherUserID:  userID,
		Title:            title,
		Description:      description,
		RepoURL:          strings.TrimSpace(req.RepoURL),
		TaskType:         normalizeTaskType(req.TaskType),
		TagsText:         tagsText(req.Tags),
		RewardWalletType: walletType,
		RewardAmount:     req.RewardAmount,
		IdempotencyKey:   idempotencyKey,
		Status:           bounty.TaskStatusDraft,
		DeadlineAt:       deadline,
		RevisionLimit:    2,
	}

	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var existing bountyschema.BountyTask
		if err := tx.Where("idempotency_key = ?", idempotencyKey).First(&existing).Error; err == nil {
			taskTypeMatches := strings.TrimSpace(req.TaskType) == "" || existing.TaskType == normalizeTaskType(req.TaskType)
			tagsMatch := len(req.Tags) == 0 || existing.TagsText == tagsText(req.Tags)
			if existing.PublisherUserID != userID || existing.Title != title || existing.Description != description || existing.RepoURL != strings.TrimSpace(req.RepoURL) || !taskTypeMatches || !tagsMatch || existing.RewardAmount != req.RewardAmount || existing.RewardWalletType != walletType || !existing.DeadlineAt.Equal(deadline) {
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
		account, err := ensureUserAccountTx(tx, userID, walletType)
		if err != nil {
			return err
		}
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		expiresAt := deadline.Add(ReviewWindow)
		reservation, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
			AccountID:      account.AccountID,
			RequestID:      taskID,
			WorkflowID:     "bounty:" + taskID,
			ReservedAmount: req.RewardAmount,
			IdempotencyKey: "bounty:" + taskID + ":reserve",
			ExpiresAt:      &expiresAt,
		})
		if err != nil {
			return err
		}
		task.ReservationID = reservation.ReservationID
		task.PublishedAt = now
		if err := transitionTaskTx(tx, task, bounty.TaskStatusPublished, map[string]any{
			"reservation_id": task.ReservationID,
			"published_at":   task.PublishedAt,
		}); err != nil {
			return err
		}
		publishedEvent, err := recordEventTx(tx, taskID, bounty.EventTaskPublished, userID, constant.RoleCommonUser, map[string]any{
			"title":              title,
			"reward_amount":      req.RewardAmount,
			"reward_wallet_type": walletType,
			"deadline_at":        deadline,
		})
		if err != nil {
			return err
		}
		if _, err := recordEventTx(tx, taskID, bounty.EventRewardHeld, userID, constant.RoleCommonUser, map[string]any{
			"reservation_id": reservation.ReservationID,
			"amount":         req.RewardAmount,
			"wallet_type":    walletType,
		}); err != nil {
			return err
		}
		return createNotificationTx(tx, userID, taskID, publishedEvent.EventID, "task_published", "任务已发布", "悬赏额度已冻结，等待执行者申请。")
	}); err != nil {
		return nil, err
	}
	return GetTaskDetail(task.TaskID, userID, constant.RoleCommonUser)
}

func normalizeTaskType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case bounty.TaskTypeUI:
		return bounty.TaskTypeUI
	case bounty.TaskTypeFrontend:
		return bounty.TaskTypeFrontend
	case bounty.TaskTypeBackend:
		return bounty.TaskTypeBackend
	default:
		return bounty.TaskTypeGeneral
	}
}

func ListTasks(userID int64, req ListTasksRequest, role int) (*TaskListResponse, error) {
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	query := platformdb.DB.Model(&bountyschema.BountyTask{})
	if role < constant.RoleAdminUser && req.Scope != "mine_published" && req.Scope != "mine_assigned" && req.Scope != "mine_disputes" {
		query = query.Where("status NOT IN ?", []string{bounty.TaskStatusDraft, bounty.TaskStatusSuspended})
	}
	if req.Scope == "mine_published" {
		if userID <= 0 {
			return nil, ErrForbidden
		}
		query = query.Where("publisher_user_id = ?", userID)
	} else if req.Scope == "mine_assigned" {
		if userID <= 0 {
			return nil, ErrForbidden
		}
		query = query.Where("assignee_user_id = ?", userID)
	} else if req.Scope == "mine_disputes" {
		if userID <= 0 {
			return nil, ErrForbidden
		}
		participantTasks := platformdb.DB.Model(&bountyschema.BountyTask{}).Select("task_id").Where("publisher_user_id = ? OR assignee_user_id = ?", userID, userID)
		query = query.Where("task_id IN (?)", platformdb.DB.Model(&bountyschema.BountyDispute{}).Select("task_id").Where("opened_by_user_id = ? OR task_id IN (?)", userID, participantTasks))
	}
	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		pattern := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(title) LIKE ? OR LOWER(description) LIKE ? OR LOWER(tags_text) LIKE ?", pattern, pattern, pattern)
	}
	if strings.TrimSpace(req.Tag) != "" {
		query = query.Where("LOWER(tags_text) LIKE ?", "%"+strings.ToLower(strings.TrimSpace(req.Tag))+"%")
	}
	if strings.TrimSpace(req.WalletType) != "" {
		walletType, err := bounty.NormalizeWalletType(req.WalletType)
		if err != nil {
			return nil, err
		}
		query = query.Where("reward_wallet_type = ?", walletType)
	}
	if req.MinReward > 0 {
		query = query.Where("reward_amount >= ?", req.MinReward)
	}
	if req.MaxReward > 0 {
		query = query.Where("reward_amount <= ?", req.MaxReward)
	}
	query = applyTaskStatusFilter(query, req.Status)
	switch req.Sort {
	case "reward_desc":
		query = query.Order("reward_amount DESC, created_at DESC")
	case "deadline_asc":
		query = query.Order("deadline_at ASC, created_at DESC")
	default:
		query = query.Order("created_at DESC")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var tasks []bountyschema.BountyTask
	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error; err != nil {
		return nil, err
	}
	views, err := buildTaskViews(tasks, userID, role)
	if err != nil {
		return nil, err
	}
	return &TaskListResponse{Items: views, Total: total, Page: page, PageSize: pageSize}, nil
}

func applyTaskStatusFilter(query *gorm.DB, status string) *gorm.DB {
	switch strings.TrimSpace(status) {
	case "available":
		return query.Where("status IN ?", []string{bounty.TaskStatusPublished, bounty.TaskStatusSelecting})
	case "ending_soon":
		return query.Where("deadline_at > ? AND deadline_at <= ? AND status NOT IN ?", time.Now(), time.Now().Add(72*time.Hour), []string{bounty.TaskStatusCompleted, bounty.TaskStatusCancelled, bounty.TaskStatusExpired, bounty.TaskStatusResolved})
	case "active":
		return query.Where("status NOT IN ?", []string{bounty.TaskStatusDraft, bounty.TaskStatusCompleted, bounty.TaskStatusCancelled, bounty.TaskStatusExpired, bounty.TaskStatusResolved})
	case "":
		return query
	default:
		return query.Where("status = ?", status)
	}
	return query
}

func GetTaskDetail(taskID string, userID int64, role int) (*TaskDetailView, error) {
	var task bountyschema.BountyTask
	if err := platformdb.DB.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	participant := task.PublisherUserID == userID || (task.AssigneeUserID != nil && *task.AssigneeUserID == userID)
	if role < constant.RoleAdminUser && !participant && (task.Status == bounty.TaskStatusDraft || task.Status == bounty.TaskStatusSuspended) {
		return nil, ErrTaskNotFound
	}
	return buildTaskDetail(task, userID, role)
}

func GetTimeline(taskID string, userID int64, role int) ([]EventView, error) {
	var task bountyschema.BountyTask
	if err := platformdb.DB.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	participant := task.PublisherUserID == userID || (task.AssigneeUserID != nil && *task.AssigneeUserID == userID)
	if !participant && role < constant.RoleAdminUser {
		if task.Status == bounty.TaskStatusDraft || task.Status == bounty.TaskStatusSuspended {
			return nil, ErrTaskNotFound
		}
		return buildPublicTimeline(taskID)
	}
	var events []bountyschema.BountyEvent
	if err := platformdb.DB.Where("task_id = ?", taskID).Order("created_at ASC").Find(&events).Error; err != nil {
		return nil, err
	}
	return buildEventViews(events)
}

func ListBalances(userID int64) ([]BalanceView, error) {
	if userID <= 0 {
		return nil, ErrForbidden
	}
	balances := make([]BalanceView, 0, 2)
	for _, walletType := range []string{bounty.WalletTypeDefault, bounty.WalletTypeClaude} {
		balance, err := loadBalanceTx(platformdb.DB, userID, walletType)
		if err != nil {
			return nil, err
		}
		balances = append(balances, balance)
	}
	return balances, nil
}

func buildTaskViews(tasks []bountyschema.BountyTask, userID int64, role int) ([]TaskView, error) {
	ids := make([]int64, 0, len(tasks)*2)
	for index := range tasks {
		ids = append(ids, tasks[index].PublisherUserID)
		if tasks[index].AssigneeUserID != nil {
			ids = append(ids, *tasks[index].AssigneeUserID)
		}
	}
	users, err := loadUserViewsTx(platformdb.DB, ids)
	if err != nil {
		return nil, err
	}
	applicationMap, err := loadMyApplications(tasks, userID)
	if err != nil {
		return nil, err
	}
	views := make([]TaskView, 0, len(tasks))
	for index := range tasks {
		views = append(views, taskView(tasks[index], userID, role, users, applicationMap[tasks[index].TaskID]))
	}
	return views, nil
}

func loadMyApplications(tasks []bountyschema.BountyTask, userID int64) (map[string]bountyschema.BountyApplication, error) {
	result := make(map[string]bountyschema.BountyApplication)
	if userID <= 0 || len(tasks) == 0 {
		return result, nil
	}
	ids := make([]string, 0, len(tasks))
	for index := range tasks {
		ids = append(ids, tasks[index].TaskID)
	}
	var applications []bountyschema.BountyApplication
	if err := platformdb.DB.Where("applicant_user_id = ? AND task_id IN ?", userID, ids).Find(&applications).Error; err != nil {
		return nil, err
	}
	for index := range applications {
		result[applications[index].TaskID] = applications[index]
	}
	return result, nil
}

func taskView(task bountyschema.BountyTask, userID int64, role int, users map[int64]UserView, application bountyschema.BountyApplication) TaskView {
	view := TaskView{
		TaskID:                   task.TaskID,
		Publisher:                users[task.PublisherUserID],
		Title:                    task.Title,
		Description:              task.Description,
		RepoURL:                  task.RepoURL,
		TaskType:                 task.TaskType,
		Tags:                     parseTags(task.TagsText),
		RewardWalletType:         task.RewardWalletType,
		RewardAmount:             task.RewardAmount,
		Status:                   task.Status,
		DeadlineAt:               task.DeadlineAt,
		ReviewDeadlineAt:         task.ReviewDeadlineAt,
		RevisionLimit:            task.RevisionLimit,
		RevisionCount:            task.RevisionCount,
		CanManage:                task.PublisherUserID == userID,
		CanStart:                 task.AssigneeUserID != nil && *task.AssigneeUserID == userID && task.Status == bounty.TaskStatusAssigned,
		CanSubmit:                task.AssigneeUserID != nil && *task.AssigneeUserID == userID && (task.Status == bounty.TaskStatusInProgress || task.Status == bounty.TaskStatusChangesRequested || task.Status == bounty.TaskStatusPublisherReplied),
		CanHandleMaterialTimeout: task.AssigneeUserID != nil && *task.AssigneeUserID == userID && task.Status == bounty.TaskStatusWaitingForPublisher,
		CanDispute:               canOpenDispute(task, userID),
		CanReport:                userID > 0 && task.PublisherUserID != userID && (task.AssigneeUserID == nil || *task.AssigneeUserID != userID) && role < constant.RoleAdminUser,
		CreatedAt:                task.CreatedAt,
		UpdatedAt:                task.UpdatedAt,
	}
	if task.PublisherUserID == userID || role >= constant.RoleAdminUser {
		view.ReservationID = task.ReservationID
	}
	if task.AssigneeUserID != nil {
		assignee := users[*task.AssigneeUserID]
		view.Executor = &assignee
	}
	view.CanApply = userID > 0 && task.PublisherUserID != userID && (task.Status == bounty.TaskStatusPublished || task.Status == bounty.TaskStatusSelecting) && application.ApplicationID == ""
	return view
}

func canOpenDispute(task bountyschema.BountyTask, userID int64) bool {
	if userID <= 0 || (task.PublisherUserID != userID && (task.AssigneeUserID == nil || *task.AssigneeUserID != userID)) {
		return false
	}
	switch task.Status {
	case bounty.TaskStatusInProgress, bounty.TaskStatusWaitingForPublisher, bounty.TaskStatusPublisherReplied, bounty.TaskStatusSubmitted, bounty.TaskStatusReviewing, bounty.TaskStatusChangesRequested:
		return true
	default:
		return false
	}
}
