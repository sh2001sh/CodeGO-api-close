package domain

type GroupBuyListItem struct {
	Id              int64   `json:"id"`
	PlanId          int     `json:"plan_id"`
	PlanName        string  `json:"plan_name"`
	PlanPrice       float64 `json:"plan_price"`
	Currency        string  `json:"currency"`
	BaseQuotaUSD    float64 `json:"base_quota_usd"`
	CurrentCount    int     `json:"current_count"`
	TargetCount     int     `json:"target_count"`
	BonusAt2        float64 `json:"bonus_at_2"`
	BonusAt3        float64 `json:"bonus_at_3"`
	BonusAt5        float64 `json:"bonus_at_5"`
	ExpiresAt       int64   `json:"expires_at"`
	InitiatorId     int     `json:"initiator_id"`
	InitiatorAvatar string  `json:"initiator_avatar"`
	Status          string  `json:"status"`
	Joined          bool    `json:"joined,omitempty"`
}
