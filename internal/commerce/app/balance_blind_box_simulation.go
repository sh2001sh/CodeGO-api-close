package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxBalanceBlindBoxSimulationMinutes = 7 * 24 * 60

type BalanceBlindBoxSimulationOverview struct {
	Active                  bool    `json:"active"`
	SessionID               int     `json:"session_id,omitempty"`
	ExpiresAt               int64   `json:"expires_at,omitempty"`
	RemainingSeconds        int64   `json:"remaining_seconds,omitempty"`
	DrawCount               int     `json:"draw_count"`
	SimulatedCostUSD        float64 `json:"simulated_cost_usd"`
	SimulatedRewardValueUSD float64 `json:"simulated_reward_value_usd"`
	SimulatedNetUSD         float64 `json:"simulated_net_usd"`
	Reason                  string  `json:"reason,omitempty"`
}

type AdminBalanceBlindBoxSimulationRequest struct {
	DurationMinutes int    `json:"duration_minutes"`
	Reason          string `json:"reason"`
}

type BalanceBlindBoxSimulationResult struct {
	Records    []commerceschema.BlindBoxOpenRecord `json:"records"`
	Simulation BalanceBlindBoxSimulationOverview   `json:"simulation"`
}

func StartBalanceBlindBoxSimulation(userID, adminUserID int, req AdminBalanceBlindBoxSimulationRequest) (*BalanceBlindBoxSimulationOverview, error) {
	req.Reason = strings.TrimSpace(req.Reason)
	if userID <= 0 || adminUserID <= 0 || req.DurationMinutes <= 0 || req.DurationMinutes > maxBalanceBlindBoxSimulationMinutes {
		return nil, fmt.Errorf("模拟时长必须在 1 到 %d 分钟之间", maxBalanceBlindBoxSimulationMinutes)
	}
	var session commerceschema.BalanceBlindBoxSimulationSession
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var user identityschema.User
		if err := tx.Select("id").Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		now := platformruntime.GetTimestamp()
		if err := expireBalanceBlindBoxSimulationSessionsTx(tx, userID, now); err != nil {
			return err
		}
		if err := tx.Model(&commerceschema.BalanceBlindBoxSimulationSession{}).
			Where("user_id = ? AND status = ?", userID, commerceschema.BalanceBlindBoxSimulationStatusActive).
			Updates(map[string]any{"status": commerceschema.BalanceBlindBoxSimulationStatusStopped, "expires_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		var pity commerceschema.BalanceBlindBoxPityState
		pityErr := tx.Where("user_id = ?", userID).First(&pity).Error
		if pityErr != nil && !errors.Is(pityErr, gorm.ErrRecordNotFound) {
			return pityErr
		}
		var issuedCount int64
		if err := tx.Model(&commerceschema.BalanceBlindBoxItem{}).Where("purchase_user_id = ?", userID).Count(&issuedCount).Error; err != nil {
			return err
		}
		session = commerceschema.BalanceBlindBoxSimulationSession{
			UserId: userID, AdminUserId: adminUserID, Reason: req.Reason,
			StartsAt: now, ExpiresAt: now + int64(req.DurationMinutes*60),
			ConsecutiveUnder6USD: pity.ConsecutiveUnder6USD, ConsecutiveUnder35USD: pity.ConsecutiveUnder35USD,
			FirstDrawEligible: issuedCount == 0,
		}
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		return auditapp.RecordLogTx(tx, adminUserID, auditschema.LogTypeManage,
			fmt.Sprintf("为用户 %d 开启余额盲盒模拟，时长 %d 分钟，原因：%s", userID, req.DurationMinutes, req.Reason))
	})
	if err != nil {
		return nil, err
	}
	overview := buildBalanceBlindBoxSimulationOverview(&session, platformruntime.GetTimestamp())
	return &overview, nil
}

