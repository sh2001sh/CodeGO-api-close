package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	bounty "github.com/sh2001sh/new-api/internal/bounty/domain"
	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTaskNotFound        = errors.New("bounty task not found")
	ErrApplicationFound    = errors.New("application already exists")
	ErrForbidden           = errors.New("you do not have permission to perform this bounty action")
	ErrInvalidState        = errors.New("bounty task is not in a valid state for this action")
	ErrIdempotencyConflict = errors.New("bounty idempotency key conflict")
)

func parseDeadline(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		parsed, err = time.ParseInLocation("2006-01-02T15:04", strings.TrimSpace(value), time.Local)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("deadline_at must be an RFC3339 timestamp")
	}
	if !parsed.After(time.Now()) {
		return time.Time{}, fmt.Errorf("deadline_at must be later than now")
	}
	return parsed, nil
}

func parseOptionalTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		parsed, err = time.ParseInLocation("2006-01-02T15:04", strings.TrimSpace(value), time.Local)
	}
	if err != nil {
		return nil, fmt.Errorf("timestamp must be an RFC3339 timestamp")
	}
	return &parsed, nil
}

func normalizeTags(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, minInt(len(values), 12))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > 32 {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) == 12 {
			break
		}
	}
	return result
}

func tagsText(values []string) string {
	return strings.Join(normalizeTags(values), ",")
}

func parseTags(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	return normalizeTags(parts)
}

func effectImagesText(values []string) string {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			clean = append(clean, value)
		}
	}
	return strings.Join(clean, "\n")
}

func parseEffectImages(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	return strings.FieldsFunc(value, func(r rune) bool { return r == '\n' || r == ',' })
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func actorRole(role int) string {
	switch {
	case role >= constant.RoleRootUser:
		return "root"
	case role >= constant.RoleAdminUser:
		return "admin"
	default:
		return "user"
	}
}

func lockTaskTx(tx *gorm.DB, taskID string) (*bountyschema.BountyTask, error) {
	var task bountyschema.BountyTask
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return &task, nil
}

func transitionTaskTx(tx *gorm.DB, task *bountyschema.BountyTask, next string, updates map[string]any) error {
	if task == nil {
		return ErrTaskNotFound
	}
	if err := bounty.RequireTransition(task.Status, next); err != nil {
		return err
	}
	if updates == nil {
		updates = make(map[string]any)
	}
	updates["status"] = next
	if err := tx.Model(task).Updates(updates).Error; err != nil {
		return err
	}
	task.Status = next
	return nil
}

func loadUserTx(tx *gorm.DB, userID int64) (*identityschema.User, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user id")
	}
	var user identityschema.User
	if err := tx.Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func userView(user *identityschema.User) UserView {
	if user == nil {
		return UserView{}
	}
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = user.Username
	}
	return UserView{ID: int64(user.Id), Username: user.Username, DisplayName: name}
}

func loadUserViewsTx(tx *gorm.DB, ids []int64) (map[int64]UserView, error) {
	result := make(map[int64]UserView, len(ids))
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return result, nil
	}
	var users []identityschema.User
	if err := tx.Where("id IN ?", unique).Find(&users).Error; err != nil {
		return nil, err
	}
	for index := range users {
		result[int64(users[index].Id)] = userView(&users[index])
	}
	return result, nil
}

func ensureUserAccountTx(tx *gorm.DB, userID int64, walletType string) (*billingschema.BillingAccount, error) {
	accountType, err := bounty.NormalizeWalletType(walletType)
	if err != nil {
		return nil, err
	}
	column := "quota"
	if accountType == bounty.WalletTypeClaude {
		column = "claude_quota"
	}
	var legacyBalance int64
	if err := tx.Model(&identityschema.User{}).Where("id = ?", userID).Select(column).Scan(&legacyBalance).Error; err != nil {
		return nil, err
	}
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
		AccountType: accountType,
		OwnerType:   "user",
		OwnerID:     userID,
		QuotaUnit:   "quota",
	})
	if err != nil {
		return nil, err
	}
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
		return nil, err
	}
	if snapshot.AvailableBalance == 0 && snapshot.ReservedBalance == 0 && snapshot.ConsumedTotal == 0 && snapshot.GrantedTotal == 0 && legacyBalance > 0 {
		_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
			AccountID:      account.AccountID,
			Amount:         legacyBalance,
			IdempotencyKey: fmt.Sprintf("bounty:mirror-bootstrap:%d:%s", userID, accountType),
			ReasonCode:     "mirror_bootstrap",
			ReferenceType:  "user",
			ReferenceID:    strconv.FormatInt(userID, 10),
			OperatorType:   "bounty",
		})
		if err != nil {
			return nil, err
		}
	}
	return account, nil
}

func loadBalanceTx(tx *gorm.DB, userID int64, walletType string) (BalanceView, error) {
	accountType, err := bounty.NormalizeWalletType(walletType)
	if err != nil {
		return BalanceView{}, err
	}
	var account billingschema.BillingAccount
	if err := tx.Where("owner_type = ? AND owner_id = ? AND account_type = ? AND quota_unit = ?", "user", userID, accountType, "quota").First(&account).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return BalanceView{}, err
		}
		column := "quota"
		if accountType == bounty.WalletTypeClaude {
			column = "claude_quota"
		}
		var balance int64
		if err := tx.Model(&identityschema.User{}).Where("id = ?", userID).Select(column).Scan(&balance).Error; err != nil {
			return BalanceView{}, err
		}
		return BalanceView{WalletType: accountType, AvailableBalance: balance}, nil
	}
	var snapshot billingschema.BillingBalanceSnapshot
	if err := tx.Where("account_id = ?", account.AccountID).First(&snapshot).Error; err != nil {
		return BalanceView{}, err
	}
	return BalanceView{WalletType: accountType, AvailableBalance: snapshot.AvailableBalance, ReservedBalance: snapshot.ReservedBalance}, nil
}

func recordEventTx(tx *gorm.DB, taskID string, eventType string, actorID int64, role int, payload any) (*bountyschema.BountyEvent, error) {
	serialized, err := platformencoding.Marshal(payload)
	if err != nil {
		return nil, err
	}
	event := &bountyschema.BountyEvent{TaskID: taskID, EventType: eventType, ActorUserID: actorID, ActorRole: actorRole(role), PayloadText: string(serialized)}
	if err := tx.Create(event).Error; err != nil {
		return nil, err
	}
	return event, nil
}

func createNotificationTx(tx *gorm.DB, userID int64, taskID string, eventID string, notificationType string, title string, content string) error {
	if userID <= 0 {
		return nil
	}
	return tx.Create(&bountyschema.BountyNotification{UserID: userID, TaskID: taskID, EventID: eventID, Type: notificationType, Title: title, Content: content}).Error
}

func createNotificationsTx(tx *gorm.DB, users []int64, taskID string, event *bountyschema.BountyEvent, notificationType string, title string, content string) error {
	seen := make(map[int64]struct{}, len(users))
	for _, userID := range users {
		if _, exists := seen[userID]; exists || userID <= 0 {
			continue
		}
		seen[userID] = struct{}{}
		if err := createNotificationTx(tx, userID, taskID, event.EventID, notificationType, title, content); err != nil {
			return err
		}
	}
	return nil
}

func taskRewardAccountType(walletType string) string {
	if walletType == bounty.WalletTypeClaude {
		return bounty.WalletTypeClaude
	}
	return bounty.WalletTypeDefault
}
