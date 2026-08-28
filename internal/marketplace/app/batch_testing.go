package app

import (
	"errors"
	"strings"
	"sync"
	"time"

	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

type BatchTestRequest struct {
	GroupIDs []string `json:"group_ids"`
	Model    string   `json:"model"`
}

type BatchTestItem struct {
	GroupID       string     `json:"group_id"`
	GroupName     string     `json:"group_name"`
	Status        string     `json:"status"`
	LatencyMS     int64      `json:"latency_ms"`
	Error         string     `json:"error,omitempty"`
	QuotaCharged  int        `json:"quota_charged"`
	LogCreated    bool       `json:"log_created"`
	RequestID     string     `json:"request_id,omitempty"`
	BillingSource string     `json:"billing_source,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
}

type BatchTestView struct {
	ID           string          `json:"id"`
	OwnerUserID  int             `json:"-"`
	Model        string          `json:"model"`
	Status       string          `json:"status"`
	BillingMode  string          `json:"billing_mode"`
	QuotaCharged bool            `json:"quota_charged"`
	LogCreated   bool            `json:"log_created"`
	Items        []BatchTestItem `json:"items"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

var batchTests = struct {
	sync.RWMutex
	items map[string]*BatchTestView
}{items: make(map[string]*BatchTestView)}

func StartBatchMarketplaceTest(userID int, req BatchTestRequest) (*BatchTestView, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return nil, errors.New("请选择要测试的模型")
	}
	if len(req.GroupIDs) == 0 || len(req.GroupIDs) > 5 {
		return nil, errors.New("批量测试需要选择 1-5 个分组")
	}
	groups, channels, err := loadAutoRouteGroups(userID)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]marketplaceschema.Group, len(groups))
	for _, group := range groups {
		allowed[group.ID] = group
	}
	seen := map[string]bool{}
	items := make([]BatchTestItem, 0, len(req.GroupIDs))
	for _, rawID := range req.GroupIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		group, ok := allowed[id]
		if !ok {
			return nil, errors.New("包含不可用或无权访问的分组")
		}
		channel := channels[group.ChannelID]
		if !containsFold(decodeModels(channel.DeclaredModels), model) {
			return nil, errors.New("所选分组不支持指定模型")
		}
		items = append(items, BatchTestItem{GroupID: id, GroupName: group.SystemDisplayName, Status: "queued"})
	}
	if len(items) == 0 {
		return nil, errors.New("没有有效的测试分组")
	}
	targets := make(map[string]batchTestTarget, len(items))
	for _, item := range items {
		group := allowed[item.GroupID]
		channel := channels[group.ChannelID]
		if channel.InternalChannelID == nil || *channel.InternalChannelID <= 0 {
			return nil, errors.New("所选分组缺少可用的内部渠道")
		}
		targets[item.GroupID] = batchTestTarget{
			InternalChannelID: *channel.InternalChannelID,
			GroupID:           group.ID,
			InternalGroup:     group.InternalGroupName,
			OwnerUserID:       group.OwnerUserID,
			CreditPoolPolicy:  group.CreditPoolPolicy,
			Multiplier:        group.Multiplier,
			ModelPrices:       decodeChannelModelPrices(channel.ModelPrices),
		}
	}
	view := &BatchTestView{
		ID: platformruntime.GetUUID(), OwnerUserID: userID, Model: model,
		Status: "queued", BillingMode: "user_quota", QuotaCharged: false,
		LogCreated: false, Items: items, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	batchTests.Lock()
	batchTests.items[view.ID] = view
	batchTests.Unlock()
	go executeBatchMarketplaceTest(view.ID, userID, targets)
	return cloneBatchTest(view), nil
}

func GetBatchMarketplaceTest(userID int, id string) (*BatchTestView, error) {
	batchTests.RLock()
	view, ok := batchTests.items[strings.TrimSpace(id)]
	if ok {
		view = cloneBatchTest(view)
	}
	batchTests.RUnlock()
	if !ok || view == nil || view.OwnerUserID != userID {
		return nil, errors.New("批量测试任务不存在或已过期")
	}
	return view, nil
}

