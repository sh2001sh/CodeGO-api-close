package app

import (
	"strings"
	"unicode"

	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

func ownerUserIDsByExternalID(search string) ([]int, error) {
	search = normalizeExternalIDSearch(search)
	if search == "" {
		return nil, nil
	}
	var ids []int
	err := platformdb.DB.Unscoped().Model(&identityschema.User{}).
		Where("external_id LIKE ?", "%"+search+"%").
		Pluck("id", &ids).Error
	return ids, err
}

func ownerExternalIDs(ownerUserIDs []int) (map[int]string, error) {
	result := make(map[int]string, len(ownerUserIDs))
	if len(ownerUserIDs) == 0 {
		return result, nil
	}
	var users []identityschema.User
	if err := platformdb.DB.Unscoped().Select("id", "external_id").Where("id IN ?", ownerUserIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		result[user.Id] = user.ExternalId
	}
	return result, nil
}

func normalizeExternalIDSearch(value string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsDigit(char) || (char >= 'A' && char <= 'Z') {
			return char
		}
		return -1
	}, strings.ToUpper(strings.TrimSpace(value)))
}
