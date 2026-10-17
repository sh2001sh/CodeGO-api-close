package app

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

var (
	publicContactPattern = regexp.MustCompile(`(?i)(h(?:tt|xx)ps?://|www\.|[a-z0-9][a-z0-9-]*\.(com|cn|net|org|io|me|top|xyz|shop|cfd|cc|vip|app|dev|ai|link|site|online|store)(?:$|[^a-z0-9-])|t\.me(?:$|[^a-z0-9-])|[a-z0-9._+%-]+@[a-z0-9.-]+\.[a-z]{2,})`)
	publicMobilePattern  = regexp.MustCompile(`(?:^|[^0-9])(?:86)?1[3-9][0-9]{9}(?:$|[^0-9])`)
	publicVXPattern      = regexp.MustCompile(`(?:^|[^a-z0-9])vx`)
)

func normalizePublicText(value string, limit int, name bool) (string, error) {
	value = strings.TrimSpace(norm.NFKC.String(value))
	if !name {
		value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return "", errors.New("名称、备注或标签超过长度限制")
	}
	var compact strings.Builder
	for _, char := range value {
		if !name && char == '\n' {
			continue
		}
		if unicode.IsControl(char) || unicode.In(char, unicode.Cf) {
			return "", errors.New("名称、备注或标签不能包含隐藏字符")
		}
		if name && !unicode.IsLetter(char) && !unicode.IsDigit(char) && !strings.ContainsRune(" -_·. +()（）", char) {
			return "", errors.New("名称和标签仅支持文字、数字、空格、短横线、下划线、点号、加号和括号")
		}
		if unicode.IsLetter(char) || unicode.IsDigit(char) || strings.ContainsRune(".@:/", char) {
			compact.WriteRune(unicode.ToLower(char))
		}
	}
	text := compact.String()
	if publicContactPattern.MatchString(text) || publicMobilePattern.MatchString(text) || publicNumericContact(value) {
		return "", errors.New("名称、备注和标签不能包含网址或联系方式")
	}
	if publicVXPattern.MatchString(text) {
		return "", errors.New("名称、备注和标签不能包含联系方式或站外引流广告")
	}
	for _, keyword := range []string{"微信", "微 信", "薇信", "威信", "加v", "加微", "加群", "加我", "私聊", "私信", "联系", "扣扣", "企鹅群", "qq群", "qq号", "wechat", "telegram", "whatsapp", "discord", "引流", "站外", "扫码", "二维码", "访问网站", "点击链接"} {
		if strings.Contains(text, strings.ReplaceAll(keyword, " ", "")) {
			return "", errors.New("名称、备注和标签不能包含联系方式或站外引流广告")
		}
	}
	return value, nil
}

// Dates and model version suffixes are technical metadata, not phone numbers.
// Unlabelled numeric-only contact strings still require semantic-free rejection.
func publicNumericContact(value string) bool {
	for _, layout := range []string{"2006-01-02", "2006/01/02", "2006.01.02", "20060102"} {
		if _, err := time.Parse(layout, value); err == nil {
			return false
		}
	}
	digits := 0
	for _, char := range value {
		if char >= '0' && char <= '9' {
			digits++
		} else if !unicode.IsSpace(char) && !strings.ContainsRune("+-().", char) {
			return false
		}
	}
	return digits >= 7
}

func normalizeGroupTags(tags []string) ([]string, error) {
	if len(tags) > 6 {
		return nil, errors.New("每个分组最多设置 6 个标签")
	}
	result := make([]string, 0, len(tags))
	seen := make(map[string]bool)
	for _, raw := range tags {
		tag, err := normalizePublicText(raw, 16, true)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(tag)
		if tag != "" && !seen[key] {
			result = append(result, tag)
			seen[key] = true
		}
	}
	return result, nil
}

func applyPublicPresentation(group *marketplaceschema.Group, name, description *string, tags *[]string) error {
	if name == nil && description == nil && tags == nil {
		return nil
	}
	oldName, oldDescription, oldTags := group.CustomName, group.Description, group.Tags
	var err error
	if name != nil {
		group.CustomName, err = normalizePublicText(*name, 20, true)
		if err != nil {
			return err
		}
	}
	if description != nil {
		group.Description, err = normalizePublicText(*description, 300, false)
		if err != nil {
			return err
		}
	}
	if tags != nil {
		normalized, err := normalizeGroupTags(*tags)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return err
		}
		group.Tags = string(encoded)
	}
	if group.CustomName == oldName && group.Description == oldDescription && group.Tags == oldTags {
		return nil
	}
	group.ModerationStatus = "pending"
	group.ModerationReason = ""
	group.ModerationCheckedAt = nil
	return nil
}

func safePublicText(value string, limit int, name bool) string {
	text, err := normalizePublicText(value, limit, name)
	if err != nil {
		return ""
	}
	return text
}

// Old channels without custom public content need no semantic approval. Every
// custom revision must pass platform-controlled or administrator review.
func approvedMarketplacePresentationQuery(query *gorm.DB) *gorm.DB {
	return query.Where("(moderation_status = ? OR (COALESCE(moderation_status, '') = '' AND COALESCE(custom_name, '') = '' AND COALESCE(description, '') = '' AND COALESCE(tags, '') IN ?))", "passed", []string{"", "[]", "null"})
}

func marketplacePresentationApproved(group *marketplaceschema.Group) bool {
	return group.ModerationStatus == "passed" || (group.ModerationStatus == "" && group.CustomName == "" && group.Description == "" && len(decodeModels(group.Tags)) == 0)
}

func hasGroupTag(group marketplaceschema.Group, tag string) bool {
	if strings.TrimSpace(tag) == "" {
		return true
	}
	for _, existing := range decodeModels(group.Tags) {
		if strings.EqualFold(existing, strings.TrimSpace(tag)) {
			return true
		}
	}
	return false
}
