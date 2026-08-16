package app

import (
	"errors"
	"fmt"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"strings"
)

// ListUserBlindBoxProps returns the user's blind-box props after expiring overdue active props.
func ListUserBlindBoxProps(userID int) ([]commerceschema.BlindBoxProp, error) {
	if userID <= 0 {
		return []commerceschema.BlindBoxProp{}, nil
	}
	var props []commerceschema.BlindBoxProp
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		props, err = listUserBlindBoxPropsTx(tx, userID)
		return err
	})
	return props, err
}

// ActivateBlindBoxProp activates a manually usable blind-box prop for the user.
func ActivateBlindBoxProp(userID int, propID int) (*commerceschema.BlindBoxProp, error) {
	if userID <= 0 || propID <= 0 {
		return nil, errors.New("invalid blind box prop request")
	}
	var prop commerceschema.BlindBoxProp
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := platformruntime.GetTimestamp()
		if err := expireUserBlindBoxPropsTx(tx, userID, now); err != nil {
			return err
		}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ? AND user_id = ?", propID, userID).First(&prop).Error; err != nil {
			return err
		}
		if err := expireBlindBoxPropIfNeededTx(tx, &prop, now); err != nil {
			return err
		}
		spec, ok := getBlindBoxPropSpecByType(prop.PropType)
		if !ok || !spec.Activatable {
			return errors.New("this prop is applied automatically")
		}
		canResume := isPausableBlindBoxMultiplierProp(prop.PropType) && prop.Status == commerceschema.BlindBoxPropStatusPaused
		if prop.Status != commerceschema.BlindBoxPropStatusAvailable && !canResume {
			return errors.New("this prop cannot be activated")
		}
		if prop.PropType == commerceschema.BlindBoxPropTypeZeroHourMultiplier && hasActiveZeroHourPropTx(tx, userID) {
			return errors.New("zero-hour prop is already active")
		}
		if prop.PropType == commerceschema.BlindBoxPropTypeMonthlyPassMultiplier && hasActiveMonthlyPassPropTx(tx, userID) {
			return errors.New("0.1 倍率卡已经生效")
		}
		if isPausableBlindBoxMultiplierProp(prop.PropType) {
			if prop.RemainingSeconds <= 0 {
				prop.RemainingSeconds = prop.DurationSeconds
			}
			if prop.RemainingSeconds <= 0 {
				return errors.New("倍率卡剩余时间不足")
			}
		}
		prop.Status = commerceschema.BlindBoxPropStatusActive
		prop.ActivatedAt = now
		if isPausableBlindBoxMultiplierProp(prop.PropType) {
			prop.ExpiresAt = now + prop.RemainingSeconds
		} else {
			prop.ExpiresAt = now + prop.DurationSeconds
		}
		return tx.Save(&prop).Error
	})
	if err != nil {
		return nil, err
	}
	return &prop, nil
}

// PauseBlindBoxProp pauses a pausable multiplier card and preserves unused time.
func PauseBlindBoxProp(userID int, propID int) (*commerceschema.BlindBoxProp, error) {
	if userID <= 0 || propID <= 0 {
		return nil, errors.New("invalid blind box prop request")
	}
	var prop commerceschema.BlindBoxProp
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := platformruntime.GetTimestamp()
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ? AND user_id = ?", propID, userID).First(&prop).Error; err != nil {
			return err
		}
		if !isPausableBlindBoxMultiplierProp(prop.PropType) {
			return errors.New("this prop cannot be paused")
		}
		if err := expireBlindBoxPropIfNeededTx(tx, &prop, now); err != nil {
			return err
		}
		if prop.Status != commerceschema.BlindBoxPropStatusActive {
			return errors.New("this prop is not active")
		}
		prop.RemainingSeconds = max(prop.ExpiresAt-now, 0)
		if prop.RemainingSeconds == 0 {
			prop.Status = commerceschema.BlindBoxPropStatusExpired
			prop.ExpiresAt = 0
			return tx.Save(&prop).Error
		}
		prop.Status = commerceschema.BlindBoxPropStatusPaused
		prop.ExpiresAt = 0
		return tx.Save(&prop).Error
	})
	if err != nil {
		return nil, err
	}
	return &prop, nil
}

func isPausableBlindBoxMultiplierProp(propType string) bool {
	return propType == commerceschema.BlindBoxPropTypeZeroHourMultiplier ||
		propType == commerceschema.BlindBoxPropTypeMonthlyPassMultiplier
}

