package schema

import (
	"time"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

type BountyApplication struct {
	ApplicationID       string     `json:"application_id" gorm:"column:application_id;primaryKey;size:64"`
	TaskID              string     `json:"task_id" gorm:"column:task_id;size:64;index:idx_bounty_applications_task_status"`
	ApplicantUserID     int64      `json:"applicant_user_id" gorm:"column:applicant_user_id;index:idx_bounty_applications_applicant"`
	Message             string     `json:"message" gorm:"column:message;type:text"`
	EstimatedDeliveryAt *time.Time `json:"estimated_delivery_at,omitempty" gorm:"column:estimated_delivery_at"`
	Status              string     `json:"status" gorm:"column:status;size:24;index:idx_bounty_applications_task_status"`
	CreatedAt           time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt           time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (BountyApplication) TableName() string { return "bounty_applications" }

func (item *BountyApplication) BeforeCreate(_ *gorm.DB) error {
	if item.ApplicationID == "" {
		item.ApplicationID = platformruntime.GetUUID()
	}
	if item.Status == "" {
		item.Status = "pending"
	}
	return nil
}

type BountyMaterialRequest struct {
	RequestID         string     `json:"request_id" gorm:"column:request_id;primaryKey;size:64"`
	TaskID            string     `json:"task_id" gorm:"column:task_id;size:64;index"`
	RequesterUserID   int64      `json:"requester_user_id" gorm:"column:requester_user_id;index"`
	Content           string     `json:"content" gorm:"column:content;type:text"`
	IsBlocking        bool       `json:"is_blocking" gorm:"column:is_blocking;index"`
	Status            string     `json:"status" gorm:"column:status;size:32;index"`
	CreatedAt         time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty" gorm:"column:resolved_at"`
	TimeoutAt         *time.Time `json:"timeout_at,omitempty" gorm:"column:timeout_at;index"`
	TimeoutNotifiedAt *time.Time `json:"-" gorm:"column:timeout_notified_at"`
	TimeoutAction     string     `json:"timeout_action,omitempty" gorm:"column:timeout_action;size:32"`
}

func (BountyMaterialRequest) TableName() string { return "bounty_material_requests" }

func (item *BountyMaterialRequest) BeforeCreate(_ *gorm.DB) error {
	if item.RequestID == "" {
		item.RequestID = platformruntime.GetUUID()
	}
	if item.Status == "" {
		item.Status = "open"
	}
	return nil
}

type BountyMaterialReply struct {
	ReplyID      string    `json:"reply_id" gorm:"column:reply_id;primaryKey;size:64"`
	RequestID    string    `json:"request_id" gorm:"column:request_id;size:64;index"`
	AuthorUserID int64     `json:"author_user_id" gorm:"column:author_user_id;index"`
	Content      string    `json:"content" gorm:"column:content;type:text"`
	SourceType   string    `json:"source_type" gorm:"column:source_type;size:24"`
	SourceURL    string    `json:"source_url" gorm:"column:source_url;size:512"`
	CreatedAt    time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (BountyMaterialReply) TableName() string { return "bounty_material_replies" }

func (item *BountyMaterialReply) BeforeCreate(_ *gorm.DB) error {
	if item.ReplyID == "" {
		item.ReplyID = platformruntime.GetUUID()
	}
	return nil
}