type batchTestTarget struct {
	InternalChannelID int
	GroupID           string
	InternalGroup     string
	OwnerUserID       int
	CreditPoolPolicy  string
	Multiplier        float64
	ModelPrices       map[string]marketplacedomain.ChannelModelPrice
}

func executeBatchMarketplaceTest(id string, userID int, targets map[string]batchTestTarget) {
	updateBatch(id, func(view *BatchTestView) { view.Status = "running" })
	model := getBatchModel(id)
	for itemIndex := range getBatchItems(id) {
		item := getBatchItem(id, itemIndex)
		if item == nil {
			continue
		}
		target, ok := targets[item.GroupID]
		if !ok {
			updateBatchItem(id, itemIndex, func(target *BatchTestItem) {
				target.Status = "failed"
				target.Error = "测试目标不存在"
			})
			continue
		}
		started := time.Now().UTC()
		updateBatchItem(id, itemIndex, func(target *BatchTestItem) { target.Status = "running"; target.StartedAt = &started })
		start := time.Now()
		_, report, _, err := gatewayexecutionapp.TestMarketplaceChannelByID(
			target.InternalChannelID, model, "", false,
			gatewayexecutionapp.MarketplaceChannelTestOptions{
				UserID: userID, MarketplaceGroupID: target.GroupID,
				InternalGroup: target.InternalGroup, MarketplaceOwnerID: target.OwnerUserID,
				CreditPoolPolicy: target.CreditPoolPolicy, Multiplier: target.Multiplier,
				ModelPrices: target.ModelPrices,
			},
		)
		ended := time.Now().UTC()
		updateBatchItem(id, itemIndex, func(target *BatchTestItem) {
			target.EndedAt = &ended
			target.LatencyMS = time.Since(start).Milliseconds()
			target.RequestID = report.RequestID
			target.BillingSource = report.BillingSource
			target.QuotaCharged = report.QuotaCharged
			target.LogCreated = report.LogCreated
			if err != nil {
				target.Status = "failed"
				target.Error = err.Error()
			} else {
				target.Status = "passed"
			}
		})
	}
	updateBatch(id, func(view *BatchTestView) {
		view.Status = "completed"
		for _, item := range view.Items {
			if item.Status == "running" || item.Status == "queued" {
				view.Status = "failed"
				return
			}
			if item.QuotaCharged > 0 {
				view.QuotaCharged = true
			}
			if item.LogCreated {
				view.LogCreated = true
			}
		}
	})
}

func getBatchModel(id string) string {
	batchTests.RLock()
	defer batchTests.RUnlock()
	if view := batchTests.items[id]; view != nil {
		return view.Model
	}
	return ""
}

func updateBatch(id string, fn func(*BatchTestView)) {
	batchTests.Lock()
	defer batchTests.Unlock()
	if view := batchTests.items[id]; view != nil {
		fn(view)
		view.UpdatedAt = time.Now().UTC()
	}
}
func updateBatchItem(id string, index int, fn func(*BatchTestItem)) {
	updateBatch(id, func(view *BatchTestView) {
		if index >= 0 && index < len(view.Items) {
			fn(&view.Items[index])
		}
	})
}
func getBatchItems(id string) []BatchTestItem {
	batchTests.RLock()
	defer batchTests.RUnlock()
	if view := batchTests.items[id]; view != nil {
		return append([]BatchTestItem(nil), view.Items...)
	}
	return nil
}
func getBatchItem(id string, index int) *BatchTestItem {
	batchTests.RLock()
	defer batchTests.RUnlock()
	if view := batchTests.items[id]; view != nil && index >= 0 && index < len(view.Items) {
		item := view.Items[index]
		return &item
	}
	return nil
}
func cloneBatchTest(view *BatchTestView) *BatchTestView {
	if view == nil {
		return nil
	}
	clone := *view
	clone.Items = append([]BatchTestItem(nil), view.Items...)
	return &clone
}
