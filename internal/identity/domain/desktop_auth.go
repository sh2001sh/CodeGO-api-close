package domain

const (
	DesktopAuthSessionStatusPending  = "pending"
	DesktopAuthSessionStatusApproved = "approved"
	DesktopAuthSessionStatusExpired  = "expired"
	DesktopAuthSessionStatusRejected = "rejected"

	DesktopAuthorizedDeviceStatusActive  = "active"
	DesktopAuthorizedDeviceStatusRevoked = "revoked"

	DesktopScopeAccountRead    = "desktop:account:read"
	DesktopScopeLogsRead       = "desktop:logs:read"
	DesktopScopeTokensRead     = "desktop:tokens:read"
	DesktopScopeTokensWrite    = "desktop:tokens:write"
	DesktopScopeConfigRead     = "desktop:config:read"
	DesktopScopeConfigWrite    = "desktop:config:write"
	DesktopScopeTelemetryWrite = "desktop:telemetry:write"
)

// DesktopAuthSession records a browser-approved desktop authorization flow.
type DesktopAuthSession struct {
	SessionID  string `json:"session_id" gorm:"primaryKey;type:varchar(64)"`
	UserCode   string `json:"user_code" gorm:"uniqueIndex;type:varchar(16);not null"`
	UserID     int    `json:"user_id" gorm:"index;default:0"`
	DeviceID   int    `json:"device_id" gorm:"index;default:0"`
	DeviceName string `json:"device_name" gorm:"type:varchar(128);not null"`
	Platform   string `json:"platform" gorm:"type:varchar(64);default:''"`
	AppVersion string `json:"app_version" gorm:"type:varchar(64);default:''"`
	Status     string `json:"status" gorm:"type:varchar(32);index;not null"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index"`
	ApprovedAt int64  `json:"approved_at" gorm:"bigint;default:0"`
	ExpiresAt  int64  `json:"expires_at" gorm:"bigint;index"`
}

// DesktopAuthorizedDevice records an active desktop client credential.
type DesktopAuthorizedDevice struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	UserID      int    `json:"user_id" gorm:"index;not null"`
	DeviceName  string `json:"device_name" gorm:"type:varchar(128);not null"`
	Platform    string `json:"platform" gorm:"type:varchar(64);default:''"`
	AppVersion  string `json:"app_version" gorm:"type:varchar(64);default:''"`
	AccessToken string `json:"access_token" gorm:"uniqueIndex;type:varchar(128);not null"`
	Scopes      string `json:"scopes" gorm:"type:text;default:''"`
	Status      string `json:"status" gorm:"type:varchar(32);index;not null"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;index"`
	LastUsedAt  int64  `json:"last_used_at" gorm:"bigint;default:0"`
	ExpiresAt   int64  `json:"expires_at" gorm:"bigint;default:0"`
	RevokedAt   int64  `json:"revoked_at" gorm:"bigint;default:0"`
}