func StopBalanceBlindBoxSimulation(userID, adminUserID int) (*BalanceBlindBoxSimulationOverview, error) {
	if userID <= 0 || adminUserID <= 0 {
		return nil, errors.New("invalid balance blind box simulation request")
	}
	now := platformruntime.GetTimestamp()
	var session commerceschema.BalanceBlindBoxSimulationSession
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND status = ?", userID, commerceschema.BalanceBlindBoxSimulationStatusActive).
			Order("id desc").First(&session).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("该用户没有进行中的余额盲盒模拟")
		}
		if err != nil {
			return err
		}
		session.Status, session.ExpiresAt = commerceschema.BalanceBlindBoxSimulationStatusStopped, now
		if err := tx.Save(&session).Error; err != nil {
			return err
		}
		return auditapp.RecordLogTx(tx, adminUserID, auditschema.LogTypeManage,
			fmt.Sprintf("提前结束用户 %d 的余额盲盒模拟，会话 %d", userID, session.Id))
	})
	if err != nil {
		return nil, err
	}
	overview := buildBalanceBlindBoxSimulationOverview(&session, now)
	return &overview, nil
}

func GetBalanceBlindBoxSimulationOverview(userID int) (BalanceBlindBoxSimulationOverview, error) {
	if userID <= 0 {
		return BalanceBlindBoxSimulationOverview{}, nil
	}
	now := platformruntime.GetTimestamp()
	var session commerceschema.BalanceBlindBoxSimulationSession
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := expireBalanceBlindBoxSimulationSessionsTx(tx, userID, now); err != nil {
			return err
		}
		return tx.Where("user_id = ? AND status = ? AND expires_at > ?", userID, commerceschema.BalanceBlindBoxSimulationStatusActive, now).
			Order("id desc").First(&session).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) || isBalanceBlindBoxSchemaMissing(err) {
		return BalanceBlindBoxSimulationOverview{}, nil
	}
	if err != nil {
		return BalanceBlindBoxSimulationOverview{}, err
	}
	return buildBalanceBlindBoxSimulationOverview(&session, now), nil
}

