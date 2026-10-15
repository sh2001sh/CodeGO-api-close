package app

import (
	"sync"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

var routePoolAutoBuildOnce sync.Once
var routePoolAutoBuildRunning sync.Map

// StartMarketplaceRoutePoolAutoBuildTask runs enabled pool plans server-side.
func StartMarketplaceRoutePoolAutoBuildTask() {
	routePoolAutoBuildOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			runDueRoutePoolAutoBuilds()
			for range ticker.C {
				runDueRoutePoolAutoBuilds()
			}
		}()
	})
}

func runDueRoutePoolAutoBuilds() {
	if platformdb.DB == nil {
		return
	}
	var pools []marketplaceschema.RoutePool
	if err := platformdb.DB.Where("auto_build_enabled = ?", true).Find(&pools).Error; err != nil {
		platformobservability.SysError("list route pool auto builds: " + err.Error())
		return
	}
	now := time.Now().UTC()
	for _, pool := range pools {
		if pool.AutoBuildNextAt != nil && pool.AutoBuildNextAt.After(now) {
			continue
		}
		if _, loaded := routePoolAutoBuildRunning.LoadOrStore(pool.ID, struct{}{}); loaded {
			continue
		}
		go func(item marketplaceschema.RoutePool) {
			defer routePoolAutoBuildRunning.Delete(item.ID)
			if _, err := RunRoutePoolAutoBuild(item.OwnerUserID, item.ID); err != nil {
				platformobservability.SysError("run route pool auto build: " + err.Error())
				_ = platformdb.DB.Model(&marketplaceschema.RoutePool{}).Where("id = ?", item.ID).Updates(map[string]interface{}{"auto_build_last_error": err.Error(), "auto_build_next_at": now.Add(15 * time.Minute)}).Error
			}
		}(pool)
	}
}