// ConvertBlindBoxDiscountProp converts an unused 10% top-up discount card
// into a subscription discount card, or vice versa, without consuming it.
func ConvertBlindBoxDiscountProp(userID int, propID int, targetType string) (*commerceschema.BlindBoxProp, error) {
	if userID <= 0 || propID <= 0 {
		return nil, errors.New("invalid blind box prop request")
	}
	targetSpec, ok := getBlindBoxPropSpecByType(targetType)
	if !ok || !isConvertibleBlindBoxDiscountPropType(targetSpec.PropType) {
		return nil, errors.New("unsupported blind box prop conversion")
	}
	var prop commerceschema.BlindBoxProp
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ? AND user_id = ?", propID, userID).First(&prop).Error; err != nil {
			return err
		}
		if !isConvertibleBlindBoxDiscountPropType(prop.PropType) {
			return errors.New("this prop cannot be converted")
		}
		if prop.Status != commerceschema.BlindBoxPropStatusAvailable {
			return errors.New("only available discount cards can be converted")
		}
		if prop.PropType == targetSpec.PropType {
			return errors.New("discount card already has the requested type")
		}
		originalTitle := prop.Title
		prop.PropType = targetSpec.PropType
		prop.Title = targetSpec.Title
		prop.DiscountRate = targetSpec.DiscountRate
		prop.Multiplier = targetSpec.Multiplier
		prop.DurationSeconds = targetSpec.DurationSeconds
		if err := tx.Save(&prop).Error; err != nil {
			return err
		}
		return auditapp.RecordLogTx(
			tx,
			userID,
			auditschema.LogTypeManage,
			fmt.Sprintf("盲盒九折卡转换，道具ID：%d，原类型：%s，目标类型：%s", prop.Id, originalTitle, prop.Title),
		)
	})
	if err != nil {
		return nil, err
	}
	return &prop, nil
}

func isConvertibleBlindBoxDiscountPropType(propType string) bool {
	return propType == commerceschema.BlindBoxPropTypeTopupDiscount90 ||
		propType == commerceschema.BlindBoxPropTypeSubscriptionDiscount90
}

func GetUserBlindBoxConsumptionDiscountRate(userID int) float64 {
	if userID <= 0 {
		return 0
	}
	now := platformruntime.GetTimestamp()
	props := []commerceschema.BlindBoxProp{}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := expireUserBlindBoxPropsTx(tx, userID, now); err != nil {
			return err
		}
		return tx.Where("user_id = ? AND status = ? AND prop_type IN ?", userID, commerceschema.BlindBoxPropStatusActive, []string{
			commerceschema.BlindBoxPropTypeConsumeDiscount95,
			commerceschema.BlindBoxPropTypeConsumeDiscount90,
		}).Find(&props).Error
	})
	if err != nil {
		return 0
	}
	bestRate := 0.0
	for _, prop := range props {
		if prop.DiscountRate > bestRate {
			bestRate = prop.DiscountRate
		}
	}
	return bestRate
}

// RequiresOfficialBlindBoxChannel reports whether an active multiplier card
// should constrain automatic routing to official channels.
func RequiresOfficialBlindBoxChannel(userID int) bool {
	if userID <= 0 {
		return false
	}
	var active bool
	_ = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := platformruntime.GetTimestamp()
		if err := expireUserBlindBoxPropsTx(tx, userID, now); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&commerceschema.BlindBoxProp{}).
			Where("user_id = ? AND status = ? AND prop_type IN ? AND (expires_at = 0 OR expires_at > ?)", userID, commerceschema.BlindBoxPropStatusActive, []string{
				commerceschema.BlindBoxPropTypeConsumeDiscount95,
				commerceschema.BlindBoxPropTypeConsumeDiscount90,
				commerceschema.BlindBoxPropTypeZeroHourMultiplier,
				commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
			}, now).
			Count(&count).Error; err != nil {
			return err
		}
		active = count > 0
		return nil
	})
	return active
}

func GetUserBlindBoxTopupDiscountRate(userID int) float64 {
	if userID <= 0 {
		return 0
	}
	return getAvailableBlindBoxPropDiscountRateTx(platformdb.DB, userID, commerceschema.BlindBoxPropTypeTopupDiscount90)
}

func GetUserBlindBoxSubscriptionDiscountRate(userID int) float64 {
	if userID <= 0 {
		return 0
	}
	return getAvailableBlindBoxPropDiscountRateTx(platformdb.DB, userID, commerceschema.BlindBoxPropTypeSubscriptionDiscount90)
}

