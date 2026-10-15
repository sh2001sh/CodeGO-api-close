package schema

import "time"

// UserFile stores metadata for a user-uploaded OpenAI-compatible file.
// Content is kept on disk; the database only contains metadata and the path.
type UserFile struct {
	ID          string     `json:"id" gorm:"column:id;primaryKey;size:64"`
	UserID      int        `json:"user_id" gorm:"column:user_id;index:idx_user_files_user_created;uniqueIndex:uq_user_files_user_sha256,priority:1"`
	SHA256      string     `json:"sha256" gorm:"column:sha256;size:64;uniqueIndex:uq_user_files_user_sha256,priority:2"`
	Purpose     string     `json:"purpose" gorm:"column:purpose;size:64"`
	Filename    string     `json:"filename" gorm:"column:filename;size:255"`
	MimeType    string     `json:"mime_type" gorm:"column:mime_type;size:128"`
	Size        int64      `json:"bytes" gorm:"column:size"`
	StoragePath string     `json:"-" gorm:"column:storage_path;size:1024"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime;index:idx_user_files_user_created"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty" gorm:"column:last_used_at;index:idx_gateway_user_files_last_used"`
}

func (UserFile) TableName() string { return "gateway_user_files" }
