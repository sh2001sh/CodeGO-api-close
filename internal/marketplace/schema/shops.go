package schema

import "time"

// A shop belongs to one channel owner. Existing owners have a safe generated
// shop name until they save a public profile; no private user profile is exposed.
type Shop struct {
	OwnerUserID      int    `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Name             string `json:"name" gorm:"size:80"`
	Description      string `json:"description" gorm:"size:500"`
	ModerationStatus string `json:"moderation_status" gorm:"size:24"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (Shop) TableName() string { return tableName("shops") }

type ShopRating struct {
	OwnerUserID int `json:"-" gorm:"primaryKey;autoIncrement:false"`
	UserID      int `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Stars       int `json:"stars" gorm:"not null;check:stars >= 1 AND stars <= 5"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (ShopRating) TableName() string { return tableName("shop_ratings") }

type MarketReport struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ReporterID int       `json:"reporter_id" gorm:"uniqueIndex:uq_market_report,priority:1;not null"`
	TargetType string    `json:"target_type" gorm:"size:16;uniqueIndex:uq_market_report,priority:2;not null"`
	TargetID   string    `json:"target_id" gorm:"size:64;uniqueIndex:uq_market_report,priority:3;not null"`
	Reason     string    `json:"reason" gorm:"size:32;not null"`
	Details    string    `json:"details" gorm:"size:500"`
	Evidence   string    `json:"evidence" gorm:"type:text"`
	Status     string    `json:"status" gorm:"size:24;index;not null"`
	Resolution string    `json:"resolution" gorm:"size:500"`
	ReviewedBy int       `json:"reviewed_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (MarketReport) TableName() string { return tableName("reports") }
