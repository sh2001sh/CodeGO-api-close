package schema

import (
	"time"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

type BountyDispute struct {
	DisputeID        string     `json:"dispute_id" gorm:"column:dispute_id;primaryKey;size:64"`
	TaskID           string     `json:"task_id" gorm:"column:task_id;size:64;index"`
	OpenedByUserID   int64      `json:"opened_by_user_id" gorm:"column:opened_by_user_id;index"`
	Reason           string     `json:"reason" gorm:"column:reason;type:text"`
	DesiredOutcome   string     `json:"desired_outcome" gorm:"column:desired_outcome;type:text"`
	EvidenceText     string     `json:"evidence_text" gorm:"column:evidence_text;type:text"`
	SnapshotText     string     `json:"-" gorm:"column:snapshot_text;type:text"`
	AIAnalysisText   string     `json:"ai_analysis_text,omitempty" gorm:"column:ai_analysis_text;type:text"`
	AIModel          string     `json:"ai_model,omitempty" gorm:"column:ai_model;size:96"`
	AIStatus         string     `json:"ai_status" gorm:"column:ai_status;size:24"`
	Status           string     `json:"status" gorm:"column:status;size:24;index"`
	ResolutionType   string     `json:"resolution_type,omitempty" gorm:"column:resolution_type;size:32"`
	ResolutionAmount int64      `json:"resolution_amount,omitempty" gorm:"column:resolution_amount"`
	ResolutionNote   string     `json:"resolution_note,omitempty" gorm:"column:resolution_note;type:text"`
	ResolvedByUserID *int64     `json:"resolved_by_user_id,omitempty" gorm:"column:resolved_by_user_id;index"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty" gorm:"column:resolved_at"`
	CreatedAt        time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (BountyDispute) TableName() string { return "bounty_disputes" }

func (item *BountyDispute) BeforeCreate(_ *gorm.DB) error {
	if item.DisputeID == "" {
		item.DisputeID = platformruntime.GetUUID()
	}
	if item.Status == "" {
		item.Status = "open"
	}
	if item.AIStatus == "" {
		item.AIStatus = "completed"
	}
	return nil
}
