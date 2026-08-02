package domain

import commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"

type BlindBoxOverview struct {
	AvailableBoxes         int                                 `json:"available_boxes"`
	PendingBoxes           int                                 `json:"pending_boxes"`
	RemainingQuota         int64                               `json:"remaining_quota"`
	ClaudeQuota            int64                               `json:"claude_quota"`
	PityProgress           int                                 `json:"pity_progress"`
	PityThreshold          int                                 `json:"pity_threshold"`
	EffectivePityThreshold int                                 `json:"effective_pity_threshold"`
	PurchasedToday         int                                 `json:"purchased_today"`
	PurchasedThisMonth     int                                 `json:"purchased_this_month"`
	RecentRecords          []commerceschema.BlindBoxOpenRecord `json:"recent_records"`
}

// BlindBoxRewardStatistics summarizes one category of rewards opened by a user.
type BlindBoxRewardStatistics struct {
	RewardType   string  `json:"reward_type"`
	OpenedCount  int64   `json:"opened_count"`
	RewardUSD    float64 `json:"reward_usd"`
	CreditAmount int64   `json:"credit_amount"`
}

// BlindBoxStatistics summarizes the user's full blind-box opening history.
type BlindBoxStatistics struct {
	TotalOpened int64                      `json:"total_opened"`
	PityWins    int64                      `json:"pity_wins"`
	Rewards     []BlindBoxRewardStatistics `json:"rewards"`
}
