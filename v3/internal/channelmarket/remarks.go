package channelmarket

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var ErrInvalidRemark = fmt.Errorf("%w: invalid group remark", ErrInvalid)

const MaxGroupRemarkLength = 200

func normalizeGroupRemark(value string) (string, error) {
	if !utf8.ValidString(value) || len(value) > 2048 {
		return "", ErrInvalidRemark
	}
	value = norm.NFKC.String(value)
	for _, r := range value {
		if r == '\n' || r == '\r' {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) || strings.ContainsRune("@\\<>", r) {
			return "", ErrInvalidRemark
		}
		if unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		return "", ErrInvalidRemark
	}
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > MaxGroupRemarkLength || publicTextHasAdvertising(value) {
		return "", ErrInvalidRemark
	}
	return value, nil
}
