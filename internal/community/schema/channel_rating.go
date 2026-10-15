package schema

import "time"

// ChannelRating stores one CodeGo user's current star rating for a public channel.
type ChannelRating struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelID string    `json:"channel_id" gorm:"column:channel_id;size:64;not null;uniqueIndex:uq_community_channel_rating,priority:1;index"`
	UserID    int       `json:"-" gorm:"column:user_id;not null;uniqueIndex:uq_community_channel_rating,priority:2;index"`
	Stars     int       `json:"stars" gorm:"column:stars;not null;check:chk_community_channel_rating_stars,stars >= 1 AND stars <= 5"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoCreateTime;autoUpdateTime"`
}

func (ChannelRating) TableName() string { return "community_channel_ratings" }
