package projection

import (
	"context"
	"fmt"
	"sync"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
)

type UserUsageDaily struct {
	Day              string `gorm:"column:day;primaryKey;size:10"`
	UserID           int    `gorm:"column:user_id;primaryKey;index"`
	RequestCount     int64  `gorm:"column:request_count"`
	PromptTokens     int64  `gorm:"column:prompt_tokens"`
	CompletionTokens int64  `gorm:"column:completion_tokens"`
	Quota            int64  `gorm:"column:quota"`
	UpdatedAt        time.Time
}

func (UserUsageDaily) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "readmodel.user_usage_daily"
	}
	return "readmodel_user_usage_daily"
}

type ChannelUsageDaily struct {
	Day              string `gorm:"column:day;primaryKey;size:10"`
	ChannelID        int    `gorm:"column:channel_id;primaryKey;index"`
	RequestCount     int64  `gorm:"column:request_count"`
	PromptTokens     int64  `gorm:"column:prompt_tokens"`
	CompletionTokens int64  `gorm:"column:completion_tokens"`
	Quota            int64  `gorm:"column:quota"`
	UpdatedAt        time.Time
}

type UsageDailyCursor struct {
	Name      string    `gorm:"column:name;primaryKey;size:64"`
	LastLogID int64     `gorm:"column:last_log_id"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (UsageDailyCursor) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "readmodel.usage_daily_cursors"
	}
	return "readmodel_usage_daily_cursors"
}

func (ChannelUsageDaily) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "readmodel.channel_usage_daily"
	}
	return "readmodel_channel_usage_daily"
}

type BillingAccountView struct {
	AccountID        string `gorm:"column:account_id;primaryKey;size:64"`
	AccountType      string `gorm:"column:account_type;size:32;index"`
	OwnerType        string `gorm:"column:owner_type;size:32;index"`
	OwnerID          int64  `gorm:"column:owner_id;index"`
	QuotaUnit        string `gorm:"column:quota_unit;size:32"`
	AvailableBalance int64  `gorm:"column:available_balance"`
	ReservedBalance  int64  `gorm:"column:reserved_balance"`
	ConsumedTotal    int64  `gorm:"column:consumed_total"`
	RefundedTotal    int64  `gorm:"column:refunded_total"`
	GrantedTotal     int64  `gorm:"column:granted_total"`
	UpdatedAt        time.Time
}

type readModelSchemaMigration struct {
	ID        string `gorm:"primaryKey;size:128"`
	AppliedAt time.Time
}

func (readModelSchemaMigration) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "platform.schema_migrations"
	}
	return "platform_schema_migrations"
}

func (BillingAccountView) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "readmodel.billing_account_views"
	}
	return "readmodel_billing_account_views"
}

var readModelWorkerOnce sync.Once

func StartReadModelWorker(ctx context.Context) {
	readModelWorkerOnce.Do(func() {
		go func() {
			for {
				if err := RebuildReadModels(ctx); err != nil {
					platformobservability.SysError("read model rebuild failed: " + err.Error())
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Hour):
				}
			}
		}()
	})
}

func RebuildReadModels(ctx context.Context) error {
	if platformdb.DB == nil {
		return fmt.Errorf("primary database is not initialized")
	}
	if err := EnsureReadModelSchema(); err != nil {
		return err
	}
	if err := rebuildBillingAccountViews(ctx); err != nil {
		return err
	}
	if platformdb.LogDB == nil {
		return nil
	}
	return rebuildUsageDaily(ctx)
}

// EnsureReadModelSchema creates the dedicated reporting tables.
func EnsureReadModelSchema() error {
	if platformdb.DB == nil {
		return fmt.Errorf("primary database is not initialized")
	}
	return platformdb.DB.AutoMigrate(&UserUsageDaily{}, &ChannelUsageDaily{}, &BillingAccountView{}, &UsageDailyCursor{})
}

// ApplyReadModelMigrations creates reporting tables and records the schema version.
func ApplyReadModelMigrations(ctx context.Context) error {
	if platformdb.DB == nil {
		return fmt.Errorf("primary database is not initialized")
	}
	db := platformdb.DB.WithContext(ctx)
	if err := db.AutoMigrate(&readModelSchemaMigration{}); err != nil {
		return err
	}
	steps := []struct {
		id     string
		models []any
	}{
		{id: "20260710_read_models", models: []any{&UserUsageDaily{}, &ChannelUsageDaily{}, &BillingAccountView{}}},
		{id: "20260819_usage_daily_incremental", models: []any{&UsageDailyCursor{}}},
	}
	for _, step := range steps {
		var applied readModelSchemaMigration
		err := db.Where("id = ?", step.id).First(&applied).Error
		if err == nil {
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(step.models...); err != nil {
				return err
			}
			return tx.Create(&readModelSchemaMigration{ID: step.id}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func rebuildBillingAccountViews(ctx context.Context) error {
	var accounts []billingschema.BillingAccount
	if err := platformdb.DB.WithContext(ctx).Find(&accounts).Error; err != nil {
		return err
	}
	views := make([]BillingAccountView, 0, len(accounts))
	for _, account := range accounts {
		var snapshot billingschema.BillingBalanceSnapshot
		if err := platformdb.DB.WithContext(ctx).Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		views = append(views, BillingAccountView{AccountID: account.AccountID, AccountType: account.AccountType, OwnerType: account.OwnerType, OwnerID: account.OwnerID, QuotaUnit: account.QuotaUnit, AvailableBalance: snapshot.AvailableBalance, ReservedBalance: snapshot.ReservedBalance, ConsumedTotal: snapshot.ConsumedTotal, RefundedTotal: snapshot.RefundedTotal, GrantedTotal: snapshot.GrantedTotal})
	}
	return platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&BillingAccountView{}).Error; err != nil {
			return err
		}
		if len(views) == 0 {
			return nil
		}
		return tx.CreateInBatches(views, 500).Error
	})
}
