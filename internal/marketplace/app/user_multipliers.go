package app

import (
	"errors"
	"fmt"
	"sort"
	"time"

	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MultiplierTarget struct {
	ChannelID string `json:"channel_id"`
	UserID    int    `json:"user_id"`
}

type OwnerMultiplierItem struct {
	MultiplierTarget
	ExternalUserID   string    `json:"external_user_id"`
	ChannelName      string    `json:"channel_name"`
	PublicMultiplier float64   `json:"public_multiplier"`
	Multiplier       float64   `json:"multiplier"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func ListOwnerMultipliers(owner, page int) ([]OwnerMultiplierItem, int64, error) {
	if page < 1 {
		page = 1
	}
	query := platformdb.DB.Table(marketplaceschema.UserMultiplier{}.TableName()+" AS um").
		Joins("JOIN "+marketplaceschema.Channel{}.TableName()+" c ON c.id=um.channel_id AND c.deleted_at IS NULL").
		Joins("JOIN "+marketplaceschema.Group{}.TableName()+" g ON g.channel_id=c.id AND g.deleted_at IS NULL").
		Joins("JOIN users u ON u.id=um.user_id AND u.deleted_at IS NULL").Where("c.owner_user_id=?", owner)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []OwnerMultiplierItem{}
	err := query.Select("um.channel_id, um.user_id, u.external_id AS external_user_id, g.system_display_name AS channel_name, g.multiplier AS public_multiplier, um.multiplier, um.updated_at").
		Order("um.updated_at DESC, um.id DESC").Offset((page - 1) * 50).Limit(50).Scan(&items).Error
	return items, total, err
}

func BatchSetUserMultipliers(owner int, targets []MultiplierTarget, multiplier *float64) (int, error) {
	if len(targets) == 0 || len(targets) > 100 {
		return 0, errors.New("每次请选择 1 至 100 条专属倍率")
	}
	if multiplier != nil {
		if err := validateMultiplier(*multiplier); err != nil {
			return 0, err
		}
		normalized := marketplacedomain.NormalizeMultiplier(*multiplier)
		if normalized <= 0 {
			return 0, errors.New("倍率精度过小")
		}
		multiplier = &normalized
	}
	// Lock channels in a stable order across manual, batch and bargain writes.
	targets = append([]MultiplierTarget(nil), targets...)
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].ChannelID == targets[j].ChannelID {
			return targets[i].UserID < targets[j].UserID
		}
		return targets[i].ChannelID < targets[j].ChannelID
	})
	changed := 0
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		seen := map[MultiplierTarget]bool{}
		for _, target := range targets {
			if seen[target] {
				continue
			}
			seen[target] = true
			if target.UserID <= 0 || target.ChannelID == "" {
				return errors.New("用户或渠道无效")
			}
			var channel marketplaceschema.Channel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND owner_user_id=?", target.ChannelID, owner).First(&channel).Error; err != nil {
				return errors.New("渠道不存在或无权修改")
			}
			var user identityschema.User
			if err := tx.Select("id").First(&user, target.UserID).Error; err != nil {
				return errors.New("用户不存在")
			}
			var group marketplaceschema.Group
			if err := tx.Where("channel_id=?", channel.ID).First(&group).Error; err != nil {
				return err
			}
			updated, err := setUserMultiplierTx(tx, group, target.UserID, multiplier, "manual")
			if err != nil {
				return err
			}
			if updated {
				changed++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}

func setUserMultiplierTx(tx *gorm.DB, group marketplaceschema.Group, userID int, multiplier *float64, source string) (bool, error) {
	var current marketplaceschema.UserMultiplier
	err := tx.Where("channel_id=? AND user_id=?", group.ChannelID, userID).First(&current).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	exists := err == nil
	if !exists && multiplier == nil {
		return false, nil
	}
	if exists && multiplier != nil && current.Multiplier == *multiplier {
		return false, nil
	}
	previous := group.Multiplier
	if exists {
		previous = current.Multiplier
	}
	next := group.Multiplier
	if multiplier == nil {
		err = tx.Delete(&current).Error
	} else {
		next = *multiplier
		if exists {
			err = tx.Model(&current).Update("multiplier", next).Error
		} else {
			err = tx.Create(&marketplaceschema.UserMultiplier{ChannelID: group.ChannelID, UserID: userID, Multiplier: next}).Error
		}
	}
	if err != nil {
		return false, err
	}
	notice := marketplaceschema.MultiplierNotice{UserID: userID, ChannelID: group.ChannelID, ChannelName: group.SystemDisplayName, PreviousMultiplier: previous, Multiplier: next, Cleared: multiplier == nil, Source: source}
	if err := tx.Create(&notice).Error; err != nil {
		return false, fmt.Errorf("保存倍率通知失败: %w", err)
	}
	return true, nil
}

func ListMultiplierNotices(userID int) ([]marketplaceschema.MultiplierNotice, error) {
	items := []marketplaceschema.MultiplierNotice{}
	err := platformdb.DB.Where("user_id=? AND read_at IS NULL", userID).Order("id ASC").Limit(50).Find(&items).Error
	return items, err
}

func ReadMultiplierNotice(userID int, id uint64) error {
	return platformdb.DB.Model(&marketplaceschema.MultiplierNotice{}).Where("id=? AND user_id=? AND read_at IS NULL", id, userID).Update("read_at", time.Now().UTC()).Error
}
