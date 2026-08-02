package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	bountyschema "github.com/sh2001sh/new-api/internal/bounty/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
)

const (
	disputeAIEndpointEnv = "BOUNTY_AI_ENDPOINT"
	disputeAIKeyEnv      = "BOUNTY_AI_API_KEY"
	disputeAIModelEnv    = "BOUNTY_AI_MODEL"
	disputeAIModel       = "bounty-advisory-v1"
	disputeAIRequestSize = 2 << 20
)

type disputeAIProvider struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

type disputeAIRequest struct {
	Model          string             `json:"model"`
	Messages       []disputeAIMessage `json:"messages"`
	Temperature    float64            `json:"temperature"`
	ResponseFormat map[string]string  `json:"response_format,omitempty"`
}

type disputeAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type disputeAIResponse struct {
	Choices []struct {
		Message disputeAIMessage `json:"message"`
	} `json:"choices"`
}

func configuredDisputeAIProvider() (*disputeAIProvider, bool, error) {
	endpoint := strings.TrimSpace(os.Getenv(disputeAIEndpointEnv))
	if endpoint == "" {
		return nil, false, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, false, fmt.Errorf("%s must be an absolute URL", disputeAIEndpointEnv)
	}
	if parsed.Scheme != "https" && !isLoopbackHost(parsed.Hostname()) {
		return nil, false, fmt.Errorf("%s must use https outside localhost", disputeAIEndpointEnv)
	}
	model := strings.TrimSpace(os.Getenv(disputeAIModelEnv))
	if model == "" {
		model = disputeAIModel
	}
	return &disputeAIProvider{
		endpoint: endpoint,
		apiKey:   strings.TrimSpace(os.Getenv(disputeAIKeyEnv)),
		model:    model,
		client:   &http.Client{Timeout: 30 * time.Second},
	}, true, nil
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (provider *disputeAIProvider) analyze(ctx context.Context, snapshot string) (string, error) {
	prompt := "Analyze this bounty dispute snapshot. Return only a JSON object with the keys final_requirement_summary, requirement_changed, evidence_matches_commit, requirement_checks, publisher_response_timely, executor_followed_replies, missing_evidence, recommended_resolution, confidence, risk_flags, disclaimer. This is advisory evidence analysis only; never make or imply a funds decision. Do not infer private repository contents; use only the supplied platform records.\n\n" + snapshot
	body, err := platformencoding.Marshal(disputeAIRequest{
		Model: provider.model,
		Messages: []disputeAIMessage{
			{Role: "system", Content: "You are a neutral review assistant for a coding bounty platform."},
			{Role: "user", Content: prompt},
		},
		Temperature:    0,
		ResponseFormat: map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	if provider.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+provider.apiKey)
	}
	response, err := provider.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("bounty AI provider returned status %d", response.StatusCode)
	}
	var decoded disputeAIResponse
	if err := platformencoding.DecodeJSON(io.LimitReader(response.Body, disputeAIRequestSize), &decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("bounty AI provider returned no analysis")
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	var object map[string]any
	if err := platformencoding.Unmarshal([]byte(content), &object); err != nil {
		return "", fmt.Errorf("bounty AI provider returned invalid JSON: %w", err)
	}
	object["disclaimer"] = "AI 仅提供基于平台快照的建议，不能直接操作额度；最终裁决由管理员完成。"
	return marshalEvidenceSnapshot(object)
}

func buildDisputeSnapshotTx(tx *gorm.DB, task *bountyschema.BountyTask, reason string, desiredOutcome string, evidence string) (string, error) {
	var requests []bountyschema.BountyMaterialRequest
	if err := tx.Where("task_id = ?", task.TaskID).Order("created_at ASC").Find(&requests).Error; err != nil {
		return "", err
	}
	requestIDs := make([]string, 0, len(requests))
	for index := range requests {
		requestIDs = append(requestIDs, requests[index].RequestID)
	}
	var replies []bountyschema.BountyMaterialReply
	if len(requestIDs) > 0 {
		if err := tx.Where("request_id IN ?", requestIDs).Order("created_at ASC").Find(&replies).Error; err != nil {
			return "", err
		}
	}
	var submissions []bountyschema.BountySubmission
	if err := tx.Where("task_id = ?", task.TaskID).Order("version ASC").Find(&submissions).Error; err != nil {
		return "", err
	}
	var events []bountyschema.BountyEvent
	if err := tx.Where("task_id = ?", task.TaskID).Order("created_at ASC").Find(&events).Error; err != nil {
		return "", err
	}
	return marshalEvidenceSnapshot(map[string]any{
		"captured_at": time.Now(),
		"task":        task,
		"dispute": map[string]string{
			"reason":          strings.TrimSpace(reason),
			"desired_outcome": strings.TrimSpace(desiredOutcome),
			"evidence_text":   strings.TrimSpace(evidence),
		},
		"material_requests": requests,
		"material_replies":  replies,
		"submissions":       submissions,
		"events":            events,
	})
}

func marshalEvidenceSnapshot(value any) (string, error) {
	encoded, err := platformencoding.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func buildEvidenceAnalysis(snapshot string) (string, error) {
	var evidence map[string]any
	if err := platformencoding.Unmarshal([]byte(snapshot), &evidence); err != nil {
		return "", err
	}
	submissions, _ := evidence["submissions"].([]any)
	requests, _ := evidence["material_requests"].([]any)
	replies, _ := evidence["material_replies"].([]any)
	checks := []map[string]string{
		{"item": "任务描述已保存", "result": "pass"},
		{"item": "最终 Commit SHA 已提交", "result": "unknown"},
		{"item": "GitHub 交付地址已提交", "result": "unknown"},
		{"item": "测试结果已提交", "result": "unknown"},
	}
	missing := make([]string, 0, 3)
	evidenceMatches := false
	if len(submissions) > 0 {
		last, _ := submissions[len(submissions)-1].(map[string]any)
		commit, commitOK := last["commit_sha"].(string)
		repo, repoOK := last["repo_url"].(string)
		testReport, testOK := last["test_report"].(string)
		evidenceMatches = commitOK && commit != "" && repoOK && repo != ""
		if evidenceMatches {
			checks[1]["result"] = "pass"
			checks[2]["result"] = "pass"
		} else {
			missing = append(missing, "GitHub 仓库或 Commit 证据不完整")
		}
		if testOK && testReport != "" {
			checks[3]["result"] = "pass"
		} else {
			missing = append(missing, "测试结果未提供")
		}
	} else {
		missing = append(missing, "尚未发现交付记录")
	}
	requirementChanged := len(replies) > 0 || len(requests) > 0
	recommendation := "manual_review"
	if evidenceMatches && len(missing) == 0 {
		recommendation = "compare_requirement_and_delivery"
	}
	analysis := map[string]any{
		"final_requirement_summary": "以任务原文和材料回复为准，需由管理员核对最终确认内容。",
		"requirement_changed":       requirementChanged,
		"evidence_matches_commit":   evidenceMatches,
		"requirement_checks":        checks,
		"publisher_response_timely": "unknown",
		"executor_followed_replies": "unknown",
		"missing_evidence":          missing,
		"recommended_resolution":    recommendation,
		"confidence":                0.55,
		"risk_flags":                []string{"规则引擎不能访问私有仓库内容或验证 Commit 是否真实存在"},
		"disclaimer":                "这是基于平台记录的结构化分析建议，不能直接操作额度。",
	}
	result, err := marshalEvidenceSnapshot(analysis)
	if err != nil {
		return "", fmt.Errorf("marshal dispute analysis: %w", err)
	}
	return result, nil
}

func processDisputeAnalysis(disputeID string) error {
	provider, configured, err := configuredDisputeAIProvider()
	if err != nil {
		return err
	}
	if !configured {
		return nil
	}
	var dispute bountyschema.BountyDispute
	if err := platformdb.DB.Where("dispute_id = ?", disputeID).First(&dispute).Error; err != nil {
		return err
	}
	if dispute.AIStatus != "pending" {
		return nil
	}
	analysis, err := provider.analyze(context.Background(), dispute.SnapshotText)
	status := "completed"
	if err != nil {
		fallback, fallbackErr := buildEvidenceAnalysis(dispute.SnapshotText)
		if fallbackErr != nil {
			return fmt.Errorf("bounty AI analysis failed: %w; fallback failed: %v", err, fallbackErr)
		}
		analysis = fallback
		status = "failed"
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var current bountyschema.BountyDispute
		if err := tx.Where("dispute_id = ?", disputeID).First(&current).Error; err != nil {
			return err
		}
		if current.AIStatus != "pending" {
			return nil
		}
		return tx.Model(&current).Updates(map[string]any{
			"ai_analysis_text": analysis,
			"ai_model":         provider.model,
			"ai_status":        status,
		}).Error
	})
}

func queueDisputeAnalysis(disputeID string) {
	if _, configured, err := configuredDisputeAIProvider(); err != nil || !configured {
		return
	}
	go func() {
		if err := processDisputeAnalysis(disputeID); err != nil {
			platformobservability.SysLog(fmt.Sprintf("bounty dispute AI analysis failed: dispute_id=%s error=%v", disputeID, err))
		}
	}()
}
