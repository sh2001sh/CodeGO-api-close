package app

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MarketReportRequest struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Reason     string `json:"reason"`
	Details    string `json:"details"`
}

type MarketReportList struct {
	Items    []marketplaceschema.MarketReport `json:"items"`
	Total    int64                            `json:"total"`
	Page     int                              `json:"page"`
	PageSize int                              `json:"page_size"`
}

func SubmitMarketReport(userID int, req MarketReportRequest) error {
	if userID <= 0 {
		return errors.New("请先登录后举报")
	}
	req.TargetID = strings.TrimSpace(req.TargetID)
	req.Details = strings.TrimSpace(req.Details)
	if utf8.RuneCountInString(req.Details) > 500 {
		return errors.New("举报说明不能超过 500 字")
	}
	switch req.Reason {
	case "advertising", "fraud", "inappropriate", "other":
	default:
		return errors.New("请选择有效的举报原因")
	}
	query := publicGroupsQuery(GroupQuery{})
	switch req.TargetType {
	case "channel":
		query = query.Where("channel_id = ?", req.TargetID)
	case "shop":
		id, err := strconv.Atoi(req.TargetID)
		if err != nil || id <= 0 {
			return errors.New("店铺不存在")
		}
		query = query.Where("owner_user_id = ?", id)
	default:
		return errors.New("举报对象无效")
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return errors.New("举报对象不存在或未公开")
	}
	var evidence any
	if req.TargetType == "shop" {
		var profile marketplaceschema.Shop
		if err := platformdb.DB.Where("owner_user_id = ?", req.TargetID).First(&profile).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		evidence = map[string]any{"name": profile.Name, "description": profile.Description}
	} else {
		var group marketplaceschema.Group
		if err := platformdb.DB.Where("channel_id = ?", req.TargetID).First(&group).Error; err != nil {
			return err
		}
		evidence = map[string]any{"name": group.CustomName, "description": group.Description, "tags": decodeModels(group.Tags)}
	}
	encodedEvidence, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	report := marketplaceschema.MarketReport{ReporterID: userID, TargetType: req.TargetType, TargetID: req.TargetID, Reason: req.Reason, Details: req.Details, Status: "pending"}
	report.Evidence = string(encodedEvidence)
	result := platformdb.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&report)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("你已举报过该对象，请等待管理员处理")
	}
	return nil
}

func ListMarketReports(status string, page, pageSize int) (*MarketReportList, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query := platformdb.DB.Model(&marketplaceschema.MarketReport{})
	if status != "" {
		if status != "pending" && status != "dismissed" && status != "resolved" {
			return nil, errors.New("举报状态无效")
		}
		query = query.Where("status = ?", status)
	}
	result := &MarketReportList{Items: []marketplaceschema.MarketReport{}, Page: page, PageSize: pageSize}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&result.Items).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func ResolveMarketReport(adminID int, id uint64, action, note string) error {
	if adminID <= 0 || id == 0 {
		return errors.New("举报处理参数无效")
	}
	if action != "dismiss" && action != "remove_content" {
		return errors.New("举报处理操作无效")
	}
	note = strings.TrimSpace(note)
	if note == "" || utf8.RuneCountInString(note) > 500 {
		return errors.New("请填写 1 到 500 字的处理说明")
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var report marketplaceschema.MarketReport
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&report, id).Error; err != nil {
			return err
		}
		if report.Status != "pending" {
			return errors.New("该举报已处理")
		}
		status := "dismissed"
		if action == "remove_content" {
			status = "resolved"
			if report.TargetType == "shop" {
				if err := tx.Model(&marketplaceschema.Shop{}).Where("owner_user_id = ?", report.TargetID).Updates(map[string]any{"name": "", "description": "", "moderation_status": "passed"}).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Model(&marketplaceschema.Group{}).Where("channel_id = ?", report.TargetID).Updates(map[string]any{"custom_name": "", "description": "", "tags": "[]", "moderation_status": "passed", "moderation_reason": note, "moderation_checked_at": time.Now().UTC()}).Error; err != nil {
					return err
				}
			}
		}
		return tx.Model(&report).Updates(map[string]any{"status": status, "resolution": note, "reviewed_by": adminID}).Error
	})
}
