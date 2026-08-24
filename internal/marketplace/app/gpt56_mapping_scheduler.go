package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

var gpt56SchedulerOnce sync.Once

// StartGPT56MappingScheduler checks eligible GPT-5.6 channels once per day.
func StartGPT56MappingScheduler(ctx context.Context) {
	gpt56SchedulerOnce.Do(func() {
		go func() {
			runDueGPT56MappingChecks(ctx)
			ticker := time.NewTicker(GPT56MappingSchedulerInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					runDueGPT56MappingChecks(ctx)
				}
			}
		}()
	})
}

func runDueGPT56MappingChecks(ctx context.Context) {
	if platformdb.DB == nil || ctx.Err() != nil {
		return
	}
	var channels []marketplaceschema.Channel
	dueBefore := time.Now().UTC().Add(-24 * time.Hour)
	if err := platformdb.DB.Where(
		"status IN ? AND (declared_models LIKE ? OR declared_models LIKE ? OR declared_models LIKE ?) AND (gpt56_mapping_checked_at IS NULL OR gpt56_mapping_checked_at <= ?)",
		[]string{marketplacedomain.LifecycleActive, marketplacedomain.LifecycleDegraded},
		"%gpt-5.6-sol%", "%gpt-5.6-terra%", "%gpt-5.6-luna%", dueBefore,
	).Find(&channels).Error; err != nil {
		platformobservability.SysError("load due GPT-5.6 mapping checks: " + err.Error())
		return
	}
	for index := range channels {
		channel := &channels[index]
		taskCtx, finish, started := marketplaceVerificationTasks.begin(
			ctx, channel.ID, verificationTaskGPT56Mapping,
		)
		if !started {
			continue
		}
		_, err := runGPT56MappingCheckWithRequest(taskCtx, channel, gpt56CheckRequest{
			Level: GPT56MappingLevelDailyLight, Trigger: GPT56MappingTriggerScheduled,
		})
		finish()
		if err != nil {
			platformobservability.SysError(fmt.Sprintf(
				"run GPT-5.6 mapping check channel=%s: %s", channel.ID, err.Error(),
			))
		}
	}
}
