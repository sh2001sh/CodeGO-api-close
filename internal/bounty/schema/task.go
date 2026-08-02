package schema

import (
	"time"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

type BountyTask struct {
	TaskID                    string     `json:"task_id" gorm:"column:task_id;primaryKey;size:64"`
	PublisherUserID           int64      `json:"publisher_user_id" gorm:"column:publisher_user_id;index"`
	Title                     string     `json:"title" gorm:"column:title;size:160"`
	Description               string     `json:"description" gorm:"column:description;type:text"`
	RepoURL                   string     `json:"repo_url" gorm:"column:repo_url;size:512"`
	TaskType                  string     `json:"task_type" gorm:"column:task_type;size:32;index"`
	TagsText                  string     `json:"tags_text" gorm:"column:tags_text;type:text"`
	RewardWalletType          string     `json:"reward_wallet_type" gorm:"column:reward_wallet_type;size:32;index"`
	RewardAmount              int64      `json:"reward_amount" gorm:"column:reward_amount"`
	ReservationID             string     `json:"reservation_id" gorm:"column:reservation_id;size:64;index"`
	IdempotencyKey            string     `json:"-" gorm:"column:idempotency_key;size:255;uniqueIndex:uq_bounty_tasks_idempotency"`
	Status                    string     `json:"status" gorm:"column:status;size:40;index"`
	DeadlineAt                time.Time  `json:"deadline_at" gorm:"column:deadline_at;index"`
	ReviewDeadlineAt          *time.Time `json:"review_deadline_at,omitempty" gorm:"column:review_deadline_at;index"`
	ReviewDeadlineNotifiedAt  *time.Time `json:"-" gorm:"column:review_deadline_notified_at"`
	AssigneeUserID            *int64     `json:"assignee_user_id,omitempty" gorm:"column:assignee_user_id;index"`
	RevisionLimit             int        `json:"revision_limit" gorm:"column:revision_limit;default:2"`
	RevisionCount             int        `json:"revision_count" gorm:"column:revision_count;default:0"`
	PausedAt                  *time.Time `json:"paused_at,omitempty" gorm:"column:paused_at"`
	SuspendedAt               *time.Time `json:"-" gorm:"column:suspended_at"`
	SuspendedPreviousPausedAt *time.Time `json:"-" gorm:"column:suspended_previous_paused_at"`
	SuspendedFromStatus       string     `json:"-" gorm:"column:suspended_from_status;size:40"`
	PublishedAt               time.Time  `json:"published_at" gorm:"column:published_at;index"`
	CreatedAt                 time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt                 time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (BountyTask) TableName() string { return "bounty_tasks" }

func (task *BountyTask) BeforeCreate(_ *gorm.DB) error {
	if task.TaskID == "" {
		task.TaskID = platformruntime.GetUUID()
	}
	if task.Status == "" {
		task.Status = "draft"
	}
	if task.RevisionLimit <= 0 {
		task.RevisionLimit = 2
	}
	return nil
}
