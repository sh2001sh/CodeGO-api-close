package app

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm/clause"
)

const pelicanPrompt = `生成单文件 HTML，内容使用内联 SVG 绘制“鹈鹕骑自行车”的 2D 动画。画面需要清晰、美观并包含可见动画，但 SVG 源码不得超过 18 KiB、图形元素不得超过 60 个；优先复用 defs、g、use 和简洁 path。禁止外部资源、网络请求、脚本工具、注释、说明文字和 Markdown 代码围栏。只输出完整 HTML，并务必在输出上限前闭合 </svg></body></html>，不要执行测试。`

const (
	pelicanMaxOutputTokens = 8000
	maxPelicanSVGBytes     = 512 * 1024
)

type PelicanTestRequest struct {
	GroupID string `json:"group_id"`
	Model   string `json:"model"`
}

type PelicanTestView struct {
	ID            string     `json:"id"`
	OwnerUserID   int        `json:"-"`
	GroupID       string     `json:"group_id"`
	Model         string     `json:"model"`
	Status        string     `json:"status"`
	Error         string     `json:"error,omitempty"`
	QuotaCharged  int        `json:"quota_charged"`
	BillingSource string     `json:"billing_source,omitempty"`
	RequestID     string     `json:"request_id,omitempty"`
	DurationMS    int64      `json:"duration_ms"`
	GeneratedAt   *time.Time `json:"generated_at,omitempty"`
	ArtifactURL   string     `json:"artifact_url,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type pelicanTarget struct {
	InternalChannelID  int
	GroupID            string
	MarketplaceGroupID string
	ChannelID          string
	InternalGroup      string
	OwnerUserID        int
	CreditPoolPolicy   string
	Multiplier         float64
	ModelPrices        map[string]marketplacedomain.ChannelModelPrice
}

var pelicanTests = struct {
	sync.RWMutex
	items map[string]*PelicanTestView
}{items: map[string]*PelicanTestView{}}

var pelicanRunning sync.Map
var pelicanScheduleSlots = make(chan struct{}, 2)

func StartPelicanTest(userID int, req PelicanTestRequest) (*PelicanTestView, error) {
	if userID <= 0 {
		return nil, errors.New("用户未登录")
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return nil, errors.New("请选择鹈鹕测试模型")
	}
	target, err := resolvePelicanTarget(userID, strings.TrimSpace(req.GroupID), model)
	if err != nil {
		return nil, err
	}
	if _, loaded := pelicanRunning.LoadOrStore(target.GroupID, struct{}{}); loaded {
		return nil, errors.New("该分组正在生成鹈鹕作品")
	}
	now := time.Now().UTC()
	view := &PelicanTestView{ID: platformruntime.GetUUID(), OwnerUserID: userID, GroupID: target.GroupID, Model: model, Status: "queued", CreatedAt: now, UpdatedAt: now}
	pelicanTests.Lock()
	pelicanTests.items[view.ID] = view
	pelicanTests.Unlock()
	go executePelicanTest(view.ID, target, userID, true, "manual")
	return clonePelicanTest(view), nil
}

func GetPelicanTest(userID int, id string) (*PelicanTestView, error) {
	pelicanTests.RLock()
	view := clonePelicanTest(pelicanTests.items[strings.TrimSpace(id)])
	pelicanTests.RUnlock()
	if view == nil || view.OwnerUserID != userID {
		return nil, errors.New("鹈鹕测试任务不存在或已过期")
	}
	return view, nil
}

func GetPelicanArtifact(groupID string, viewerUserID int) (*marketplaceschema.PelicanArtifact, error) {
	groupID = strings.TrimSpace(groupID)
	if !strings.HasPrefix(groupID, officialAutoRoutePrefix) {
		var count int64
		if err := publicGroupsQuery(GroupQuery{ViewerUserID: viewerUserID, IncludeAccess: viewerUserID > 0}).Where("id = ?", groupID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, errors.New("鹈鹕作品不存在或无权访问")
		}
	}
	var artifact marketplaceschema.PelicanArtifact
	if err := platformdb.DB.First(&artifact, "group_id = ?", groupID).Error; err != nil {
		return nil, err
	}
	return &artifact, nil
}

func resolvePelicanTarget(userID int, groupID, model string) (pelicanTarget, error) {
	if strings.HasPrefix(groupID, officialAutoRoutePrefix) {
		name := strings.TrimSpace(strings.TrimPrefix(groupID, officialAutoRoutePrefix))
		models, err := officialStatusModels(userID)
		if err != nil || !containsFold(models[name], model) {
			return pelicanTarget{}, errors.New("所选官方分组不支持该模型")
		}
		channels, err := gatewaystore.LoadEnabledChannelsForGroup(name)
		if err != nil {
			return pelicanTarget{}, err
		}
		for _, channel := range channels {
			if gatewaystore.IsChannelEnabledForGroupModel(name, model, channel.Id) {
				userGroup, loadErr := identitystore.LoadUserGroup(userID, false)
				if loadErr != nil {
					return pelicanTarget{}, loadErr
				}
				return pelicanTarget{InternalChannelID: channel.Id, GroupID: groupID, InternalGroup: name, CreditPoolPolicy: marketplacedomain.CreditPolicyOfficialDefault, Multiplier: gatewayroutingapp.GetUserGroupRatio(userGroup, name)}, nil
			}
		}
		return pelicanTarget{}, errors.New("所选官方分组当前没有可用渠道")
	}
	groups, channels, err := loadAutoRouteGroups(userID)
	if err != nil {
		return pelicanTarget{}, err
	}
	for _, group := range groups {
		if group.ID != groupID {
			continue
		}
		channel := channels[group.ChannelID]
		if !containsFold(decodeModels(channel.DeclaredModels), model) {
			return pelicanTarget{}, errors.New("所选分组不支持该模型")
		}
		if channel.InternalChannelID == nil || *channel.InternalChannelID <= 0 {
			return pelicanTarget{}, errors.New("所选分组缺少可用内部渠道")
		}
		return pelicanTarget{InternalChannelID: *channel.InternalChannelID, GroupID: group.ID, MarketplaceGroupID: group.ID, ChannelID: channel.ID, InternalGroup: group.InternalGroupName, OwnerUserID: group.OwnerUserID, CreditPoolPolicy: group.CreditPoolPolicy, Multiplier: group.Multiplier, ModelPrices: decodeChannelModelPrices(channel.ModelPrices)}, nil
	}
	return pelicanTarget{}, errors.New("分组不存在或无权访问")
}

func executePelicanTest(id string, target pelicanTarget, userID int, billUser bool, trigger string) {
	defer pelicanRunning.Delete(target.GroupID)
	updatePelicanTest(id, func(view *PelicanTestView) { view.Status = "running" })
	started := time.Now()
	text, report, _, err := gatewayexecutionapp.GenerateMarketplaceChannelContentByID(target.InternalChannelID, pelicanModel(id), gatewayexecutionapp.MarketplaceContentGenerationOptions{
		MarketplaceChannelTestOptions: gatewayexecutionapp.MarketplaceChannelTestOptions{UserID: userID, MarketplaceGroupID: target.MarketplaceGroupID, InternalGroup: target.InternalGroup, MarketplaceOwnerID: target.OwnerUserID, CreditPoolPolicy: target.CreditPoolPolicy, Multiplier: target.Multiplier, ModelPrices: target.ModelPrices},
		Prompt:                        pelicanPrompt, MaxOutputTokens: pelicanMaxOutputTokens, BillUser: billUser,
	})
	duration := time.Since(started).Milliseconds()
	if err == nil {
		var svg string
		svg, err = sanitizePelicanSVG(text)
		if err == nil {
			now := time.Now().UTC()
			artifact := marketplaceschema.PelicanArtifact{GroupID: target.GroupID, ChannelID: target.ChannelID, Model: pelicanModel(id), SVG: svg, Trigger: trigger, TriggerUserID: userID, RequestID: report.RequestID, DurationMS: duration, GeneratedAt: now}
			err = platformdb.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "group_id"}}, DoUpdates: clause.AssignmentColumns([]string{"channel_id", "model", "svg", "trigger", "trigger_user_id", "request_id", "duration_ms", "generated_at", "updated_at"})}).Create(&artifact).Error
			if err == nil {
				invalidateMarketplaceListCache()
			}
			if err == nil && id != "" {
				updatePelicanTest(id, func(view *PelicanTestView) {
					view.GeneratedAt = &now
					view.ArtifactURL = "/api/marketplace/pelican-artifact.svg?group_id=" + url.QueryEscape(target.GroupID)
				})
			}
		}
	}
	if id != "" {
		updatePelicanTest(id, func(view *PelicanTestView) {
			view.DurationMS, view.QuotaCharged, view.BillingSource, view.RequestID = duration, report.QuotaCharged, report.BillingSource, report.RequestID
			if err != nil {
				view.Status, view.Error = "failed", err.Error()
			} else {
				view.Status = "completed"
			}
		})
	}
}

func pelicanModel(id string) string {
	pelicanTests.RLock()
	defer pelicanTests.RUnlock()
	if view := pelicanTests.items[id]; view != nil {
		return view.Model
	}
	return ""
}

func updatePelicanTest(id string, fn func(*PelicanTestView)) {
	pelicanTests.Lock()
	defer pelicanTests.Unlock()
	if view := pelicanTests.items[id]; view != nil {
		fn(view)
		view.UpdatedAt = time.Now().UTC()
	}
}

func clonePelicanTest(view *PelicanTestView) *PelicanTestView {
	if view == nil {
		return nil
	}
	clone := *view
	return &clone
}

func invalidateMarketplaceListCache() {
	marketplaceListCache.Lock()
	marketplaceListCache.result = nil
	marketplaceListCache.Unlock()
}

var allowedPelicanElements = map[string]bool{
	"svg": true, "g": true, "path": true, "circle": true, "ellipse": true, "rect": true, "line": true, "polyline": true, "polygon": true, "text": true, "tspan": true, "defs": true, "lineargradient": true, "radialgradient": true, "stop": true, "clippath": true, "mask": true, "filter": true, "fegaussianblur": true, "feoffset": true, "femerge": true, "femergenode": true, "animate": true, "animatetransform": true, "animatemotion": true, "mpath": true, "style": true, "title": true, "desc": true, "use": true,
}

func sanitizePelicanSVG(raw string) (string, error) {
	start := strings.Index(strings.ToLower(raw), "<svg")
	end := strings.LastIndex(strings.ToLower(raw), "</svg>")
	if start < 0 || end < start {
		return "", errors.New("模型返回内容中没有完整 SVG")
	}
	end += len("</svg>")
	if end-start > maxPelicanSVGBytes {
		return "", errors.New("鹈鹕 SVG 超过 512 KiB 限制")
	}
	decoder := xml.NewDecoder(strings.NewReader(raw[start:end]))
	var out bytes.Buffer
	depth, skipped := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("SVG 格式无效: %w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			name := strings.ToLower(value.Name.Local)
			if skipped > 0 {
				skipped++
				continue
			}
			if !allowedPelicanElements[name] {
				skipped = 1
				continue
			}
			depth++
			out.WriteByte('<')
			out.WriteString(value.Name.Local)
			for _, attr := range value.Attr {
				attrName := attr.Name.Local
				lowerName, lowerValue := strings.ToLower(attrName), strings.ToLower(strings.TrimSpace(attr.Value))
				if strings.HasPrefix(lowerName, "on") || ((lowerName == "href" || lowerName == "xlink:href") && !strings.HasPrefix(lowerValue, "#")) || unsafePelicanStyle(lowerValue) {
					continue
				}
				out.WriteByte(' ')
				out.WriteString(attrName)
				out.WriteString(`="`)
				out.WriteString(html.EscapeString(attr.Value))
				out.WriteByte('"')
			}
			out.WriteByte('>')
		case xml.EndElement:
			if skipped > 0 {
				skipped--
				continue
			}
			if depth > 0 {
				out.WriteString("</")
				out.WriteString(value.Name.Local)
				out.WriteByte('>')
				depth--
			}
		case xml.CharData:
			if skipped == 0 {
				text := string(value)
				if unsafePelicanStyle(strings.ToLower(text)) {
					continue
				}
				out.WriteString(html.EscapeString(text))
			}
		}
	}
	result := out.String()
	if !strings.HasPrefix(strings.ToLower(result), "<svg") || len(result) > maxPelicanSVGBytes {
		return "", errors.New("清洗后的 SVG 无效")
	}
	return result, nil
}

