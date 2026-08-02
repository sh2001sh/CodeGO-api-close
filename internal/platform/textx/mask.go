package textx

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	maskURLPattern    = regexp.MustCompile(`(http|https)://[^\s/$.?#].[^\s]*`)
	maskDomainPattern = regexp.MustCompile(`\b(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}\b`)
	maskIPPattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	maskAPIKeyPattern = regexp.MustCompile(`(['"]?)api_key:([^\s'"]+)(['"]?)`)

	upstreamQuotaLeakPattern   = regexp.MustCompile(`(?i)(status_code\s*=\s*403.*(?:预扣费额度失败|用户剩余额度|需要预扣费额度)|(?:预扣费额度失败|用户剩余额度|需要预扣费额度).*(?:request id\s*:|status_code\s*=)|用户额度不足[^\n]*(?:剩余额度|余额)[^\n]*(?:request id\s*:|status_code\s*=)|\binsufficient[\s_]+(?:quota|balance)\b|pre-?consume.*quota.*(?:request id\s*:|status_code\s*=))`)
	upstreamUnavailablePattern = regexp.MustCompile(`(?i)(\bno\s+available\s+(?:channel|route|provider|model)\b|\bno\s+(?:enabled|valid)\s+(?:channel|route|provider)\b|\b(?:model|service|channel|provider|upstream)\b[^\n]{0,80}\b(?:unavailable|not available|not found|not exist|not supported|temporarily unavailable)\b|(?:没有|无)可用(?:的)?(?:渠道|路由|供应商|模型)|(?:渠道|路由|模型|服务).{0,40}(?:不可用|不存在|不支持))`)
)

const UpstreamQuotaGenericMessage = "当前模型服务暂不可用，请稍后重试"

// MaskEmail hides the local part of an email address for logs and user-facing telemetry.
func MaskEmail(email string) string {
	if email == "" {
		return "***masked***"
	}

	atIndex := strings.Index(email, "@")
	if atIndex == -1 {
		return "***masked***"
	}

	return "***@" + email[atIndex+1:]
}

func maskHostTail(parts []string) []string {
	if len(parts) < 2 {
		return parts
	}
	lastPart := parts[len(parts)-1]
	secondLastPart := parts[len(parts)-2]
	if len(lastPart) == 2 && len(secondLastPart) <= 3 {
		return []string{secondLastPart, lastPart}
	}
	return []string{lastPart}
}

func maskHostForURL(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "***"
	}
	tail := maskHostTail(parts)
	return "***." + strings.Join(tail, ".")
}

func maskHostForPlainDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return domain
	}
	tail := maskHostTail(parts)
	numStars := len(parts) - len(tail)
	if numStars < 1 {
		numStars = 1
	}
	stars := strings.TrimSuffix(strings.Repeat("***.", numStars), ".")
	return stars + "." + strings.Join(tail, ".")
}

// MaskSensitiveInfo redacts URLs, domains, IPs, and inline API keys in log text.
func MaskSensitiveInfo(str string) string {
	str = maskURLPattern.ReplaceAllStringFunc(str, func(urlStr string) string {
		u, err := url.Parse(urlStr)
		if err != nil {
			return urlStr
		}

		host := u.Host
		if host == "" {
			return urlStr
		}

		maskedHost := maskHostForURL(host)
		result := u.Scheme + "://" + maskedHost

		if u.Path != "" && u.Path != "/" {
			pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
			maskedPathParts := make([]string, len(pathParts))
			for i := range pathParts {
				if pathParts[i] != "" {
					maskedPathParts[i] = "***"
				}
			}
			if len(maskedPathParts) > 0 {
				result += "/" + strings.Join(maskedPathParts, "/")
			}
		} else if u.Path == "/" {
			result += "/"
		}

		if u.RawQuery != "" {
			values, err := url.ParseQuery(u.RawQuery)
			if err != nil {
				result += "?***"
			} else {
				maskedParams := make([]string, 0, len(values))
				for key := range values {
					maskedParams = append(maskedParams, key+"=***")
				}
				if len(maskedParams) > 0 {
					result += "?" + strings.Join(maskedParams, "&")
				}
			}
		}

		return result
	})

	str = maskDomainPattern.ReplaceAllStringFunc(str, func(domain string) string {
		return maskHostForPlainDomain(domain)
	})

	str = maskIPPattern.ReplaceAllString(str, "***.***.***.***")
	str = maskAPIKeyPattern.ReplaceAllString(str, "${1}api_key:***${3}")
	return str
}

// SanitizeUpstreamProviderErrorMessage hides upstream availability and quota details.
func SanitizeUpstreamProviderErrorMessage(message string) string {
	if message == "" {
		return ""
	}
	lowerMessage := strings.ToLower(message)
	if strings.Contains(lowerMessage, "status_code=429") || strings.Contains(lowerMessage, "cooling down via provider") {
		return "status_code=429"
	}
	if IsUpstreamProviderUnavailableMessage(message) {
		return UpstreamQuotaGenericMessage
	}
	return message
}

// IsUpstreamProviderUnavailableMessage reports whether upstream text exposes provider capacity details.
func IsUpstreamProviderUnavailableMessage(message string) bool {
	if message == "" {
		return false
	}
	return upstreamQuotaLeakPattern.MatchString(message) || upstreamUnavailablePattern.MatchString(message)
}

// SanitizeUpstreamQuotaErrorMessage is kept for callers that only need the legacy name.
func SanitizeUpstreamQuotaErrorMessage(message string) string {
	return SanitizeUpstreamProviderErrorMessage(message)
}

// IsUpstreamQuotaLeakMessage reports whether the message leaks upstream quota or balance details.
func IsUpstreamQuotaLeakMessage(message string) bool {
	if message == "" {
		return false
	}
	return upstreamQuotaLeakPattern.MatchString(message)
}
