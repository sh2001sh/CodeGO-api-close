package schema

import (
	"time"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

type BountySubmission struct {
	SubmissionID      string    `json:"submission_id" gorm:"column:submission_id;primaryKey;size:64"`
	TaskID            string    `json:"task_id" gorm:"column:task_id;size:64;index:idx_bounty_submissions_task_version;uniqueIndex:uq_bounty_submissions_task_version"`
	ExecutorUserID    int64     `json:"executor_user_id" gorm:"column:executor_user_id;index"`
	Version           int       `json:"version" gorm:"column:version;uniqueIndex:uq_bounty_submissions_task_version"`
	RepoURL           string    `json:"repo_url" gorm:"column:repo_url;size:512"`
	IssueURL          string    `json:"issue_url" gorm:"column:issue_url;size:512"`
	PullRequestURL    string    `json:"pull_request_url" gorm:"column:pull_request_url;size:512"`
	CommitSHA         string    `json:"commit_sha" gorm:"column:commit_sha;size:64"`
	CompletionSummary string    `json:"completion_summary" gorm:"column:completion_summary;type:text"`
	EffectImagesText  string    `json:"effect_images_text" gorm:"column:effect_images_text;type:text"`
	TestReport        string    `json:"test_report" gorm:"column:test_report;type:text"`
	KnownLimitations  string    `json:"known_limitations" gorm:"column:known_limitations;type:text"`
	Status            string    `json:"status" gorm:"column:status;size:24;index"`
	CreatedAt         time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (BountySubmission) TableName() string { return "bounty_submissions" }

func (item *BountySubmission) BeforeCreate(_ *gorm.DB) error {
	if item.SubmissionID == "" {
		item.SubmissionID = platformruntime.GetUUID()
	}
	if item.Status == "" {
		item.Status = "submitted"
	}
	return nil
}