func ReserveBlindBoxTopupDiscountPropTx(tx *gorm.DB, userID int, tradeNo string) (*commerceschema.BlindBoxProp, error) {
	return reserveBlindBoxDiscountPropTx(tx, userID, tradeNo, commerceschema.BlindBoxPropTypeTopupDiscount90, commerceschema.BlindBoxPropOrderTypeTopup)
}

func ReserveBlindBoxSubscriptionDiscountPropTx(tx *gorm.DB, userID int, tradeNo string) (*commerceschema.BlindBoxProp, error) {
	return reserveBlindBoxDiscountPropTx(tx, userID, tradeNo, commerceschema.BlindBoxPropTypeSubscriptionDiscount90, commerceschema.BlindBoxPropOrderTypeSubscription)
}

func ReleaseReservedBlindBoxPropByTradeNoTx(tx *gorm.DB, tradeNo string, orderType string) error {
	if tx == nil || strings.TrimSpace(tradeNo) == "" || strings.TrimSpace(orderType) == "" {
		return nil
	}
	return tx.Model(&commerceschema.BlindBoxProp{}).
		Where("reserved_order_trade_no = ? AND reserved_order_type = ? AND status = ?", tradeNo, orderType, commerceschema.BlindBoxPropStatusReserved).
		Updates(map[string]any{
			"status":                  commerceschema.BlindBoxPropStatusAvailable,
			"reserved_at":             int64(0),
			"reserved_order_type":     "",
			"reserved_order_trade_no": "",
			"updated_at":              platformruntime.GetTimestamp(),
		}).Error
}

func ConsumeReservedBlindBoxPropByTradeNoTx(tx *gorm.DB, tradeNo string, orderType string) error {
	if tx == nil || strings.TrimSpace(tradeNo) == "" || strings.TrimSpace(orderType) == "" {
		return nil
	}
	return tx.Model(&commerceschema.BlindBoxProp{}).
		Where("reserved_order_trade_no = ? AND reserved_order_type = ? AND status = ?", tradeNo, orderType, commerceschema.BlindBoxPropStatusReserved).
		Updates(map[string]any{
			"status":                  commerceschema.BlindBoxPropStatusUsed,
			"used_at":                 platformruntime.GetTimestamp(),
			"reserved_order_type":     "",
			"reserved_order_trade_no": "",
			"updated_at":              platformruntime.GetTimestamp(),
		}).Error
}

func listUserBlindBoxPropsTx(tx *gorm.DB, userID int) ([]commerceschema.BlindBoxProp, error) {
	now := platformruntime.GetTimestamp()
	if err := expireUserBlindBoxPropsTx(tx, userID, now); err != nil {
		return nil, err
	}
	var props []commerceschema.BlindBoxProp
	err := tx.Where("user_id = ?", userID).Order("created_at desc, id desc").Find(&props).Error
	return props, err
}

func expireBlindBoxPropIfNeededTx(tx *gorm.DB, prop *commerceschema.BlindBoxProp, now int64) error {
	if tx == nil || prop == nil || prop.Status != commerceschema.BlindBoxPropStatusActive {
		return nil
	}
	if prop.ExpiresAt <= 0 || prop.ExpiresAt > now {
		return nil
	}
	prop.Status = commerceschema.BlindBoxPropStatusExpired
	if isPausableBlindBoxMultiplierProp(prop.PropType) {
		prop.RemainingSeconds = 0
	}
	return tx.Save(prop).Error
}

func expireUserBlindBoxPropsTx(tx *gorm.DB, userID int, now int64) error {
	if tx == nil || userID <= 0 {
		return nil
	}
	return tx.Model(&commerceschema.BlindBoxProp{}).
		Where("user_id = ? AND status = ? AND expires_at > 0 AND expires_at <= ?", userID, commerceschema.BlindBoxPropStatusActive, now).
		Updates(map[string]any{
			"status":            commerceschema.BlindBoxPropStatusExpired,
			"remaining_seconds": int64(0),
			"updated_at":        now,
		}).Error
}

func createBlindBoxPropTx(tx *gorm.DB, userID int, openRecordID int, rewardTitle string) (*commerceschema.BlindBoxProp, error) {
	spec, ok := getBlindBoxPropSpecByTitle(rewardTitle)
	if !ok {
		return nil, errors.New("unsupported blind box prop reward")
	}
	prop := &commerceschema.BlindBoxProp{
		UserId:           userID,
		OpenRecordId:     openRecordID,
		PropType:         spec.PropType,
		Title:            spec.Title,
		Status:           commerceschema.BlindBoxPropStatusAvailable,
		DiscountRate:     spec.DiscountRate,
		Multiplier:       spec.Multiplier,
		DurationSeconds:  spec.DurationSeconds,
		MaxDiscountQuota: spec.MaxDiscountQuota,
	}
	if err := tx.Create(prop).Error; err != nil {
		return nil, err
	}
	return prop, nil
}

