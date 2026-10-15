package app

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewaydomain "github.com/sh2001sh/new-api/internal/gateway/domain"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformschema "github.com/sh2001sh/new-api/internal/platform/schema"
	"gorm.io/gorm"
)

func migrateUnifiedCreditGPTGroupRatios() error {
	if !platformdb.DB.Migrator().HasTable(&gatewayschema.Channel{}) {
		return nil
	}
	var channels []gatewayschema.Channel
	if err := platformdb.DB.Find(&channels).Error; err != nil {
		return err
	}
	gptGroups := make(map[string]struct{})
	for index := range channels {
		channel := &channels[index]
		if !channel.IsOfficial() || gatewaydomain.GetSettings(channel).ClaudeWalletEnabled {
			continue
		}
		for _, group := range channel.GetGroups() {
			if trimmed := strings.TrimSpace(group); trimmed != "" {
				gptGroups[trimmed] = struct{}{}
			}
		}
	}
	if len(gptGroups) == 0 {
		return nil
	}

	groupNames := make([]string, 0, len(gptGroups))
	for group := range gptGroups {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)

	var encoded string
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		ratioMap := gatewaystore.GetGroupRatioCopy()
		var option platformschema.Option
		err := tx.Where("key = ?", "GroupRatio").First(&option).Error
		if err == nil && strings.TrimSpace(option.Value) != "" {
			if err := json.Unmarshal([]byte(option.Value), &ratioMap); err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		now := platformruntime.GetTimestamp()
		for _, group := range groupNames {
			ratio, exists := ratioMap[group]
			if !exists {
				continue
			}
			var audit commerceschema.UnifiedCreditGroupRatioMigration
			err := tx.Where("version = ? AND group_name = ?", commerceschema.UnifiedCreditMigrationVersion, group).First(&audit).Error
			if err == nil {
				ratioMap[group] = audit.RatioAfter
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			after := unifiedCreditTargetGroupRatio(group, ratio)
			if err := tx.Create(&commerceschema.UnifiedCreditGroupRatioMigration{
				Version: commerceschema.UnifiedCreditMigrationVersion, GroupName: group,
				RatioBefore: ratio, RatioAfter: after, CreatedAt: now,
			}).Error; err != nil {
				return err
			}
			ratioMap[group] = after
		}

		payload, err := json.Marshal(ratioMap)
		if err != nil {
			return err
		}
		encoded = string(payload)
		option.Key = "GroupRatio"
		option.Value = encoded
		return tx.Save(&option).Error
	})
	if err != nil {
		return err
	}
	if err := gatewaystore.UpdateGroupRatioByJSONString(encoded); err != nil {
		return err
	}
	return migrateUnifiedCreditSubscriptionGroupPolicies()
}

func unifiedCreditTargetGroupRatio(group string, current float64) float64 {
	switch strings.TrimSpace(group) {
	case "Plus分组":
		return 0.10
	case "纯Pro号池":
		return 0.16
	default:
		return current * 0.25
	}
}

func migrateUnifiedCreditSubscriptionGroupPolicies() error {
	policyMap := map[string]gatewaystore.SubscriptionGroupPolicy{}
	if err := json.Unmarshal([]byte(gatewaystore.SubscriptionGroupPolicy2JSONString()), &policyMap); err != nil {
		return err
	}

	var encoded string
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var option platformschema.Option
		err := tx.Where("key = ?", gatewaystore.SubscriptionGroupPolicyOptionKey).First(&option).Error
		if err == nil && strings.TrimSpace(option.Value) != "" {
			if err := json.Unmarshal([]byte(option.Value), &policyMap); err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		policyMap["Plus分组"] = gatewaystore.SubscriptionGroupPolicy{Enabled: true, Multiplier: 1}
		policyMap["纯Pro号池"] = gatewaystore.SubscriptionGroupPolicy{Enabled: true, Multiplier: 1.5}
		payload, err := json.Marshal(policyMap)
		if err != nil {
			return err
		}
		encoded = string(payload)
		option.Key = gatewaystore.SubscriptionGroupPolicyOptionKey
		option.Value = encoded
		return tx.Save(&option).Error
	})
	if err != nil {
		return err
	}
	return gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(encoded)
}
