package store

import (
	"errors"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
)

func ListUsers(pageInfo *platformpagination.PageInfo) ([]*identityschema.User, int64, error) {
	tx := platformdb.DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var (
		users []*identityschema.User
		total int64
	)
	if err := tx.Unscoped().Model(&identityschema.User{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err := tx.Unscoped().
		Order("id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Omit("password").
		Find(&users).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err := attachCurrentSubscriptionSummary(tx, users); err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func SearchUsers(keyword string, group string, startIdx int, num int) ([]*identityschema.User, int64, error) {
	keyword = strings.TrimSpace(keyword)
	tx := platformdb.DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var (
		users []*identityschema.User
		total int64
	)
	query := tx.Unscoped().Model(&identityschema.User{})
	pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(keyword)) + "%"
	likeCondition := "LOWER(external_id) LIKE ? ESCAPE '!' OR LOWER(username) LIKE ? ESCAPE '!' OR LOWER(email) LIKE ? ESCAPE '!' OR LOWER(display_name) LIKE ? ESCAPE '!'"
	args := []any{pattern, pattern, pattern, pattern}
	order := clause.OrderBy{Expression: clause.Expr{
		SQL: "CASE WHEN LOWER(external_id) = ? THEN 0 ELSE 1 END, id DESC", Vars: []any{strings.ToLower(keyword)},
	}}
	if keywordInt, err := strconv.Atoi(keyword); err == nil {
		likeCondition = "id = ? OR " + likeCondition
		args = append([]any{keywordInt}, args...)
		order.Expression = clause.Expr{SQL: "CASE WHEN id = ? OR LOWER(external_id) = ? THEN 0 ELSE 1 END, id DESC", Vars: []any{keywordInt, strings.ToLower(keyword)}}
	}
	query = query.Where("("+likeCondition+")", args...)
	if group != "" {
		query = query.Where(userGroupColumn()+" = ?", group)
	}

	// A narrow window query evaluates substring filters once. Fetching the page
	// separately keeps large identity records out of PostgreSQL's window buffer.
	if platformdb.UsingPostgreSQL && keyword != "" {
		var matches []struct {
			ID    int
			Total int64
		}
		if err := query.Session(&gorm.Session{}).Select("id, COUNT(*) OVER() AS total").Order(order).Limit(num).Offset(startIdx).Scan(&matches).Error; err != nil {
			tx.Rollback()
			return nil, 0, err
		}
		if len(matches) > 0 {
			total = matches[0].Total
			ids := make([]int, len(matches))
			for i, match := range matches {
				ids[i] = match.ID
			}
			query = tx.Unscoped().Model(&identityschema.User{}).Where("id IN ?", ids)
			startIdx = 0
		} else {
			if startIdx > 0 || num == 0 {
				if err := query.Count(&total).Error; err != nil {
					tx.Rollback()
					return nil, 0, err
				}
			}
			if err := tx.Commit().Error; err != nil {
				return nil, 0, err
			}
			return []*identityschema.User{}, total, nil
		}
	} else if err := query.Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err := query.Omit("password").Order(order).Limit(num).Offset(startIdx).Find(&users).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err := attachCurrentSubscriptionSummary(tx, users); err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func HardDeleteUserByID(userID int) error {
	if userID == 0 {
		return errors.New("id 为空！")
	}
	return platformdb.DB.Unscoped().Delete(&identityschema.User{}, "id = ?", userID).Error
}

func TouchUserLastLoginAt(userID int) {
	if err := platformdb.DB.Model(&identityschema.User{}).Where("id = ?", userID).Update("last_login_at", platformruntime.GetTimestamp()).Error; err != nil {
		platformobservability.SysLog("failed to update user last_login_at: " + err.Error())
	}
}

type userSubscriptionSummaryRow struct {
	UserId    int
	Status    string
	EndTime   int64
	PlanTitle string
}

func attachCurrentSubscriptionSummary(tx *gorm.DB, users []*identityschema.User) error {
	if len(users) == 0 {
		return nil
	}

	userIDs := make([]int, 0, len(users))
	for _, user := range users {
		if user == nil {
			continue
		}
		userIDs = append(userIDs, user.Id)
		user.CurrentSubscriptionStatus = "none"
		user.CurrentSubscriptionPlanTitle = ""
		user.CurrentSubscriptionEndTime = 0
	}
	if len(userIDs) == 0 {
		return nil
	}

	now := platformruntime.GetTimestamp()
	rows := make([]userSubscriptionSummaryRow, 0, len(userIDs))
	if err := tx.Table("user_subscriptions AS us").
		Select("us.user_id, us.status, us.end_time, COALESCE(sp.title, '') AS plan_title").
		Joins("LEFT JOIN subscription_plans AS sp ON sp.id = us.plan_id").
		Where("us.user_id IN ? AND us.status = ? AND us.end_time > ?", userIDs, "active", now).
		Order("us.user_id ASC").
		Order("CASE WHEN sp.duration_unit = 'day' THEN 0 ELSE 1 END ASC").
		Order("us.end_time ASC").
		Order("us.id ASC").
		Scan(&rows).Error; err != nil {
		return err
	}

	summaryMap := make(map[int]userSubscriptionSummaryRow, len(rows))
	for _, row := range rows {
		if _, exists := summaryMap[row.UserId]; exists {
			continue
		}
		summaryMap[row.UserId] = row
	}

	for _, user := range users {
		if user == nil {
			continue
		}
		if row, ok := summaryMap[user.Id]; ok {
			user.CurrentSubscriptionStatus = row.Status
			user.CurrentSubscriptionPlanTitle = row.PlanTitle
			user.CurrentSubscriptionEndTime = row.EndTime
		}
	}
	return nil
}

func userGroupColumn() string {
	if platformdb.UsingPostgreSQL {
		return `"group"`
	}
	return "`group`"
}