func unsafePelicanStyle(value string) bool {
	return strings.Contains(value, "url(") || strings.Contains(value, "@import") || strings.Contains(value, "expression(") || strings.Contains(value, "javascript:") || strings.Contains(value, "data:")
}

// StartMarketplacePelicanScheduleTask generates at most once per local day for
// active third-party channels. Official groups have no Channel row and cannot enter this scan.
func StartMarketplacePelicanScheduleTask() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			runDuePelicanSchedules(time.Now())
			<-ticker.C
		}
	}()
}

func runDuePelicanSchedules(now time.Time) {
	var channels []marketplaceschema.Channel
	if err := platformdb.DB.Where("pelican_probe_enabled = ? AND status IN ?", true, []string{marketplacedomain.LifecycleActive, marketplacedomain.LifecycleDegraded}).Find(&channels).Error; err != nil {
		platformobservability.SysError("load pelican schedules: " + err.Error())
		return
	}
	minute := now.Hour()*60 + now.Minute()
	for index := range channels {
		channel := channels[index]
		if channel.PelicanProbeDailyMinute != minute || (channel.PelicanProbeLastAt != nil && sameLocalDay(*channel.PelicanProbeLastAt, now)) {
			continue
		}
		var group marketplaceschema.Group
		if err := platformdb.DB.Where("channel_id = ? AND source_type = ? AND lifecycle_status IN ?", channel.ID, marketplacedomain.SourceTypeMarketplaceUser, []string{marketplacedomain.LifecycleActive, marketplacedomain.LifecycleDegraded}).First(&group).Error; err != nil {
			continue
		}
		if channel.InternalChannelID == nil || *channel.InternalChannelID <= 0 || !containsFold(decodeModels(channel.DeclaredModels), channel.PelicanProbeModel) {
			continue
		}
		if _, loaded := pelicanRunning.LoadOrStore(group.ID, struct{}{}); loaded {
			continue
		}
		target := pelicanTarget{InternalChannelID: *channel.InternalChannelID, GroupID: group.ID, MarketplaceGroupID: group.ID, ChannelID: channel.ID, InternalGroup: group.InternalGroupName, OwnerUserID: group.OwnerUserID, CreditPoolPolicy: group.CreditPoolPolicy, Multiplier: group.Multiplier, ModelPrices: decodeChannelModelPrices(channel.ModelPrices)}
		model := channel.PelicanProbeModel
		go func() {
			pelicanScheduleSlots <- struct{}{}
			defer func() { <-pelicanScheduleSlots }()
			// A scheduled owner test is an operational cost, never a user charge or self-settlement.
			executeScheduledPelican(target, model)
		}()
	}
}

