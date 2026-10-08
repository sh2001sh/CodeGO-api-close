package channelmarket

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// ErrInvalidName is separate from generic payload failures so clients can
// explain the public naming rules without returning submitted content.
var ErrInvalidName = fmt.Errorf("%w: invalid group name", ErrInvalid)

const MaxGroupNameLength = 40

var (
	nameDomain      = regexp.MustCompile(`(?i)(?:[\p{L}\p{N}-]+\.)+(?:[\p{L}]{2,24}|xn--[a-z0-9-]+)(?:\b|$)`)
	namePhone       = regexp.MustCompile(`(?:[0-9][\s().+-]*){7,}`)
	nameAdvertising = regexp.MustCompile(`(?i)(?:https?|www|telegram|whatsapp|wechat|discord|weixin|vx|qq|t\.me|contact|join|buy|sale|discount|coupon|promo|cheap|free|official|verified|客服|联系|聯繫|加微|微信|微[信訊]|电报|電報|企鹅|企鵝|加群|进群|進群|购买|購買|优惠|優惠|折扣|低价|低價|充值|返利|推广|推廣|招商|代理|广告|廣告|包月|包年|免费|免費|官方|认证|認證|直营|直營|碼高|码高|codego)`)
)

// Public names are short service labels. Normalize compatibility characters
// before checking them so full-width contact information cannot bypass rules.
// Letters/marks from all supported languages remain usable; controls, hidden
// formatting, contact punctuation and decorative payloads are not name text.
func normalizeGroupName(value string) (string, error) {
	if !utf8.ValidString(value) || len(value) > 255 {
		return "", ErrInvalidName
	}
	value = norm.NFKC.String(value)
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) {
			return "", ErrInvalidName
		}
		if unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || r == ' ' || strings.ContainsRune("-_.()/+&", r) {
			continue
		}
		return "", ErrInvalidName
	}
	value = strings.Join(strings.Fields(value), " ")
	if (value != "" && utf8.RuneCountInString(value) < 2) || utf8.RuneCountInString(value) > MaxGroupNameLength || len(value) > 255 {
		return "", ErrInvalidName
	}
	if value != "" && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) {
		return "", ErrInvalidName
	}
	if publicTextHasAdvertising(value) {
		return "", ErrInvalidName
	}
	return value, nil
}

func publicTextHasAdvertising(value string) bool {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, value)
	contact := strings.Map(func(r rune) rune {
		for _, block := range unicode.Digit.R16 {
			if uint32(r) >= uint32(block.Lo) && uint32(r) <= uint32(block.Hi) && (uint32(r)-uint32(block.Lo))%uint32(block.Stride) == 0 {
				return '0' + rune((uint32(r)-uint32(block.Lo))/uint32(block.Stride)%10)
			}
		}
		for _, block := range unicode.Digit.R32 {
			if uint32(r) >= block.Lo && uint32(r) <= block.Hi && (uint32(r)-block.Lo)%block.Stride == 0 {
				return '0' + rune((uint32(r)-block.Lo)/block.Stride%10)
			}
		}
		return r
	}, value)
	return nameDomain.MatchString(value) || namePhone.MatchString(contact) || nameAdvertising.MatchString(compact)
}
