package app

import (
	"strings"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

// Removing a group from saved pools does not delete the group or its earnings.
func removeGroupFromRoutePools(tx *gorm.DB, groupID string) error {
	if err := tx.Where("group_id = ?", groupID).Delete(&marketplaceschema.AutoRoutePoolMember{}).Error; err != nil {
		return err
	}
	return tx.Where("group_id = ?", groupID).Delete(&marketplaceschema.RoutePoolMember{}).Error
}

// Prune groups that were taken offline before lifecycle cleanup was added.
// Access restrictions and model availability must not delete a user's choice.
func pruneInactiveRoutePoolGroups(selected map[string]int) error {
	ids := make([]string, 0, len(selected))
	for id := range selected {
		if !strings.HasPrefix(id, officialAutoRoutePrefix) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var active []string
	if err := platformdb.DB.Model(&marketplaceschema.Group{}).
		Where("id IN ? AND lifecycle_status IN ?", ids, []string{marketplacedomain.LifecycleActive, marketplacedomain.LifecycleDegraded}).
		Pluck("id", &active).Error; err != nil {
		return err
	}
	valid := make(map[string]bool, len(active))
	for _, id := range active {
		valid[id] = true
	}
	stale := make([]string, 0)
	for _, id := range ids {
		if !valid[id] {
			stale = append(stale, id)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id IN ?", stale).Delete(&marketplaceschema.AutoRoutePoolMember{}).Error; err != nil {
			return err
		}
		return tx.Where("group_id IN ?", stale).Delete(&marketplaceschema.RoutePoolMember{}).Error
	}); err != nil {
		return err
	}
	for _, id := range stale {
		delete(selected, id)
	}
	return nil
}