func executeScheduledPelican(target pelicanTarget, model string) {
	defer pelicanRunning.Delete(target.GroupID)
	started := time.Now()
	text, report, _, err := gatewayexecutionapp.GenerateMarketplaceChannelContentByID(target.InternalChannelID, model, gatewayexecutionapp.MarketplaceContentGenerationOptions{MarketplaceChannelTestOptions: gatewayexecutionapp.MarketplaceChannelTestOptions{UserID: target.OwnerUserID, MarketplaceGroupID: target.MarketplaceGroupID, InternalGroup: target.InternalGroup, MarketplaceOwnerID: target.OwnerUserID, CreditPoolPolicy: target.CreditPoolPolicy, Multiplier: target.Multiplier, ModelPrices: target.ModelPrices}, Prompt: pelicanPrompt, MaxOutputTokens: pelicanMaxOutputTokens, BillUser: false})
	if err != nil {
		platformobservability.SysError("scheduled pelican test: " + err.Error())
		return
	}
	svg, err := sanitizePelicanSVG(text)
	if err != nil {
		platformobservability.SysError("sanitize scheduled pelican: " + err.Error())
		return
	}
	now := time.Now().UTC()
	artifact := marketplaceschema.PelicanArtifact{GroupID: target.GroupID, ChannelID: target.ChannelID, Model: model, SVG: svg, Trigger: "scheduled", TriggerUserID: target.OwnerUserID, RequestID: report.RequestID, DurationMS: time.Since(started).Milliseconds(), GeneratedAt: now}
	if err := platformdb.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "group_id"}}, DoUpdates: clause.AssignmentColumns([]string{"channel_id", "model", "svg", "trigger", "trigger_user_id", "request_id", "duration_ms", "generated_at", "updated_at"})}).Create(&artifact).Error; err != nil {
		platformobservability.SysError("save scheduled pelican: " + err.Error())
		return
	}
	invalidateMarketplaceListCache()
	platformdb.DB.Model(&marketplaceschema.Channel{}).Where("id = ?", target.ChannelID).Update("pelican_probe_last_at", now)
}

func sameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.In(time.Local).Date()
	by, bm, bd := b.In(time.Local).Date()
	return ay == by && am == bm && ad == bd
}
