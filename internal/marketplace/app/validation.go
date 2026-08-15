package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strings"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
)

func validateCreateRequest(req CreateChannelRequest) error {
	if err := validateProvider(req.ProviderType); err != nil {
		return err
	}
	if err := validateSourceLabel(req.ProviderType, req.SourceLabel); err != nil {
		return err
	}
	if err := ValidateMarketplaceURL(req.BaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(req.APIKey) == "" {
		return errors.New("API Key 不能为空")
	}
	if err := validateModels(req.DeclaredModels); err != nil {
		return err
	}
	if err := validateMultiplier(req.Multiplier); err != nil {
		return err
	}
	if err := validateVisibility(req.Visibility); err != nil {
		return err
	}
	if req.MaxConcurrency <= 0 || req.MaxConcurrency > 10000 || req.QPS <= 0 || req.QPS > 10000 {
		return errors.New("容量声明必须是有效的最大并发和 QPS")
	}
	return nil
}

// ValidateMarketplaceURL rejects non-HTTPS and non-public destinations.
func ValidateMarketplaceURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("Base URL 必须是有效的 HTTPS 地址")
	}
	if parsed.Port() != "" && parsed.Port() != "443" && parsed.Port() != "8443" {
		return errors.New("Base URL 仅允许 443 或 8443 端口")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil || len(addresses) == 0 {
		return errors.New("Base URL 域名无法解析")
	}
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return errors.New("Base URL 不允许指向私网、环回或保留地址")
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() &&
		!ip.IsLinkLocalMulticast() && !ip.IsLinkLocalUnicast() && !ip.IsMulticast()
}

func validateProvider(provider string) error {
	switch strings.TrimSpace(provider) {
	case "openai_compatible", "codex", "azure_openai", "anthropic", "gemini":
		return nil
	default:
		return errors.New("不支持的渠道协议类型")
	}
}

func validateModels(models []string) error {
	if len(normalizeModels(models)) == 0 {
		return errors.New("至少声明一个可用模型")
	}
	for _, model := range models {
		if len(strings.TrimSpace(model)) > 255 {
			return fmt.Errorf("模型名称过长: %s", model)
		}
	}
	return nil
}

func validateMultiplier(multiplier float64) error {
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier <= 0 {
		return errors.New("倍率必须是大于 0 的有效数字")
	}
	return nil
}

func validateSourceLabel(_ string, label string) error {
	if _, ok := canonicalSourceLabel(label); !ok {
		return errors.New("请选择有效的渠道来源")
	}
	return nil
}

func validateVisibility(visibility string) error {
	switch visibility {
	case "", marketplacedomain.VisibilityPrivate, marketplacedomain.VisibilityUnlisted, marketplacedomain.VisibilityPublic:
		return nil
	default:
		return errors.New("无效的可见性")
	}
}
