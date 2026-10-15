package store

import (
	"errors"
	"strings"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func CreateResponsesBackgroundJob(job *gatewayschema.ResponsesBackgroundJob) error {
	if job == nil {
		return errors.New("background job is nil")
	}
	return platformdb.DB.Create(job).Error
}

func ClaimResponsesBackgroundJob(jobID string, now time.Time) (bool, error) {
	return ClaimResponsesBackgroundJobWithLease(jobID, now, 30*time.Second)
}

func ClaimResponsesBackgroundJobWithLease(jobID string, now time.Time, leaseDuration time.Duration) (bool, error) {
	staleBefore := now.Add(-leaseDuration)
	result := platformdb.DB.Model(&gatewayschema.ResponsesBackgroundJob{}).
		Where("id = ? AND (status = ? OR (status = ? AND native_background = ? AND upstream_response_id <> '' AND (claimed_at IS NULL OR claimed_at < ?)))",
			jobID, gatewayschema.ResponsesBackgroundQueued, gatewayschema.ResponsesBackgroundRunning, true, staleBefore).
		Updates(map[string]any{
			"status": gatewayschema.ResponsesBackgroundRunning, "claimed_at": now, "started_at": now,
		})
	return result.RowsAffected == 1, result.Error
}

func LoadResponsesBackgroundJob(jobID string) (*gatewayschema.ResponsesBackgroundJob, error) {
	var job gatewayschema.ResponsesBackgroundJob
	if err := platformdb.DB.First(&job, "id = ?", jobID).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func LoadOwnedResponsesBackgroundJob(jobID string, userID, tokenID int) (*gatewayschema.ResponsesBackgroundJob, error) {
	var job gatewayschema.ResponsesBackgroundJob
	if err := platformdb.DB.Where("id = ? AND user_id = ? AND token_id = ?", jobID, userID, tokenID).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func ListRecoverableResponsesBackgroundJobs(limit int, staleBefore time.Time) ([]gatewayschema.ResponsesBackgroundJob, error) {
	if limit <= 0 {
		limit = 20
	}
	var jobs []gatewayschema.ResponsesBackgroundJob
	err := platformdb.DB.Where("status = ? OR (status = ? AND native_background = ? AND upstream_response_id <> '' AND (claimed_at IS NULL OR claimed_at < ?))",
		gatewayschema.ResponsesBackgroundQueued, gatewayschema.ResponsesBackgroundRunning, true, staleBefore).
		Order("created_at asc").Limit(limit).Find(&jobs).Error
	return jobs, err
}

func ListQueuedResponsesBackgroundJobs(limit int) ([]gatewayschema.ResponsesBackgroundJob, error) {
	if limit <= 0 {
		limit = 20
	}
	var jobs []gatewayschema.ResponsesBackgroundJob
	err := platformdb.DB.Where("status = ?", gatewayschema.ResponsesBackgroundQueued).
		Order("created_at asc").Limit(limit).Find(&jobs).Error
	return jobs, err
}

func RenewResponsesBackgroundLease(jobID string, claimedAt time.Time) error {
	return platformdb.DB.Model(&gatewayschema.ResponsesBackgroundJob{}).
		Where("id = ? AND status = ?", jobID, gatewayschema.ResponsesBackgroundRunning).
		Update("claimed_at", claimedAt).Error
}

func UpdateResponsesBackgroundUpstreamCursor(jobID, upstreamID string, sequence int64) error {
	if strings.TrimSpace(upstreamID) == "" {
		return nil
	}
	return platformdb.DB.Model(&gatewayschema.ResponsesBackgroundJob{}).
		Where("id = ? AND status = ? AND upstream_sequence < ?", jobID, gatewayschema.ResponsesBackgroundRunning, sequence).
		Updates(map[string]any{"upstream_response_id": upstreamID, "upstream_sequence": sequence}).Error
}

func AppendResponsesBackgroundEvent(event *gatewayschema.ResponsesBackgroundEvent) error {
	if event == nil {
		return errors.New("background event is nil")
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(event).Error; err != nil {
			return err
		}
		return tx.Model(&gatewayschema.ResponsesBackgroundJob{}).
			Where("id = ? AND last_sequence < ?", event.JobID, event.Sequence).
			Update("last_sequence", event.Sequence).Error
	})
}

func ListResponsesBackgroundEvents(jobID string, after int64, limit int) ([]gatewayschema.ResponsesBackgroundEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	var events []gatewayschema.ResponsesBackgroundEvent
	err := platformdb.DB.Where("job_id = ? AND sequence > ?", jobID, after).
		Order("sequence asc").Limit(limit).Find(&events).Error
	return events, err
}

func UpdateResponsesBackgroundTerminal(jobID, status, finalCiphertext, errorCiphertext, upstreamID string, now time.Time) error {
	return platformdb.DB.Model(&gatewayschema.ResponsesBackgroundJob{}).
		Where("id = ? AND status = ?", jobID, gatewayschema.ResponsesBackgroundRunning).
		Updates(map[string]any{
			"status": status, "final_response_ciphertext": finalCiphertext,
			"error_ciphertext": errorCiphertext, "upstream_response_id": upstreamID,
			"completed_at": now,
		}).Error
}

func RequestResponsesBackgroundCancel(jobID string, userID, tokenID int, now time.Time) (*gatewayschema.ResponsesBackgroundJob, error) {
	var job *gatewayschema.ResponsesBackgroundJob
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var current gatewayschema.ResponsesBackgroundJob
		if err := tx.Where("id = ? AND user_id = ? AND token_id = ?", jobID, userID, tokenID).First(&current).Error; err != nil {
			return err
		}
		switch current.Status {
		case gatewayschema.ResponsesBackgroundQueued:
			if err := tx.Model(&current).Updates(map[string]any{
				"status": gatewayschema.ResponsesBackgroundCanceled, "cancel_requested": true, "completed_at": now,
			}).Error; err != nil {
				return err
			}
			current.Status = gatewayschema.ResponsesBackgroundCanceled
			current.CancelRequested = true
			current.CompletedAt = &now
		case gatewayschema.ResponsesBackgroundRunning:
			if err := tx.Model(&current).Update("cancel_requested", true).Error; err != nil {
				return err
			}
			current.CancelRequested = true
		}
		job = &current
		return nil
	})
	return job, err
}

func ResponsesBackgroundCancelRequested(jobID string) (bool, error) {
	var job gatewayschema.ResponsesBackgroundJob
	if err := platformdb.DB.Select("cancel_requested", "status").First(&job, "id = ?", jobID).Error; err != nil {
		return false, err
	}
	return job.CancelRequested || job.Status == gatewayschema.ResponsesBackgroundCanceled, nil
}
