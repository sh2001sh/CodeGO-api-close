package schema

import "time"

// MultiplierNotice is saved in the same transaction as the price change.
type MultiplierNotice struct {
	ID                 uint64     `json:"id" gorm:"primaryKey"`
	UserID             int        `json:"-" gorm:"not null;index"`
	ChannelID          string     `json:"channel_id" gorm:"size:64;not null"`
	ChannelName        string     `json:"channel_name" gorm:"size:128;not null"`
	PreviousMultiplier float64    `json:"previous_multiplier"`
	Multiplier         float64    `json:"multiplier"`
	Cleared            bool       `json:"cleared"`
	Source             string     `json:"source" gorm:"size:16"`
	ReadAt             *time.Time `json:"read_at"`
	CreatedAt          time.Time  `json:"created_at"`
}

func (MultiplierNotice) TableName() string { return tableName("multiplier_notices") }
