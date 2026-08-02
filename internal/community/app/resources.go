package app

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	communityschema "github.com/sh2001sh/new-api/internal/community/schema"
	communitysettings "github.com/sh2001sh/new-api/internal/community/settings"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/gorm"
)

var (
	ErrInvalidResource         = errors.New("invalid community resource")
	ErrResourceAlreadyExists   = errors.New("this GitHub repository is already submitted")
	ErrResourceNotFound        = errors.New("community resource not found")
	ErrResourceRewardDisabled  = errors.New("community resource reward is not configured")
	ErrResourceAlreadyRewarded = errors.New("this GitHub repository has already received a reward")
)

var resourceCategories = map[string]struct{}{
	"script": {},
	"skill":  {},
	"tool":   {},
	"other":  {},
}

type CreateResourceRequest struct {
	Title              string `json:"title"`
	Description        string `json:"description"`
	Category           string `json:"category"`
	GitHubURL          string `json:"github_url"`
	AcknowledgementURL string `json:"acknowledgement_url"`
}

type ReviewResourceRequest struct {
	Status      string `json:"status"`
	Note        string `json:"note"`
	GrantReward bool   `json:"grant_reward"`
}

type ListResourcesRequest struct {
	Keyword  string
	Category string
	Status   string
	Page     int
	PageSize int
}

type ResourceView struct {
	communityschema.Resource
	DownloadURL string `json:"download_url"`
}

