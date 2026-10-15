package schema

import "time"

// OIDCAuthorizationCode stores only a hash of the short-lived authorization code.
type OIDCAuthorizationCode struct {
	CodeHash      string     `gorm:"primaryKey;size:64"`
	ClientID      string     `gorm:"size:128;not null;index"`
	UserID        int        `gorm:"not null;index"`
	RedirectURI   string     `gorm:"size:512;not null"`
	Scope         string     `gorm:"size:256;not null"`
	Nonce         string     `gorm:"size:256"`
	CodeChallenge string     `gorm:"size:128;not null"`
	ExpiresAt     time.Time  `gorm:"not null;index"`
	UsedAt        *time.Time `gorm:"index"`
}

func (OIDCAuthorizationCode) TableName() string { return "codego_oidc_authorization_codes" }

// OIDCAccessToken stores only a hash of the bearer token returned to an OIDC client.
type OIDCAccessToken struct {
	TokenHash string     `gorm:"primaryKey;size:64"`
	ClientID  string     `gorm:"size:128;not null;index"`
	UserID    int        `gorm:"not null;index"`
	Scope     string     `gorm:"size:256;not null"`
	ExpiresAt time.Time  `gorm:"not null;index"`
	RevokedAt *time.Time `gorm:"index"`
}

func (OIDCAccessToken) TableName() string { return "codego_oidc_access_tokens" }