func getAvailableBlindBoxPropDiscountRateTx(tx *gorm.DB, userID int, propType string) float64 {
	if tx == nil || userID <= 0 || strings.TrimSpace(propType) == "" {
		return 0
	}
	var prop commerceschema.BlindBoxProp
	err := tx.Where("user_id = ? AND prop_type = ? AND status = ?", userID, propType, commerceschema.BlindBoxPropStatusAvailable).
		Order("created_at asc, id asc").
		First(&prop).Error
	if err != nil {
		return 0
	}
	return prop.DiscountRate
}

func getAvailableBlindBoxPropByTypeTx(tx *gorm.DB, userID int, propType string) (*commerceschema.BlindBoxProp, error) {
	var prop commerceschema.BlindBoxProp
	if err := tx.Set("gorm:query_option", "FOR UPDATE").
		Where("user_id = ? AND prop_type = ? AND status = ?", userID, propType, commerceschema.BlindBoxPropStatusAvailable).
		Order("created_at asc, id asc").
		First(&prop).Error; err != nil {
		return nil, err
	}
	return &prop, nil
}

func reserveBlindBoxDiscountPropTx(tx *gorm.DB, userID int, tradeNo string, propType string, orderType string) (*commerceschema.BlindBoxProp, error) {
	if tx == nil || userID <= 0 || strings.TrimSpace(tradeNo) == "" {
		return nil, nil
	}
	prop, err := getAvailableBlindBoxPropByTypeTx(tx, userID, propType)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	now := platformruntime.GetTimestamp()
	prop.Status = commerceschema.BlindBoxPropStatusReserved
	prop.ReservedAt = now
	prop.ReservedOrderType = orderType
	prop.ReservedOrderTradeNo = tradeNo
	if err := tx.Save(prop).Error; err != nil {
		return nil, err
	}
	return prop, nil
}

func getBlindBoxPropSpecByTitle(title string) (commerceschema.BlindBoxPropSpec, bool) {
	trimmedTitle := strings.TrimSpace(title)
	for _, spec := range blindBoxPropSpecs() {
		if spec.Title == trimmedTitle {
			return spec, true
		}
	}
	return commerceschema.BlindBoxPropSpec{}, false
}

func getBlindBoxPropSpecByType(propType string) (commerceschema.BlindBoxPropSpec, bool) {
	trimmedType := strings.TrimSpace(propType)
	for _, spec := range blindBoxPropSpecs() {
		if spec.PropType == trimmedType {
			return spec, true
		}
	}
	return commerceschema.BlindBoxPropSpec{}, false
}

func blindBoxPropSpecs() []commerceschema.BlindBoxPropSpec {
	return []commerceschema.BlindBoxPropSpec{
		{
			PropType:     commerceschema.BlindBoxPropTypeTopupDiscount90,
			Title:        "充值九折卡",
			DiscountRate: 0.10,
			Multiplier:   1,
		},
		{
			PropType:     commerceschema.BlindBoxPropTypeSubscriptionDiscount90,
			Title:        "套餐九折卡",
			DiscountRate: 0.10,
			Multiplier:   1,
		},
		{
			PropType:        commerceschema.BlindBoxPropTypeConsumeDiscount95,
			Title:           "0.95 倍率卡",
			DiscountRate:    0.05,
			Multiplier:      0.95,
			DurationSeconds: 24 * 60 * 60,
			Activatable:     true,
		},
		{
			PropType:        commerceschema.BlindBoxPropTypeConsumeDiscount90,
			Title:           "0.9 倍率卡",
			DiscountRate:    0.10,
			Multiplier:      0.90,
			DurationSeconds: 24 * 60 * 60,
			Activatable:     true,
		},
		{
			PropType:        commerceschema.BlindBoxPropTypeZeroHourMultiplier,
			Title:           "1 小时 0 倍率卡",
			Multiplier:      0,
			DurationSeconds: zeroHourDurationSeconds,
			Activatable:     true,
		},
		{
			PropType:        commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
			Title:           "0.10 倍率体验卡",
			Multiplier:      0.1,
			DurationSeconds: 15 * 60,
			Activatable:     true,
		},
	}
}