type ResourceList struct {
	Items    []ResourceView `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type ResourceConfig struct {
	SiteHost      string  `json:"site_host"`
	RewardEnabled bool    `json:"reward_enabled"`
	RewardUSD     float64 `json:"reward_usd"`
}

type UpdateResourceConfigRequest struct {
	RewardUSD float64 `json:"reward_usd"`
}

func GetResourceConfig() ResourceConfig {
	rewardUSD := communitysettings.Get().RewardUSD
	return ResourceConfig{SiteHost: "shu26.cfd", RewardEnabled: rewardUSD > 0, RewardUSD: rewardUSD}
}

func UpdateResourceConfig(request UpdateResourceConfigRequest) (ResourceConfig, error) {
	if request.RewardUSD < 0 || request.RewardUSD > 1000 {
		return ResourceConfig{}, fmt.Errorf("%w: reward must be between 0 and 1000 USD", ErrInvalidResource)
	}
	value := strconv.FormatFloat(request.RewardUSD, 'f', -1, 64)
	if err := platformstore.UpdateOption("community_resource_setting.reward_usd", value); err != nil {
		return ResourceConfig{}, err
	}
	return GetResourceConfig(), nil
}

// CreateResource validates and stores a GitHub-hosted contribution.
func CreateResource(userID int, username string, role int, request CreateResourceRequest) (*ResourceView, error) {
	resource, err := buildResource(userID, username, role, request)
	if err != nil {
		return nil, err
	}

	var duplicateCount int64
	err = platformdb.DB.Model(&communityschema.Resource{}).
		Where("repository_url = ? AND status IN ?", resource.RepositoryURL, []string{
			communityschema.ResourceStatusPending,
			communityschema.ResourceStatusApproved,
		}).
		Count(&duplicateCount).Error
	if err != nil {
		return nil, err
	}
	if duplicateCount > 0 {
		return nil, ErrResourceAlreadyExists
	}
	if err := platformdb.DB.Create(resource).Error; err != nil {
		return nil, err
	}
	view := toResourceView(*resource)
	return &view, nil
}

func ListApprovedResources(request ListResourcesRequest) (*ResourceList, error) {
	result, err := listResources(request, func(query *gorm.DB) *gorm.DB {
		return query.Where("status = ?", communityschema.ResourceStatusApproved)
	})
	if err != nil {
		return nil, err
	}
	for index := range result.Items {
		result.Items[index].ReviewNote = ""
		result.Items[index].ReviewedBy = nil
		result.Items[index].RewardedBy = nil
	}
	return result, nil
}

func ListUserResources(userID int, request ListResourcesRequest) (*ResourceList, error) {
	return listResources(request, func(query *gorm.DB) *gorm.DB {
		return query.Where("submitted_by = ?", userID)
	})
}

func ListAdminResources(request ListResourcesRequest) (*ResourceList, error) {
	return listResources(request, func(query *gorm.DB) *gorm.DB { return query })
}

// ReviewResource publishes or rejects a submitted resource.
func ReviewResource(resourceID int64, reviewerID int, request ReviewResourceRequest) (*ResourceView, error) {
	status := strings.ToLower(strings.TrimSpace(request.Status))
	if status != communityschema.ResourceStatusApproved && status != communityschema.ResourceStatusRejected {
		return nil, fmt.Errorf("%w: invalid review status", ErrInvalidResource)
	}
	note := strings.TrimSpace(request.Note)
	if len([]rune(note)) > 300 {
		return nil, fmt.Errorf("%w: review note is too long", ErrInvalidResource)
	}

	var resource communityschema.Resource
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&resource, resourceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrResourceNotFound
			}
			return err
		}
		now := time.Now().UTC()
		updates := map[string]any{
			"status":      status,
			"review_note": note,
			"reviewed_by": reviewerID,
		}
		if status == communityschema.ResourceStatusApproved {
			updates["published_at"] = &now
		} else {
			updates["published_at"] = nil
		}
		if request.GrantReward {
			if status != communityschema.ResourceStatusApproved {
				return fmt.Errorf("%w: rewards require approval", ErrInvalidResource)
			}
			if strings.TrimSpace(resource.AcknowledgementURL) == "" {
				return fmt.Errorf("%w: acknowledgement URL is required", ErrInvalidResource)
			}
			rewardUSD := communitysettings.Get().RewardUSD
			rewardQuota := int64(math.Round(rewardUSD * platformruntime.QuotaPerUnit))
			if rewardQuota <= 0 {
				return ErrResourceRewardDisabled
			}
			key := "community-resource:" + platformsecurity.Sha1([]byte(resource.RepositoryURL))
			granted, err := billingapp.GrantBonusWalletQuotaTx(
				tx,
				resource.SubmittedBy,
				rewardQuota,
				"community_resource_acknowledgement",
				strconv.FormatInt(resource.ID, 10),
				key,
			)
			if err != nil {
				return err
			}
			if !granted {
				return ErrResourceAlreadyRewarded
			}
			updates["reward_quota"] = rewardQuota
			updates["rewarded_by"] = reviewerID
			updates["rewarded_at"] = &now
		}
		if err := tx.Model(&resource).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&resource, resourceID).Error
	})
	if err != nil {
		return nil, err
	}
	view := toResourceView(resource)
	return &view, nil
}

func NormalizeGitHubResourceURL(raw string) (string, string, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", "", fmt.Errorf("%w: use an https://github.com URL", ErrInvalidResource)
	}
	if parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", fmt.Errorf("%w: GitHub URL contains unsupported parts", ErrInvalidResource)
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("%w: GitHub repository path is incomplete", ErrInvalidResource)
	}
	owner, ownerErr := url.PathUnescape(parts[0])
	repo, repoErr := url.PathUnescape(strings.TrimSuffix(parts[1], ".git"))
	if ownerErr != nil || repoErr != nil || !isGitHubPathSegment(owner) || !isGitHubPathSegment(repo) {
		return "", "", fmt.Errorf("%w: invalid GitHub owner or repository", ErrInvalidResource)
	}
	repositoryURL := "https://github.com/" + owner + "/" + repo
	parsed.Scheme = "https"
	parsed.Host = "github.com"
	parsed.Path = strings.TrimSuffix(parsed.Path, ".git")
	parsed.RawPath = ""
	return parsed.String(), repositoryURL, nil
}

func buildResource(userID int, username string, role int, request CreateResourceRequest) (*communityschema.Resource, error) {
	title := strings.TrimSpace(request.Title)
	description := strings.TrimSpace(request.Description)
	category := strings.ToLower(strings.TrimSpace(request.Category))
	githubURL, repositoryURL, err := NormalizeGitHubResourceURL(request.GitHubURL)
	if err != nil {
		return nil, err
	}
	if userID <= 0 || title == "" || len([]rune(title)) > 80 || description == "" || len([]rune(description)) > 500 {
		return nil, ErrInvalidResource
	}
	if _, ok := resourceCategories[category]; !ok {
		return nil, fmt.Errorf("%w: invalid category", ErrInvalidResource)
	}
	acknowledgementURL := ""
	if strings.TrimSpace(request.AcknowledgementURL) != "" {
		var acknowledgementRepository string
		acknowledgementURL, acknowledgementRepository, err = NormalizeGitHubResourceURL(request.AcknowledgementURL)
		if err != nil || acknowledgementRepository != repositoryURL {
			return nil, fmt.Errorf("%w: acknowledgement must be in the submitted repository", ErrInvalidResource)
		}
	}
	status := communityschema.ResourceStatusPending
	var publishedAt *time.Time
	if role >= constant.RoleAdminUser {
		status = communityschema.ResourceStatusApproved
		now := time.Now().UTC()
		publishedAt = &now
	}
	return &communityschema.Resource{
		Title:              title,
		Description:        description,
		Category:           category,
		GitHubURL:          githubURL,
		RepositoryURL:      repositoryURL,
		AcknowledgementURL: acknowledgementURL,
		SubmittedBy:        userID,
		SubmitterName:      strings.TrimSpace(username),
		Status:             status,
		PublishedAt:        publishedAt,
	}, nil
}

func listResources(request ListResourcesRequest, scope func(*gorm.DB) *gorm.DB) (*ResourceList, error) {
	request.Page, request.PageSize = normalizePage(request.Page, request.PageSize)
	query := scope(platformdb.DB.Model(&communityschema.Resource{}))
	if category := strings.ToLower(strings.TrimSpace(request.Category)); category != "" && category != "all" {
		query = query.Where("category = ?", category)
	}
	if status := strings.ToLower(strings.TrimSpace(request.Status)); status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(request.Keyword); keyword != "" {
		like := "%" + strings.NewReplacer("%", "\\%", "_", "\\_").Replace(keyword) + "%"
		query = query.Where("title LIKE ? OR description LIKE ? OR repository_url LIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var resources []communityschema.Resource
	if err := query.Order("published_at DESC, created_at DESC").
		Limit(request.PageSize).
		Offset((request.Page - 1) * request.PageSize).
		Find(&resources).Error; err != nil {
		return nil, err
	}
	items := make([]ResourceView, 0, len(resources))
	for _, resource := range resources {
		items = append(items, toResourceView(resource))
	}
	return &ResourceList{Items: items, Total: total, Page: request.Page, PageSize: request.PageSize}, nil
}

func normalizePage(page int, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return page, pageSize
}

func toResourceView(resource communityschema.Resource) ResourceView {
	return ResourceView{
		Resource:    resource,
		DownloadURL: resource.RepositoryURL + "/archive/HEAD.zip",
	}
}

func isGitHubPathSegment(value string) bool {
	if value == "" || len(value) > 100 || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("-_.", char) {
			continue
		}
		return false
	}
	return true
}

func ParseResourceID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrResourceNotFound
	}
	return id, nil
}
