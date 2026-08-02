package schema

import (
	"time"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

type BountyReport struct {
	ReportID         string     `json:"report_id" gorm:"column:report_id;primaryKey;size:64"`
	TaskID           string     `json:"task_id" gorm:"column:task_id;size:64;index"`
	ReporterUserID   int64      `json:"reporter_user_id" gorm:"column:reporter_user_id;index"`
	Reason           string     `json:"reason" gorm:"column:reason;size:64"`
	Details          string     `json:"details" gorm:"column:details;type:text"`
	Status           string     `json:"status" gorm:"column:status;size:24;index"`
	ResolvedByUserID *int64     `json:"resolved_by_user_id,omitempty" gorm:"column:resolved_by_user_id;index"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty" gorm:"column:resolved_at"`
	ResolutionNote   string     `json:"resolution_note,omitempty" gorm:"column:resolution_note;type:text"`
	CreatedAt        time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (BountyReport) TableName() string { return "bounty_reports" }

func (item *BountyReport) BeforeCreate(_ *gorm.DB) error {
	if item.ReportID == "" {
		item.ReportID = platformruntime.GetUUID()
	}
	if item.Status == "" {
		item.Status = "open"
	}
	return nil
}
