package schema

import (
	"time"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

type BountyEvent struct {
	EventID     string    `json:"event_id" gorm:"column:event_id;primaryKey;size:64"`
	TaskID      string    `json:"task_id" gorm:"column:task_id;size:64;index"`
	EventType   string    `json:"event_type" gorm:"column:event_type;size:64;index"`
	ActorUserID int64     `json:"actor_user_id" gorm:"column:actor_user_id;index"`
	ActorRole   string    `json:"actor_role" gorm:"column:actor_role;size:24"`
	PayloadText string    `json:"payload_text" gorm:"column:payload_text;type:text"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime;index"`
}

func (BountyEvent) TableName() string { return "bounty_events" }

func (item *BountyEvent) BeforeCreate(_ *gorm.DB) error {
	if item.EventID == "" {
		item.EventID = platformruntime.GetUUID()
	}
	return nil
}

type BountyNotification struct {
	NotificationID string     `json:"notification_id" gorm:"column:notification_id;primaryKey;size:64"`
	UserID         int64      `json:"user_id" gorm:"column:user_id;index"`
	TaskID         string     `json:"task_id" gorm:"column:task_id;size:64;index"`
	EventID        string     `json:"event_id" gorm:"column:event_id;size:64;index"`
	Type           string     `json:"type" gorm:"column:type;size:48;index"`
	Title          string     `json:"title" gorm:"column:title;size:160"`
	Content        string     `json:"content" gorm:"column:content;type:text"`
	ReadAt         *time.Time `json:"read_at,omitempty" gorm:"column:read_at;index"`
	CreatedAt      time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime;index"`
}

func (BountyNotification) TableName() string { return "bounty_notifications" }

func (item *BountyNotification) BeforeCreate(_ *gorm.DB) error {
	if item.NotificationID == "" {
		item.NotificationID = platformruntime.GetUUID()
	}
	return nil
}
