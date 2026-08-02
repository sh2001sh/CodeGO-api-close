package schema

import "time"

const (
	ResourceStatusPending  = "pending"
	ResourceStatusApproved = "approved"
	ResourceStatusRejected = "rejected"
)

// Resource is a GitHub-hosted community contribution submitted for publication.
type Resource struct {
	ID                 int64      `json:"id" gorm:"primaryKey;autoIncrement"`
	Title              string     `json:"title" gorm:"type:varchar(80);not null"`
	Description        string     `json:"description" gorm:"type:varchar(500);not null"`
	Category           string     `json:"category" gorm:"type:varchar(24);not null;index"`
	GitHubURL          string     `json:"github_url" gorm:"type:varchar(500);not null;index"`
	RepositoryURL      string     `json:"repository_url" gorm:"type:varchar(300);not null;index"`
	AcknowledgementURL string     `json:"acknowledgement_url,omitempty" gorm:"type:varchar(500)"`
	SubmittedBy        int        `json:"submitted_by" gorm:"not null;index"`
	SubmitterName      string     `json:"submitter_name" gorm:"type:varchar(64);not null"`
	Status             string     `json:"status" gorm:"type:varchar(24);not null;index"`
	ReviewNote         string     `json:"review_note,omitempty" gorm:"type:varchar(300)"`
	ReviewedBy         *int       `json:"reviewed_by,omitempty" gorm:"index"`
	PublishedAt        *time.Time `json:"published_at,omitempty" gorm:"index"`
	RewardQuota        int64      `json:"reward_quota" gorm:"type:bigint;not null;default:0"`
	RewardedBy         *int       `json:"rewarded_by,omitempty" gorm:"index"`
	RewardedAt         *time.Time `json:"rewarded_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt          time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

func (Resource) TableName() string { return "community_resources" }
