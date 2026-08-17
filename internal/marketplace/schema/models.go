package schema

import (
	"time"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

type Channel struct {
	ID                               string         `json:"id" gorm:"column:id;primaryKey;size:64"`
	OwnerUserID                      int            `json:"owner_user_id" gorm:"column:owner_user_id;index;not null"`
	ProviderType                     string         `json:"provider_type" gorm:"column:provider_type;size:32;not null"`
	SubmittedSourceLabel             string         `json:"submitted_source_label" gorm:"column:submitted_source_label;size:40"`
	ApprovedSourceLabel              string         `json:"approved_source_label" gorm:"column:approved_source_label;size:40"`
	SourceLabelStatus                string         `json:"source_label_status" gorm:"column:source_label_status;size:16;index"`
	BaseURLCiphertext                string         `json:"-" gorm:"column:base_url_ciphertext;type:text;not null"`
	CredentialCiphertext             string         `json:"-" gorm:"column:credential_ciphertext;type:text;not null"`
	CredentialTail                   string         `json:"credential_tail" gorm:"column:credential_tail;size:12"`
	CredentialVersion                int            `json:"credential_version" gorm:"column:credential_version;not null;default:1"`
	DeclaredModels                   string         `json:"-" gorm:"column:declared_models;type:text"`
	ModelPrices                      string         `json:"-" gorm:"column:model_prices;type:text"`
	ModelVerificationResults         string         `json:"-" gorm:"column:model_verification_results;type:text"`
	ConnectivityTestStatus           string         `json:"connectivity_test_status" gorm:"column:connectivity_test_status;size:24;index"`
	ConnectivityTestCheckedAt        *time.Time     `json:"connectivity_test_checked_at" gorm:"column:connectivity_test_checked_at;index"`
	ModelConsistencyStatus           string         `json:"model_consistency_status" gorm:"column:model_consistency_status;size:24;index"`
	GPT56MappingResults              string         `json:"-" gorm:"column:gpt56_mapping_results;type:text"`
	GPT56MappingStatus               string         `json:"gpt56_mapping_status" gorm:"column:gpt56_mapping_status;size:32;index"`
	GPT56MappingCheckedAt            *time.Time     `json:"gpt56_mapping_checked_at" gorm:"column:gpt56_mapping_checked_at;index"`
	GPT56MappingLevel                string         `json:"gpt56_mapping_level" gorm:"column:gpt56_mapping_level;size:32;index"`
	GPT56MappingTrigger              string         `json:"gpt56_mapping_trigger" gorm:"column:gpt56_mapping_trigger;size:32;index"`
	TransportCapabilities            string         `json:"-" gorm:"column:transport_capabilities;type:text"`
	AutoProbeEnabled                 bool           `json:"auto_probe_enabled" gorm:"column:auto_probe_enabled;not null;default:false;index"`
	AutoProbeIntervalMinutes         int            `json:"auto_probe_interval_minutes" gorm:"column:auto_probe_interval_minutes;not null;default:10"`
	AutoProbeModel                   string         `json:"auto_probe_model" gorm:"column:auto_probe_model;size:128"`
	AutoProbeLastStatus              string         `json:"auto_probe_last_status" gorm:"column:auto_probe_last_status;size:24;index"`
	AutoProbeLastAt                  *time.Time     `json:"auto_probe_last_at" gorm:"column:auto_probe_last_at;index"`
	MaxConcurrency                   int            `json:"max_concurrency" gorm:"column:max_concurrency;not null;default:1"`
	UserMaxConcurrency               int            `json:"user_max_concurrency" gorm:"column:user_max_concurrency;not null;default:0"`
	QPS                              float64        `json:"qps" gorm:"column:qps;not null;default:1"`
	MaintenanceWindow                string         `json:"maintenance_window" gorm:"column:maintenance_window;size:255"`
	SensitiveWordInterceptionEnabled *bool          `json:"sensitive_word_interception_enabled" gorm:"column:sensitive_word_interception_enabled;default:true"`
	Status                           string         `json:"status" gorm:"column:status;size:24;index;not null"`
	InternalChannelID                *int           `json:"internal_channel_id" gorm:"column:internal_channel_id;index"`
	LastReviewReason                 string         `json:"last_review_reason" gorm:"column:last_review_reason;size:500"`
	SourceLabelReviewReason          string         `json:"source_label_review_reason" gorm:"column:source_label_review_reason;size:500"`
	CreatedAt                        time.Time      `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt                        time.Time      `json:"updated_at" gorm:"column:updated_at;autoCreateTime;autoUpdateTime"`
	DeletedAt                        gorm.DeletedAt `json:"-" gorm:"column:deleted_at;index"`
}

func (Channel) TableName() string { return tableName("channels") }

// ChannelIDSequence allocates short, monotonic public marketplace channel IDs.
type ChannelIDSequence struct {
	ID        uint64    `json:"-" gorm:"primaryKey;autoIncrement"`
	CreatedAt time.Time `json:"-" gorm:"column:created_at;autoCreateTime"`
}

func (ChannelIDSequence) TableName() string { return tableName("channel_id_sequences") }

func (channel *Channel) BeforeCreate(_ *gorm.DB) error {
	if channel.ID == "" {
		channel.ID = platformruntime.GetUUID()
	}
	return nil
}

type Group struct {
	ID                 string         `json:"id" gorm:"column:id;primaryKey;size:64"`
	ChannelID          string         `json:"channel_id" gorm:"column:channel_id;size:64;uniqueIndex;not null"`
	OwnerUserID        int            `json:"owner_user_id" gorm:"column:owner_user_id;index;not null"`
	PublicSlug         string         `json:"public_slug" gorm:"column:public_slug;size:80;uniqueIndex;not null"`
	SystemDisplayName  string         `json:"system_display_name" gorm:"column:system_display_name;size:128;index;not null"`
	InternalGroupName  string         `json:"-" gorm:"column:internal_group_name;size:64;uniqueIndex;not null"`
	OwnerDisplayName   string         `json:"owner_display_name" gorm:"column:owner_display_name;size:128;index"`
	SourceType         string         `json:"source_type" gorm:"column:source_type;size:32;not null"`
	CreditPoolPolicy   string         `json:"credit_pool_policy" gorm:"column:credit_pool_policy;size:40;not null"`
	Multiplier         float64        `json:"multiplier" gorm:"column:multiplier;not null"`
	RoutingVersion     int            `json:"routing_version" gorm:"column:routing_version;not null;default:1"`
	LifecycleStatus    string         `json:"lifecycle_status" gorm:"column:lifecycle_status;size:24;index;not null"`
	VerificationStatus string         `json:"verification_status" gorm:"column:verification_status;size:24;index;not null"`
	Visibility         string         `json:"visibility" gorm:"column:visibility;size:16;index;not null"`
	PublishedAt        *time.Time     `json:"published_at" gorm:"column:published_at;index"`
	VerificationDueAt  *time.Time     `json:"verification_due_at" gorm:"column:verification_due_at;index"`
	CreatedAt          time.Time      `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time      `json:"updated_at" gorm:"column:updated_at;autoCreateTime;autoUpdateTime"`
	DeletedAt          gorm.DeletedAt `json:"-" gorm:"column:deleted_at;index"`
}

func (Group) TableName() string { return tableName("groups") }

type VerificationRun struct {
	ID              string     `json:"id" gorm:"column:id;primaryKey;size:64"`
	ChannelID       string     `json:"channel_id" gorm:"column:channel_id;size:64;index;not null"`
	Status          string     `json:"status" gorm:"column:status;size:24;index;not null"`
	Stage           string     `json:"stage" gorm:"column:stage;size:32"`
	DetectorName    string     `json:"detector_name" gorm:"column:detector_name;size:64"`
	DetectorVersion string     `json:"detector_version" gorm:"column:detector_version;size:32"`
	RulesetVersion  string     `json:"ruleset_version" gorm:"column:ruleset_version;size:32"`
	EvidenceHash    string     `json:"evidence_hash" gorm:"column:evidence_hash;size:128"`
	Summary         string     `json:"summary" gorm:"column:summary;size:1000"`
	StartedAt       *time.Time `json:"started_at" gorm:"column:started_at"`
	CompletedAt     *time.Time `json:"completed_at" gorm:"column:completed_at"`
	ExpiresAt       *time.Time `json:"expires_at" gorm:"column:expires_at;index"`
	CreatedAt       time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (VerificationRun) TableName() string { return tableName("verification_runs") }

func (run *VerificationRun) BeforeCreate(_ *gorm.DB) error {
	if run.ID == "" {
		run.ID = platformruntime.GetUUID()
	}
	return nil
}

// GPT56MappingRun preserves one complete detector cycle and its evidence.
type GPT56MappingRun struct {
	ID          string     `json:"id" gorm:"column:id;primaryKey;size:64"`
	ChannelID   string     `json:"channel_id" gorm:"column:channel_id;size:64;index;not null"`
	ParentRunID string     `json:"parent_run_id,omitempty" gorm:"column:parent_run_id;size:64;index"`
	Level       string     `json:"level" gorm:"column:level;size:32;index;not null"`
	Trigger     string     `json:"trigger" gorm:"column:trigger;size:32;index;not null"`
	Status      string     `json:"status" gorm:"column:status;size:32;index;not null"`
	Results     string     `json:"-" gorm:"column:results;type:text"`
	StartedAt   time.Time  `json:"started_at" gorm:"column:started_at;index;not null"`
	CompletedAt *time.Time `json:"completed_at" gorm:"column:completed_at;index"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (GPT56MappingRun) TableName() string { return tableName("gpt56_mapping_runs") }

func (run *GPT56MappingRun) BeforeCreate(_ *gorm.DB) error {
	if run.ID == "" {
		run.ID = platformruntime.GetUUID()
	}
	return nil
}

type RankingSnapshot struct {
	ID                   string    `json:"id" gorm:"column:id;primaryKey;size:64"`
	GroupID              string    `json:"group_id" gorm:"column:group_id;size:64;uniqueIndex:uq_marketplace_rank_snapshot,priority:1"`
	WindowHours          int       `json:"window_hours" gorm:"column:window_hours;uniqueIndex:uq_marketplace_rank_snapshot,priority:2"`
	RankingVersion       string    `json:"ranking_version" gorm:"column:ranking_version;size:32;uniqueIndex:uq_marketplace_rank_snapshot,priority:3"`
	Rank                 int       `json:"rank" gorm:"column:rank;index"`
	Score                float64   `json:"score" gorm:"column:score"`
	RawSuccessRate       float64   `json:"raw_success_rate" gorm:"column:raw_success_rate"`
	WilsonSuccessRate    float64   `json:"wilson_success_rate" gorm:"column:wilson_success_rate"`
	AvgTTFTMs            float64   `json:"avg_ttft_ms" gorm:"column:avg_ttft_ms"`
	AvgLatencyMs         float64   `json:"avg_latency_ms" gorm:"column:avg_latency_ms"`
	AvgTPS               float64   `json:"avg_tps" gorm:"column:avg_tps"`
	CacheHitRate         float64   `json:"cache_hit_rate" gorm:"column:cache_hit_rate"`
	RequestCount         int64     `json:"request_count" gorm:"column:request_count"`
	IndependentConsumers int64     `json:"independent_consumers" gorm:"column:independent_consumers"`
	Observing            bool      `json:"observing" gorm:"column:observing;index"`
	CalculatedAt         time.Time `json:"calculated_at" gorm:"column:calculated_at;index"`
}

func (RankingSnapshot) TableName() string { return tableName("ranking_snapshots") }

func (snapshot *RankingSnapshot) BeforeCreate(_ *gorm.DB) error {
	if snapshot.ID == "" {
		snapshot.ID = platformruntime.GetUUID()
	}
	return nil
}

// MultiplierTrendSnapshot records the market state used to render historical
// multiplier trends. One row is retained per group and 30-minute bucket.
type MultiplierTrendSnapshot struct {
	ID                uint64    `json:"-" gorm:"primaryKey;autoIncrement"`
	GroupID           string    `json:"group_id" gorm:"column:group_id;size:64;not null;uniqueIndex:uq_marketplace_multiplier_snapshot,priority:1;index"`
	ChannelID         string    `json:"channel_id" gorm:"column:channel_id;size:64;not null;index"`
	SourceLabel       string    `json:"source_label" gorm:"column:source_label;size:40;not null;index"`
	Models            string    `json:"-" gorm:"column:models;type:text;not null"`
	Multiplier        float64   `json:"multiplier" gorm:"column:multiplier;not null"`
	Reliable          bool      `json:"reliable" gorm:"column:reliable;not null;index"`
	RequestCount      int64     `json:"request_count" gorm:"column:request_count;not null"`
	WilsonSuccessRate float64   `json:"wilson_success_rate" gorm:"column:wilson_success_rate;not null"`
	BucketStartedAt   time.Time `json:"bucket_started_at" gorm:"column:bucket_started_at;not null;uniqueIndex:uq_marketplace_multiplier_snapshot,priority:2;index"`
	CapturedAt        time.Time `json:"captured_at" gorm:"column:captured_at;not null;index"`
}

func (MultiplierTrendSnapshot) TableName() string {
	return tableName("multiplier_trend_snapshots")
}

// ChannelFeedback stores one user's current assessment of a marketplace channel.
type ChannelFeedback struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelID string    `json:"channel_id" gorm:"column:channel_id;size:64;not null;uniqueIndex:uq_marketplace_channel_feedback,priority:1;index"`
	UserID    int       `json:"-" gorm:"column:user_id;not null;uniqueIndex:uq_marketplace_channel_feedback,priority:2;index"`
	Status    string    `json:"status" gorm:"column:status;size:24;not null;index"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoCreateTime;autoUpdateTime"`
}

func (ChannelFeedback) TableName() string {
	return tableName("channel_feedback")
}

type Settlement struct {
	ID                     string     `json:"id" gorm:"column:id;primaryKey;size:64"`
	RequestID              string     `json:"request_id" gorm:"column:request_id;size:64;uniqueIndex;not null"`
	GroupID                string     `json:"group_id" gorm:"column:group_id;size:64;index;not null"`
	OwnerUserID            int        `json:"owner_user_id" gorm:"column:owner_user_id;index;not null"`
	ConsumerUserID         int        `json:"consumer_user_id" gorm:"column:consumer_user_id;index;not null"`
	BillingSource          string     `json:"billing_source" gorm:"column:billing_source;size:24;not null;default:wallet"`
	ConsumerAmount         int64      `json:"consumer_amount" gorm:"column:consumer_amount;not null"`
	SettlementGrossAmount  int64      `json:"settlement_gross_amount" gorm:"column:settlement_gross_amount;not null;default:0"`
	PlatformCommission     int64      `json:"platform_commission" gorm:"column:platform_commission;not null"`
	TransactionFee         int64      `json:"transaction_fee" gorm:"column:transaction_fee;not null"`
	OwnerNetAmount         int64      `json:"owner_net_amount" gorm:"column:owner_net_amount;not null"`
	Multiplier             float64    `json:"multiplier" gorm:"column:multiplier;not null"`
	SubscriptionMultiplier float64    `json:"subscription_multiplier" gorm:"column:subscription_multiplier;not null;default:0"`
	Status                 string     `json:"status" gorm:"column:status;size:24;index;not null"`
	PendingAccountID       string     `json:"-" gorm:"column:pending_account_id;size:64"`
	AvailableAt            time.Time  `json:"available_at" gorm:"column:available_at;index"`
	ReleasedAt             *time.Time `json:"released_at" gorm:"column:released_at"`
	CreatedAt              time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (Settlement) TableName() string { return tableName("settlements") }

func (settlement *Settlement) BeforeCreate(_ *gorm.DB) error {
	if settlement.ID == "" {
		settlement.ID = platformruntime.GetUUID()
	}
	return nil
}

// AutoRoutePoolMember stores one user-selected marketplace group in the
// user's third-party automatic routing pool.
type AutoRoutePoolMember struct {
	ID          uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	OwnerUserID int       `json:"owner_user_id" gorm:"column:owner_user_id;not null;uniqueIndex:uq_marketplace_auto_pool_member,priority:1;index"`
	GroupID     string    `json:"group_id" gorm:"column:group_id;size:64;not null;uniqueIndex:uq_marketplace_auto_pool_member,priority:2;index"`
	Priority    int       `json:"priority" gorm:"column:priority;not null;default:0;index"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (AutoRoutePoolMember) TableName() string { return tableName("auto_route_pool_members") }

func tableName(name string) string {
	if platformdb.UsingPostgreSQL {
		return "marketplace." + name
	}
	return "marketplace_" + name
}