func SimulateBalanceBlindBoxes(userID int, requestID string, count int) (*BalanceBlindBoxSimulationResult, error) {
	requestID = strings.TrimSpace(requestID)
	if userID <= 0 || requestID == "" || len(requestID) > 64 || count <= 0 || count > balanceBlindBoxMaxBatch {
		return nil, errors.New("余额盲盒模拟参数无效，单次最多模拟 100 个")
	}
	setting := blindboxsettings.Get()
	result := &BalanceBlindBoxSimulationResult{}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if replayed, err := loadBalanceBlindBoxSimulationReplayTx(tx, userID, requestID, count); err != nil {
			return err
		} else if replayed != nil {
			*result = *replayed
			return nil
		}
		now := platformruntime.GetTimestamp()
		if err := expireBalanceBlindBoxSimulationSessionsTx(tx, userID, now); err != nil {
			return err
		}
		var session commerceschema.BalanceBlindBoxSimulationSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND status = ? AND expires_at > ?", userID, commerceschema.BalanceBlindBoxSimulationStatusActive, now).
			Order("id desc").First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("余额盲盒模拟权限不存在或已到期")
			}
			return err
		}
		batch := commerceschema.BalanceBlindBoxSimulationBatch{SessionId: session.Id, UserId: userID, RequestId: requestID, Count: count, ResultsJSON: "[]"}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		pity := commerceschema.BalanceBlindBoxPityState{ConsecutiveUnder6USD: session.ConsecutiveUnder6USD, ConsecutiveUnder35USD: session.ConsecutiveUnder35USD}
		records := make([]commerceschema.BlindBoxOpenRecord, 0, count)
		rewardValue := 0.0
		for index := 0; index < count; index++ {
			firstDraw := session.FirstDrawEligible && session.DrawCount == 0 && index == 0
			item := issueSealedBalanceBlindBox(0, userID, setting, &pity, firstDraw)
			drawRequestID := fmt.Sprintf("%s:%d", requestID, index)
			records = append(records, commerceschema.BlindBoxOpenRecord{
				Id: -(batch.Id*1000 + index + 1), UserId: userID, RequestId: &drawRequestID,
				PoolType: commerceschema.BlindBoxPoolTypeBalance15, RewardType: item.RewardType,
				RewardWalletType: item.RewardWalletType, RewardUSD: item.RewardUSD, CreditAmount: item.CreditAmount,
				RewardTitle: item.RewardTitle, RewardTier: item.RewardTier, IsPity: item.IsPity, CreateTime: now, Simulation: true,
			})
			rewardValue += balanceBlindBoxEquivalentValue(item.RewardType, item.RewardUSD)
		}
		encoded, err := json.Marshal(records)
		if err != nil {
			return err
		}
		batch.ResultsJSON = string(encoded)
		if err := tx.Save(&batch).Error; err != nil {
			return err
		}
		session.DrawCount += count
		session.SimulatedCostUSD += setting.BalanceBlindBoxPriceUSD * float64(count)
		session.SimulatedRewardValueUSD += rewardValue
		session.ConsecutiveUnder6USD = pity.ConsecutiveUnder6USD
		session.ConsecutiveUnder35USD = pity.ConsecutiveUnder35USD
		session.FirstDrawEligible = false
		if err := tx.Save(&session).Error; err != nil {
			return err
		}
		result.Records = records
		result.Simulation = buildBalanceBlindBoxSimulationOverview(&session, now)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func loadBalanceBlindBoxSimulationReplayTx(tx *gorm.DB, userID int, requestID string, count int) (*BalanceBlindBoxSimulationResult, error) {
	var batch commerceschema.BalanceBlindBoxSimulationBatch
	err := tx.Where("request_id = ?", requestID).First(&batch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if batch.UserId != userID || batch.Count != count {
		return nil, errors.New("余额盲盒模拟请求冲突")
	}
	var records []commerceschema.BlindBoxOpenRecord
	if err := json.Unmarshal([]byte(batch.ResultsJSON), &records); err != nil {
		return nil, err
	}
	for index := range records {
		drawRequestID := fmt.Sprintf("%s:%d", requestID, index)
		records[index].RequestId = &drawRequestID
	}
	var session commerceschema.BalanceBlindBoxSimulationSession
	if err := tx.First(&session, batch.SessionId).Error; err != nil {
		return nil, err
	}
	now := platformruntime.GetTimestamp()
	return &BalanceBlindBoxSimulationResult{Records: records, Simulation: buildBalanceBlindBoxSimulationOverview(&session, now)}, nil
}

func expireBalanceBlindBoxSimulationSessionsTx(tx *gorm.DB, userID int, now int64) error {
	return tx.Model(&commerceschema.BalanceBlindBoxSimulationSession{}).
		Where("user_id = ? AND status = ? AND expires_at <= ?", userID, commerceschema.BalanceBlindBoxSimulationStatusActive, now).
		Updates(map[string]any{"status": commerceschema.BalanceBlindBoxSimulationStatusExpired, "updated_at": now}).Error
}

func buildBalanceBlindBoxSimulationOverview(session *commerceschema.BalanceBlindBoxSimulationSession, now int64) BalanceBlindBoxSimulationOverview {
	if session == nil {
		return BalanceBlindBoxSimulationOverview{}
	}
	remaining := max(session.ExpiresAt-now, 0)
	active := session.Status == commerceschema.BalanceBlindBoxSimulationStatusActive && remaining > 0
	return BalanceBlindBoxSimulationOverview{
		Active: active, SessionID: session.Id, ExpiresAt: session.ExpiresAt, RemainingSeconds: remaining,
		DrawCount: session.DrawCount, SimulatedCostUSD: session.SimulatedCostUSD,
		SimulatedRewardValueUSD: session.SimulatedRewardValueUSD,
		SimulatedNetUSD:         session.SimulatedRewardValueUSD - session.SimulatedCostUSD, Reason: session.Reason,
	}
}
