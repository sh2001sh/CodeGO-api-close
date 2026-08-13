package domain

import commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"

type LuckyNumberSubscription struct {
	Subscription commerceschema.UserSubscription         `json:"subscription"`
	Plan         commerceschema.SubscriptionPlan         `json:"plan"`
	Number       *commerceschema.SubscriptionLuckyNumber `json:"number,omitempty"`
}

type BlindBoxLuckyNumber struct {
	BlindBoxOpenRecordId int    `json:"blind_box_open_record_id"`
	LuckySuffix          string `json:"lucky_suffix"`
	DrawDate             string `json:"draw_date"`
	ExpiresAt            int64  `json:"expires_at"`
	CreatedAt            int64  `json:"created_at"`
}

// LuckyNumberRules is the public, non-operational rule snapshot used by the activity page.
// Cost and budget controls remain admin-only fields.
type LuckyNumberRules struct {
	BaseReward1USD      float64 `json:"base_reward_1_usd"`
	BaseReward2USD      float64 `json:"base_reward_2_usd"`
	BaseReward3USD      float64 `json:"base_reward_3_usd"`
	BaseReward4USD      float64 `json:"base_reward_4_usd"`
	MultiplierLite      float64 `json:"multiplier_lite"`
	MultiplierStandard  float64 `json:"multiplier_standard"`
	MultiplierPro       float64 `json:"multiplier_pro"`
	MultiplierUltra     float64 `json:"multiplier_ultra"`
	JackpotInitialUSD   float64 `json:"jackpot_initial_usd"`
	JackpotIncrementUSD float64 `json:"jackpot_increment_usd"`
	JackpotCapUSD       float64 `json:"jackpot_cap_usd"`
}

// LuckyDrawView is the user-facing projection of a draw. Configuration and
// operational cost snapshots remain admin-only fields on the schema model.
type LuckyDrawView struct {
	Id             int     `json:"id"`
	DrawDate       string  `json:"draw_date"`
	WinningNumber  string  `json:"winning_number"`
	JackpotBefore  float64 `json:"jackpot_before"`
	JackpotAfter   float64 `json:"jackpot_after"`
	FullMatchCount int     `json:"full_match_count"`
	Status         string  `json:"status"`
	DrawnAt        int64   `json:"drawn_at"`
	CompletedAt    int64   `json:"completed_at"`
}

type LuckyNumberSelfPayload struct {
	Enabled         bool                      `json:"enabled"`
	Timezone        string                    `json:"timezone"`
	DrawHour        int                       `json:"draw_hour"`
	DrawMinute      int                       `json:"draw_minute"`
	NextDrawAt      int64                     `json:"next_draw_at"`
	TodayDraw       *LuckyDrawView            `json:"today_draw,omitempty"`
	PreviousDraw    *LuckyDrawView            `json:"previous_draw,omitempty"`
	JackpotUSD      float64                   `json:"jackpot_usd"`
	JackpotCapUSD   float64                   `json:"jackpot_cap_usd"`
	Rules           LuckyNumberRules          `json:"rules"`
	Subscriptions   []LuckyNumberSubscription `json:"subscriptions"`
	BlindBoxNumbers []BlindBoxLuckyNumber     `json:"today_blind_box_numbers"`
	RecentRewards   []LuckyRewardView         `json:"recent_rewards"`
}

type LuckyRewardView struct {
	Reward        LuckyRewardRecord `json:"reward"`
	DrawDate      string            `json:"draw_date"`
	WinningNumber string            `json:"winning_number"`
	RewardUSD     float64           `json:"reward_usd"`
}

type LuckyRewardRecord struct {
	Id                   int     `json:"id"`
	DrawId               int     `json:"draw_id"`
	UserSubscriptionId   int     `json:"user_subscription_id"`
	BlindBoxOpenRecordId int     `json:"blind_box_open_record_id,omitempty"`
	ParticipationType    string  `json:"participation_type,omitempty"`
	LuckyNumber          string  `json:"lucky_number"`
	MembershipTier       string  `json:"membership_tier"`
	MatchedDigits        int     `json:"matched_digits"`
	BaseRewardUSD        float64 `json:"base_reward_usd"`
	TierMultiplier       float64 `json:"tier_multiplier"`
	JackpotRewardUSD     float64 `json:"jackpot_reward_usd"`
	FinalRewardQuota     int64   `json:"final_reward_quota"`
	CreditStatus         string  `json:"credit_status"`
	CreditedAt           int64   `json:"credited_at"`
}

type LuckyRewardPage struct {
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int64             `json:"total"`
	Records  []LuckyRewardView `json:"records"`
}

type LuckyRewardNotification struct {
	Id        int             `json:"id"`
	Reward    LuckyRewardView `json:"reward"`
	ReadAt    int64           `json:"read_at"`
	CreatedAt int64           `json:"created_at"`
}

type LuckyRewardNotificationPage struct {
	UnreadCount int64                     `json:"unread_count"`
	Items       []LuckyRewardNotification `json:"items"`
}

type LuckyPublicWinPage struct {
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int64            `json:"total"`
	Records  []LuckyPublicWin `json:"records"`
}

type LuckyPublicWin struct {
	DrawDate       string  `json:"draw_date"`
	WinningNumber  string  `json:"winning_number"`
	MembershipTier string  `json:"membership_tier"`
	LuckySuffix    string  `json:"lucky_suffix"`
	MatchedDigits  int     `json:"matched_digits"`
	RewardUSD      float64 `json:"reward_usd"`
}

type LuckyDrawAdminView struct {
	Draw             commerceschema.SubscriptionLuckyDraw `json:"draw"`
	ParticipantCount int64                                `json:"participant_count"`
	RewardCount      int64                                `json:"reward_count"`
	CreditedCount    int64                                `json:"credited_count"`
	NominalRewardUSD float64                              `json:"nominal_reward_usd"`
	ActualCostCNY    float64                              `json:"actual_cost_cny"`
}

type LuckyBackfillResult struct {
	Scanned       int   `json:"scanned"`
	AlreadyExists int   `json:"already_exists"`
	Created       int   `json:"created"`
	Failed        int   `json:"failed"`
	FailedIDs     []int `json:"failed_ids"`
}

type LuckyDrawAdminPayload struct {
	Config                    any                  `json:"config"`
	Draws                     []LuckyDrawAdminView `json:"draws"`
	Page                      int                  `json:"page"`
	PageSize                  int                  `json:"page_size"`
	Total                     int64                `json:"total"`
	MonthlyNominalRewardUSD   float64              `json:"monthly_nominal_reward_usd"`
	MonthlyActualCostCNY      float64              `json:"monthly_actual_cost_cny"`
	MonthlyBudgetUSD          float64              `json:"monthly_budget_usd"`
	MonthlyBudgetUsagePercent float64              `json:"monthly_budget_usage_percent"`
}
