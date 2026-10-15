package schema

// AccountRequestAbuseState survives Redis eviction and service restarts. A
// restriction episode counts once; only a new window after recovery can ban.
type AccountRequestAbuseState struct {
	UserID          int    `gorm:"primaryKey;autoIncrement:false"`
	Strikes         int    `gorm:"not null"`
	RestrictedUntil int64  `gorm:"not null"`
	LastWindowEnd   int64  `gorm:"not null"`
	Blocked         bool   `gorm:"not null"`
	Evidence        string `gorm:"type:text"`
	UpdatedAt       int64  `gorm:"autoUpdateTime"`
}
