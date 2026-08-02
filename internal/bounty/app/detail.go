package app

import (
	"encoding/json"
	"errors"

	"github.com/sh2001sh/new-api/constant"
	bounty "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"gorm.io/gorm"
)

func buildTaskDetail(task bountyschema.BountyTask, userID int64, role int) (*TaskDetailView, error) {
	participant := task.PublisherUserID == userID || (task.AssigneeUserID != nil && *task.AssigneeUserID == userID) || role >= constant.RoleAdminUser
	ids := []int64{task.PublisherUserID}
	if task.AssigneeUserID != nil {
		ids = append(ids, *task.AssigneeUserID)
	}
	users, err := loadUserViewsTx(platformdb.DB, ids)
	if err != nil {
		return nil, err
	}
	detail := &TaskDetailView{
		Task:             taskView(task, userID, role, users, bountyschema.BountyApplication{}),
		Applications:     []ApplicationView{},
		MaterialRequests: []MaterialRequestView{},
		Submissions:      []SubmissionView{},
		Disputes:         []DisputeView{},
		Timeline:         []EventView{},
	}
	if userID > 0 {
		var myApplication bountyschema.BountyApplication
		if err := platformdb.DB.Where("task_id = ? AND applicant_user_id = ?", task.TaskID, userID).First(&myApplication).Error; err == nil {
			applicationViews, err := buildApplicationViews([]bountyschema.BountyApplication{myApplication})
			if err != nil {
				return nil, err
			}
			if len(applicationViews) == 1 {
				detail.MyApplication = &applicationViews[0]
				detail.Task.CanApply = false
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if !participant {
		detail.Timeline, err = buildPublicTimeline(task.TaskID)
		if err != nil {
			return nil, err
		}
		return detail, nil
	}
	var applications []bountyschema.BountyApplication
	if err := platformdb.DB.Where("task_id = ?", task.TaskID).Order("created_at ASC").Find(&applications).Error; err != nil {
		return nil, err
	}
	detail.Applications, err = buildApplicationViews(applications)
	if err != nil {
		return nil, err
	}
	var requests []bountyschema.BountyMaterialRequest
	if err := platformdb.DB.Where("task_id = ?", task.TaskID).Order("created_at ASC").Find(&requests).Error; err != nil {
		return nil, err
	}
	detail.MaterialRequests, err = buildMaterialRequestViews(requests)
	if err != nil {
		return nil, err
	}
	var submissions []bountyschema.BountySubmission
	if err := platformdb.DB.Where("task_id = ?", task.TaskID).Order("version DESC").Find(&submissions).Error; err != nil {
		return nil, err
	}
	detail.Submissions, err = buildSubmissionViews(submissions)
	if err != nil {
		return nil, err
	}
	var disputes []bountyschema.BountyDispute
	if err := platformdb.DB.Where("task_id = ?", task.TaskID).Order("created_at DESC").Find(&disputes).Error; err != nil {
		return nil, err
	}
	detail.Disputes, err = buildDisputeViews(disputes)
	if err != nil {
		return nil, err
	}
	var events []bountyschema.BountyEvent
	if err := platformdb.DB.Where("task_id = ?", task.TaskID).Order("created_at ASC").Find(&events).Error; err != nil {
		return nil, err
	}
	detail.Timeline, err = buildEventViews(events)
	if err != nil {
		return nil, err
	}
	return detail, nil
}

func buildPublicTimeline(taskID string) ([]EventView, error) {
	publicEventTypes := []string{
		bounty.EventTaskPublished,
		bounty.EventRewardHeld,
		bounty.EventApplicationSubmitted,
		bounty.EventApplicationAccepted,
		bounty.EventTaskStarted,
		bounty.EventSubmissionCreated,
		bounty.EventReviewStarted,
		bounty.EventChangesRequested,
		bounty.EventReviewApproved,
		bounty.EventTaskCompleted,
		bounty.EventRewardPaid,
		bounty.EventRewardReleased,
		bounty.EventTaskCancelled,
		bounty.EventTaskExpired,
		bounty.EventDisputeOpened,
		bounty.EventDisputeResolved,
	}
	var events []bountyschema.BountyEvent
	if err := platformdb.DB.Where("task_id = ? AND event_type IN ?", taskID, publicEventTypes).Order("created_at ASC").Find(&events).Error; err != nil {
		return nil, err
	}
	views, err := buildEventViews(events)
	if err != nil {
		return nil, err
	}
	for index := range views {
		views[index].Payload = nil
		if views[index].EventType == bounty.EventApplicationSubmitted {
			views[index].Actor = nil
		}
	}
	return views, nil
}

func buildApplicationViews(items []bountyschema.BountyApplication) ([]ApplicationView, error) {
	ids := make([]int64, 0, len(items))
	for index := range items {
		ids = append(ids, items[index].ApplicantUserID)
	}
	users, err := loadUserViewsTx(platformdb.DB, ids)
	if err != nil {
		return nil, err
	}
	views := make([]ApplicationView, 0, len(items))
	for index := range items {
		item := items[index]
		views = append(views, ApplicationView{
			ApplicationID:       item.ApplicationID,
			TaskID:              item.TaskID,
			Applicant:           users[item.ApplicantUserID],
			Message:             item.Message,
			EstimatedDeliveryAt: item.EstimatedDeliveryAt,
			Status:              item.Status,
			CreatedAt:           item.CreatedAt,
			UpdatedAt:           item.UpdatedAt,
		})
	}
	return views, nil
}

func buildMaterialRequestViews(items []bountyschema.BountyMaterialRequest) ([]MaterialRequestView, error) {
	ids := make([]int64, 0, len(items))
	requestIDs := make([]string, 0, len(items))
	for index := range items {
		ids = append(ids, items[index].RequesterUserID)
		requestIDs = append(requestIDs, items[index].RequestID)
	}
	users, err := loadUserViewsTx(platformdb.DB, ids)
	if err != nil {
		return nil, err
	}
	var replies []bountyschema.BountyMaterialReply
	if len(requestIDs) > 0 {
		if err := platformdb.DB.Where("request_id IN ?", requestIDs).Order("created_at ASC").Find(&replies).Error; err != nil {
			return nil, err
		}
	}
	replyAuthors := make([]int64, 0, len(replies))
	for index := range replies {
		replyAuthors = append(replyAuthors, replies[index].AuthorUserID)
	}
	authorViews, err := loadUserViewsTx(platformdb.DB, replyAuthors)
	if err != nil {
		return nil, err
	}
	repliesByRequest := make(map[string][]MaterialReplyView)
	for index := range replies {
		item := replies[index]
		repliesByRequest[item.RequestID] = append(repliesByRequest[item.RequestID], MaterialReplyView{
			ReplyID:    item.ReplyID,
			Author:     authorViews[item.AuthorUserID],
			Content:    item.Content,
			SourceType: item.SourceType,
			SourceURL:  item.SourceURL,
			CreatedAt:  item.CreatedAt,
		})
	}
	views := make([]MaterialRequestView, 0, len(items))
	for index := range items {
		item := items[index]
		views = append(views, MaterialRequestView{
			RequestID:     item.RequestID,
			Requester:     users[item.RequesterUserID],
			Content:       item.Content,
			IsBlocking:    item.IsBlocking,
			Status:        item.Status,
			CreatedAt:     item.CreatedAt,
			ResolvedAt:    item.ResolvedAt,
			TimeoutAt:     item.TimeoutAt,
			TimeoutAction: item.TimeoutAction,
			Replies:       repliesByRequest[item.RequestID],
		})
	}
	return views, nil
}

func buildSubmissionViews(items []bountyschema.BountySubmission) ([]SubmissionView, error) {
	ids := make([]int64, 0, len(items))
	for index := range items {
		ids = append(ids, items[index].ExecutorUserID)
	}
	users, err := loadUserViewsTx(platformdb.DB, ids)
	if err != nil {
		return nil, err
	}
	views := make([]SubmissionView, 0, len(items))
	for index := range items {
		item := items[index]
		views = append(views, SubmissionView{
			SubmissionID:      item.SubmissionID,
			TaskID:            item.TaskID,
			Executor:          users[item.ExecutorUserID],
			Version:           item.Version,
			RepoURL:           item.RepoURL,
			IssueURL:          item.IssueURL,
			PullRequestURL:    item.PullRequestURL,
			CommitSHA:         item.CommitSHA,
			CompletionSummary: item.CompletionSummary,
			EffectImages:      parseEffectImages(item.EffectImagesText),
			TestReport:        item.TestReport,
			KnownLimitations:  item.KnownLimitations,
			Status:            item.Status,
			CreatedAt:         item.CreatedAt,
		})
	}
	return views, nil
}

func buildDisputeViews(items []bountyschema.BountyDispute) ([]DisputeView, error) {
	ids := make([]int64, 0, len(items)*2)
	for index := range items {
		ids = append(ids, items[index].OpenedByUserID)
		if items[index].ResolvedByUserID != nil {
			ids = append(ids, *items[index].ResolvedByUserID)
		}
	}
	users, err := loadUserViewsTx(platformdb.DB, ids)
	if err != nil {
		return nil, err
	}
	views := make([]DisputeView, 0, len(items))
	for index := range items {
		item := items[index]
		var resolvedBy *UserView
		if item.ResolvedByUserID != nil {
			resolved := users[*item.ResolvedByUserID]
			resolvedBy = &resolved
		}
		var analysis json.RawMessage
		if item.AIAnalysisText != "" {
			analysis = json.RawMessage(item.AIAnalysisText)
		}
		views = append(views, DisputeView{
			DisputeID:        item.DisputeID,
			TaskID:           item.TaskID,
			OpenedBy:         users[item.OpenedByUserID],
			Reason:           item.Reason,
			DesiredOutcome:   item.DesiredOutcome,
			EvidenceText:     item.EvidenceText,
			AIAnalysis:       analysis,
			AIModel:          item.AIModel,
			AIStatus:         item.AIStatus,
			Status:           item.Status,
			ResolutionType:   item.ResolutionType,
			ResolutionAmount: item.ResolutionAmount,
			ResolutionNote:   item.ResolutionNote,
			ResolvedBy:       resolvedBy,
			ResolvedAt:       item.ResolvedAt,
			CreatedAt:        item.CreatedAt,
		})
	}
	return views, nil
}

func buildEventViews(items []bountyschema.BountyEvent) ([]EventView, error) {
	ids := make([]int64, 0, len(items))
	for index := range items {
		ids = append(ids, items[index].ActorUserID)
	}
	users, err := loadUserViewsTx(platformdb.DB, ids)
	if err != nil {
		return nil, err
	}
	views := make([]EventView, 0, len(items))
	for index := range items {
		item := items[index]
		var actor *UserView
		if item.ActorUserID > 0 {
			value := users[item.ActorUserID]
			actor = &value
		}
		var payload json.RawMessage
		if item.PayloadText != "" {
			payload = json.RawMessage(item.PayloadText)
		}
		views = append(views, EventView{
			EventID:   item.EventID,
			TaskID:    item.TaskID,
			EventType: item.EventType,
			Actor:     actor,
			ActorRole: item.ActorRole,
			Payload:   payload,
			CreatedAt: item.CreatedAt,
		})
	}
	return views, nil
}

func decodeEventPayload(payload string) map[string]any {
	result := make(map[string]any)
	if payload == "" {
		return result
	}
	if err := platformencoding.Unmarshal([]byte(payload), &result); err != nil {
		return map[string]any{"raw": payload}
	}
	return result
}
