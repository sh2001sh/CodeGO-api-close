package app

import (
	"context"
	"time"

	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

var marketplaceAutoProbeTasks = verificationTaskRegistry{tasks: make(map[verificationTaskKey]verificationTask)}

// Local changes cancel immediately. The database check also observes changes
// made by another control replica without depending on Redis availability.
func beginMarketplaceBackgroundProbe(parent context.Context, channel *marketplaceschema.Channel, kind string) (context.Context, func(), bool) {
	internalID := 0
	if channel.InternalChannelID != nil {
		internalID = *channel.InternalChannelID
	}
	if internalID > 0 && gatewayruntime.ActiveChannelRequests(internalID) > 0 {
		return nil, nil, false
	}
	ctx, finish, ok := marketplaceAutoProbeTasks.begin(parent, channel.ID, "background")
	if !ok {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, autoProbeLeaseTTL)
	done := make(chan struct{})
	db := platformdb.DB
	channelID, updatedAt := channel.ID, channel.UpdatedAt
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if internalID > 0 && gatewayruntime.ActiveChannelRequests(internalID) > 0 {
					cancel()
					return
				}
				var current marketplaceschema.Channel
				checkCtx, checkCancel := context.WithTimeout(ctx, time.Second)
				err := db.WithContext(checkCtx).Select("id", "status", "auto_probe_enabled", "updated_at").First(&current, "id = ?", channelID).Error
				checkCancel()
				if err != nil || !current.UpdatedAt.Equal(updatedAt) || !marketplaceBackgroundProbeAllowed(&current) ||
					(kind == "auto" && !marketplaceAutoProbeEligible(&current)) {
					if err != nil && ctx.Err() == nil {
						platformobservability.SysError("check marketplace background probe: " + err.Error())
					}
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(); <-done; finish() }, true
}
