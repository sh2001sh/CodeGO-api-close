package app

import (
	"context"
	"encoding/json"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func startGPT56MappingRun(channelID, level, trigger, parentRunID string) (*marketplaceschema.GPT56MappingRun, error) {
	return startGPT56MappingRunContext(context.Background(), channelID, level, trigger, parentRunID)
}

func startGPT56MappingRunContext(
	ctx context.Context,
	channelID, level, trigger, parentRunID string,
) (*marketplaceschema.GPT56MappingRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	run := &marketplaceschema.GPT56MappingRun{
		ChannelID: channelID, ParentRunID: parentRunID, Level: level, Trigger: trigger,
		Status: GPT56MappingStatusRunning, Results: "[]", StartedAt: now,
	}
	err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tx.Create(run).Error; err != nil {
			return err
		}
		result := tx.Model(&marketplaceschema.Channel{}).
			Where("id = ? AND COALESCE(gpt56_mapping_status, '') <> ?", channelID, GPT56MappingStatusPaused).
			Updates(map[string]any{
				"gpt56_mapping_results": "[]", "gpt56_mapping_status": GPT56MappingStatusRunning,
				"gpt56_mapping_level": level, "gpt56_mapping_trigger": trigger,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errVerificationNotRunning
		}
		return nil
	})
	return run, err
}

func saveGPT56MappingProgress(runID, channelID string, results []GPT56MappingResult) error {
	encoded, err := json.Marshal(results)
	if err != nil {
		return err
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&marketplaceschema.GPT56MappingRun{}).
			Where("id = ? AND status = ?", runID, GPT56MappingStatusRunning).
			Update("results", string(encoded))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errVerificationNotRunning
		}
		channelResult := tx.Model(&marketplaceschema.Channel{}).
			Where("id = ? AND gpt56_mapping_status = ?", channelID, GPT56MappingStatusRunning).
			Updates(map[string]any{
				"gpt56_mapping_results": string(encoded), "gpt56_mapping_status": GPT56MappingStatusRunning,
			})
		if channelResult.Error != nil {
			return channelResult.Error
		}
		if channelResult.RowsAffected == 0 {
			return errVerificationNotRunning
		}
		return nil
	})
}

func finishGPT56MappingRun(
	run *marketplaceschema.GPT56MappingRun,
	results []GPT56MappingResult,
	status string,
) error {
	encoded, err := json.Marshal(results)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(run).Where("status = ?", GPT56MappingStatusRunning).Updates(map[string]any{
			"results": string(encoded), "status": status, "completed_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errVerificationNotRunning
		}
		channelResult := tx.Model(&marketplaceschema.Channel{}).
			Where("id = ? AND gpt56_mapping_status = ?", run.ChannelID, GPT56MappingStatusRunning).
			Updates(map[string]any{
				"gpt56_mapping_results": string(encoded), "gpt56_mapping_status": status,
				"gpt56_mapping_checked_at": now, "gpt56_mapping_level": run.Level,
				"gpt56_mapping_trigger": run.Trigger,
			})
		if channelResult.Error != nil {
			return channelResult.Error
		}
		if channelResult.RowsAffected == 0 {
			return errVerificationNotRunning
		}
		return nil
	})
}

func latestGPT56MappingRuns(channelID string, limit int) []GPT56MappingRunView {
	if platformdb.DB == nil || limit <= 0 {
		return []GPT56MappingRunView{}
	}
	var runs []marketplaceschema.GPT56MappingRun
	if err := platformdb.DB.Where("channel_id = ?", channelID).
		Order("started_at desc").Limit(limit).Find(&runs).Error; err != nil {
		return []GPT56MappingRunView{}
	}
	views := make([]GPT56MappingRunView, 0, len(runs))
	for _, run := range runs {
		views = append(views, GPT56MappingRunView{
			ID: run.ID, ParentRunID: run.ParentRunID, Level: run.Level, Trigger: run.Trigger,
			Status: run.Status, Results: decodeGPT56MappingResults(run.Results),
			StartedAt: run.StartedAt, CompletedAt: run.CompletedAt,
		})
	}
	return views
}

func decodeGPT56MappingResults(raw string) []GPT56MappingResult {
	var results []GPT56MappingResult
	if json.Unmarshal([]byte(raw), &results) != nil || results == nil {
		return []GPT56MappingResult{}
	}
	return results
}

func publicGPT56MappingResults(raw string) []GPT56MappingResult {
	results := decodeGPT56MappingResults(raw)
	for resultIndex := range results {
		results[resultIndex].Error = ""
		for sampleIndex := range results[resultIndex].Samples {
			sample := &results[resultIndex].Samples[sampleIndex]
			switch sample.Status {
			case GPT56MappingSampleStatusError:
				sample.Error = "请求失败，未获得可验证结果"
			case GPT56MappingSampleStatusMissingModel:
				sample.Error = "上游响应未返回模型标识"
			default:
				sample.Error = ""
			}
		}
	}
	return results
}
