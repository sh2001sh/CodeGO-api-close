package schema

import "time"

const (
	UpstreamFileMappingReady  = "ready"
	UpstreamFileMappingFailed = "failed"
)

// UpstreamFileMapping binds a local file to one concrete upstream credential.
type UpstreamFileMapping struct {
	ID                 uint      `json:"-" gorm:"primaryKey"`
	LocalFileID        string    `json:"local_file_id" gorm:"size:64;not null;uniqueIndex:uq_gateway_upstream_file_mapping,priority:1"`
	ChannelID          int       `json:"channel_id" gorm:"not null;uniqueIndex:uq_gateway_upstream_file_mapping,priority:2"`
	KeyFingerprint     string    `json:"-" gorm:"size:64;not null;uniqueIndex:uq_gateway_upstream_file_mapping,priority:3"`
	BaseURLFingerprint string    `json:"-" gorm:"size:64;not null;uniqueIndex:uq_gateway_upstream_file_mapping,priority:4"`
	Protocol           string    `json:"protocol" gorm:"size:32;not null;uniqueIndex:uq_gateway_upstream_file_mapping,priority:5"`
	UpstreamFileID     string    `json:"upstream_file_id" gorm:"size:255"`
	Status             string    `json:"status" gorm:"size:16;not null;index"`
	LastError          string    `json:"-" gorm:"type:text"`
	CreatedAt          time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt          time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	LastUsedAt         time.Time `json:"last_used_at" gorm:"index"`
}

func (UpstreamFileMapping) TableName() string { return "gateway_upstream_file_mappings" }
